#!/usr/bin/env sh
# Upload release artifacts to the termix.cloud download mirror.
#
# After a GitHub Release, run this to make the same binaries available at
# https://termix.cloud/releases/<version>/ (and /releases/latest/).
#
# Usage:
#   ./scripts/upload-release.sh -s <user@host> -v <version> [-k <ssh-key>] <dist-dir>
#
# Example:
#   VERSION=v0.4.0 ./scripts/package-termix-release.sh
#   ./scripts/upload-release.sh -s ubuntu@203.0.113.10 -v v0.4.0 dist/release
set -eu

usage() {
  cat >&2 <<EOF
Usage: $0 -s <user@host> -v <version> [-k <ssh-key>] <dist-dir>

  -s, --server    SSH target, e.g. ubuntu@203.0.113.10  (required)
  -v, --version   Release version tag, e.g. v0.4.0      (required)
  -k, --key       SSH private key path
  <dist-dir>      Directory produced by package-termix-release.sh
                  (contains termix_*.tar.gz files)
EOF
  exit 1
}

SERVER="" VERSION="" SSH_KEY="" DIST_DIR=""

while [ $# -gt 0 ]; do
  case "$1" in
    -s|--server)  SERVER="$2";  shift 2 ;;
    -v|--version) VERSION="$2"; shift 2 ;;
    -k|--key)     SSH_KEY="$2"; shift 2 ;;
    -*)           usage ;;
    *)            DIST_DIR="$1"; shift ;;
  esac
done

[ -z "$SERVER" ] || [ -z "$VERSION" ] || [ -z "$DIST_DIR" ] && usage

if [ ! -d "$DIST_DIR" ]; then
  printf 'dist-dir not found: %s\n' "$DIST_DIR" >&2
  exit 1
fi

SSH_OPTS="-o StrictHostKeyChecking=accept-new -o ConnectTimeout=10"
[ -n "$SSH_KEY" ] && SSH_OPTS="$SSH_OPTS -i $SSH_KEY"

ssh_run() { ssh $SSH_OPTS "$SERVER" "$@"; }
scp_up()  { scp $SSH_OPTS "$@"; }

VERSIONED_DIR="/srv/termix/downloads/releases/$VERSION"
LATEST_DIR="/srv/termix/downloads/releases/latest"

echo "==> Creating remote directories..."
ssh_run "mkdir -p '$VERSIONED_DIR' '$LATEST_DIR'"

ASSETS=$(find "$DIST_DIR" -maxdepth 1 -name 'termix_*.tar.gz')
if [ -z "$ASSETS" ]; then
  printf 'No termix_*.tar.gz files found in %s\n' "$DIST_DIR" >&2
  exit 1
fi

echo "==> Uploading artifacts for $VERSION..."
for archive in $ASSETS; do
  name="$(basename "$archive")"
  printf '    %s\n' "$name"
  scp_up "$archive" "$SERVER:$VERSIONED_DIR/$name"
done

echo "==> Updating latest/..."
ssh_run "cp '$VERSIONED_DIR/'termix_*.tar.gz '$LATEST_DIR/'"

echo ""
echo "Done. Artifacts available at:"
printf '    https://termix.cloud/releases/%s/\n' "$VERSION"
echo "    https://termix.cloud/releases/latest/"
