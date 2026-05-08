#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

# Docker Compose project name — kept stable so volume names are predictable.
DC_PROJECT="termix"
DC="sudo docker compose -p $DC_PROJECT"

usage() {
    cat <<EOF
Usage: $0 -s <user@host> -d <domain> [-e <env-file>] [-k <ssh-key>] [--no-tls] [--skip-build]

  -s, --server      SSH target, e.g. ubuntu@203.0.113.10  (required)
  -d, --domain      Domain pointing to the server, e.g. termix.cloud  (required)
  -e, --env         Local .env file  (default: deploy/.env)
  -k, --key         SSH private key path
      --no-tls      Skip Let's Encrypt TLS setup (serve HTTP only)
      --skip-build  Skip docker compose build (use existing images)
EOF
    exit 1
}

SERVER="" DOMAIN="" ENV_FILE="$SCRIPT_DIR/.env" SSH_KEY="" NO_TLS=0 SKIP_BUILD=0

while [[ $# -gt 0 ]]; do
    case "$1" in
        -s|--server)   SERVER="$2";   shift 2 ;;
        -d|--domain)   DOMAIN="$2";   shift 2 ;;
        -e|--env)      ENV_FILE="$2"; shift 2 ;;
        -k|--key)      SSH_KEY="$2";  shift 2 ;;
        --no-tls)      NO_TLS=1;      shift ;;
        --skip-build)  SKIP_BUILD=1;  shift ;;
        *) usage ;;
    esac
done

[[ -z "$SERVER" || -z "$DOMAIN" ]] && usage

SSH_OPTS="-o StrictHostKeyChecking=accept-new -o ConnectTimeout=10"
[[ -n "$SSH_KEY" ]] && SSH_OPTS="$SSH_OPTS -i $SSH_KEY"

ssh_run() { ssh $SSH_OPTS "$SERVER" "$@"; }
scp_up()  { scp $SSH_OPTS "$@"; }

# ---------------------------------------------------------------------------
# 1. Prepare .env
# ---------------------------------------------------------------------------
if [[ ! -f "$ENV_FILE" ]]; then
    echo "No .env found at $ENV_FILE — generating from template."
    cp "$SCRIPT_DIR/.env.example" "$ENV_FILE"
    PG_PASS="$(openssl rand -hex 32)"
    JWT_KEY="$(openssl rand -hex 32)"
    sed -i "s/^DOMAIN=.*/DOMAIN=$DOMAIN/" "$ENV_FILE"
    sed -i "s/^POSTGRES_PASSWORD=.*/POSTGRES_PASSWORD=$PG_PASS/" "$ENV_FILE"
    sed -i "s/^JWT_SIGNING_KEY=.*/JWT_SIGNING_KEY=$JWT_KEY/" "$ENV_FILE"
    echo "Generated $ENV_FILE — edit it:"
    echo "  - Set TERMIX_ADMIN_EMAIL / TERMIX_ADMIN_PASSWORD"
    echo "  - Set CERTBOT_EMAIL for TLS"
    echo "Then re-run: $0 -s $SERVER -d $DOMAIN"
    exit 0
fi

# Load .env for local use
set -o allexport
source "$ENV_FILE"
set +o allexport

DOMAIN_IN_ENV="$(grep '^DOMAIN=' "$ENV_FILE" | cut -d= -f2)"
if [[ "$DOMAIN_IN_ENV" != "$DOMAIN" ]]; then
    echo "WARNING: DOMAIN in .env ($DOMAIN_IN_ENV) differs from -d arg ($DOMAIN). Using -d arg."
    sed -i "s/^DOMAIN=.*/DOMAIN=$DOMAIN/" "$ENV_FILE"
fi

# ---------------------------------------------------------------------------
# 2. Test SSH
# ---------------------------------------------------------------------------
echo "==> Testing SSH connection to $SERVER..."
ssh_run "echo 'SSH OK'"

# ---------------------------------------------------------------------------
# 3. Bootstrap Docker on the server
# ---------------------------------------------------------------------------
echo "==> Checking Docker on server..."
ssh_run 'bash -s' <<'REMOTE'
if ! command -v docker &>/dev/null; then
    echo "Installing Docker..."
    curl -fsSL https://get.docker.com | sh
fi
if ! sudo docker compose version &>/dev/null 2>&1; then
    echo "Installing Docker Compose plugin..."
    sudo apt-get install -y docker-compose-plugin 2>/dev/null || \
    sudo yum install -y docker-compose-plugin 2>/dev/null || \
    { echo "Could not install docker-compose-plugin — install manually"; exit 1; }
fi
echo "Docker: $(sudo docker --version)"
echo "Compose: $(sudo docker compose version)"
REMOTE

# ---------------------------------------------------------------------------
# 4. Upload repo to server
# ---------------------------------------------------------------------------
echo "==> Syncing repo to server ~/termix..."
ssh_run "mkdir -p ~/termix"
rsync -az --delete \
    --exclude='.git' \
    --exclude='.worktrees' \
    --exclude='go/bin' \
    --exclude='web/app/node_modules' \
    --exclude='web/app/dist' \
    --exclude='deploy/.env' \
    $([[ -n "$SSH_KEY" ]] && echo "-e 'ssh -i $SSH_KEY'" || echo "") \
    "$REPO_ROOT/" "$SERVER:~/termix/"

# ---------------------------------------------------------------------------
# 5. Upload .env
# ---------------------------------------------------------------------------
echo "==> Uploading .env..."
scp_up "$ENV_FILE" "$SERVER:~/termix/deploy/.env"

# ---------------------------------------------------------------------------
# 6. Generate initial nginx.conf (HTTP only)
# ---------------------------------------------------------------------------
echo "==> Configuring nginx (HTTP)..."
NGINX_CONF="$(sed "s/__DOMAIN__/$DOMAIN/g" "$SCRIPT_DIR/nginx.http.conf.template")"
ssh_run "cat > ~/termix/deploy/nginx.conf" <<< "$NGINX_CONF"

# ---------------------------------------------------------------------------
# 7. Build images and start services
# ---------------------------------------------------------------------------
echo "==> Building and starting services..."
if [[ "$SKIP_BUILD" -eq 0 ]]; then
    ssh_run "cd ~/termix/deploy && $DC build"
fi
ssh_run "cd ~/termix/deploy && $DC up -d"

# ---------------------------------------------------------------------------
# 8. Wait for control to be healthy
# ---------------------------------------------------------------------------
echo "==> Waiting for termix-control to be healthy..."
ssh_run "bash -s -- '$DC'" <<'REMOTE'
DC="$1"
for i in $(seq 1 30); do
    if $DC -f ~/termix/deploy/docker-compose.yml ps control 2>/dev/null | grep -q "healthy"; then
        echo "control is healthy"
        exit 0
    fi
    echo "  waiting ($i/30)..."
    sleep 5
done
echo "WARNING: control did not become healthy in time; continuing anyway"
REMOTE

# ---------------------------------------------------------------------------
# 9. TLS with Let's Encrypt (optional)
# ---------------------------------------------------------------------------
if [[ "$NO_TLS" -eq 0 ]]; then
    CERTBOT_EMAIL="${CERTBOT_EMAIL:-}"
    if [[ -z "$CERTBOT_EMAIL" ]]; then
        echo "WARNING: CERTBOT_EMAIL not set in .env — skipping TLS setup."
        echo "  Set CERTBOT_EMAIL in $ENV_FILE and re-run with --skip-build to add TLS."
    else
        echo "==> Obtaining Let's Encrypt certificate for $DOMAIN..."
        # --non-interactive: never prompt (SSH has no TTY for stdin).
        # --keep-until-expiring: idempotent — if the cert is still
        #   valid, exit 0 instead of asking "keep / renew" and EOFing.
        # Together these make repeat deploys safe; without them, the
        # second-and-later deploys errored out and left nginx.conf
        # mid-swap (HTTP template on disk, SSL template still in nginx
        # memory until something forces a reload).
        ssh_run "sudo docker run --rm \
            -v ${DC_PROJECT}_certbot-www:/var/www/certbot \
            -v ${DC_PROJECT}_certbot-conf:/etc/letsencrypt \
            certbot/certbot certonly --webroot \
            --webroot-path=/var/www/certbot \
            --email $CERTBOT_EMAIL \
            --agree-tos --no-eff-email \
            --non-interactive --keep-until-expiring \
            -d $DOMAIN"

        echo "==> Switching nginx to HTTPS..."
        NGINX_SSL_CONF="$(sed "s/__DOMAIN__/$DOMAIN/g" "$SCRIPT_DIR/nginx.ssl.conf.template")"
        ssh_run "cat > ~/termix/deploy/nginx.conf" <<< "$NGINX_SSL_CONF"
        ssh_run "cd ~/termix/deploy && $DC exec nginx nginx -s reload"

        echo "==> Setting up weekly cert renewal cron..."
        RENEWAL_CMD="sudo docker run --rm -v ${DC_PROJECT}_certbot-www:/var/www/certbot -v ${DC_PROJECT}_certbot-conf:/etc/letsencrypt certbot/certbot renew --quiet && $DC -f ~/termix/deploy/docker-compose.yml exec nginx nginx -s reload"
        ssh_run "(crontab -l 2>/dev/null | grep -v 'certbot/certbot renew'; echo \"0 3 * * 1 $RENEWAL_CMD\") | crontab -"
        echo "Renewal cron set (every Monday 03:00)."
    fi
fi

# ---------------------------------------------------------------------------
# 10. Publish install script to termix.cloud mirror
# ---------------------------------------------------------------------------
echo "==> Publishing install.sh to download mirror..."
ssh_run "mkdir -p /srv/termix/downloads/releases/latest"
scp_up "$SCRIPT_DIR/www/install.sh" "$SERVER:/srv/termix/downloads/install.sh"
ssh_run "chmod 644 /srv/termix/downloads/install.sh"
echo "    https://$DOMAIN/install.sh is now live."

# ---------------------------------------------------------------------------
# Done
# ---------------------------------------------------------------------------
PROTO="http"
[[ "$NO_TLS" -eq 0 && -n "${CERTBOT_EMAIL:-}" ]] && PROTO="https"
echo ""
echo "================================================================"
echo "  Termix deployed!"
echo "  URL:    $PROTO://$DOMAIN"
echo "  Logs:   ssh $SERVER '$DC -f ~/termix/deploy/docker-compose.yml logs -f'"
echo "  Shell:  ssh $SERVER '$DC -f ~/termix/deploy/docker-compose.yml exec control bash'"
echo "================================================================"
