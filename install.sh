#!/usr/bin/env sh
set -eu

TERMIX_REPO="${TERMIX_REPO:-termix/termix}"
TERMIX_VERSION="${TERMIX_VERSION:-latest}"
TERMIX_INSTALL_DIR="${TERMIX_INSTALL_DIR:-$HOME/.local/bin}"

normalize_os() {
  case "$1" in
    Darwin | darwin) printf 'Darwin' ;;
    Linux | linux) printf 'Linux' ;;
    *) printf 'unsupported' ;;
  esac
}

normalize_arch() {
  case "$1" in
    x86_64 | amd64) printf 'x86_64' ;;
    arm64 | aarch64) printf 'arm64' ;;
    *) printf 'unsupported' ;;
  esac
}

asset_name() {
  os="$(normalize_os "$1")"
  arch="$(normalize_arch "$2")"
  if [ "$os" = "unsupported" ] || [ "$arch" = "unsupported" ]; then
    printf 'unsupported'
    return
  fi
  printf 'termix_%s_%s.tar.gz' "$os" "$arch"
}

download_url() {
  repo="$1"
  version="$2"
  asset="$3"
  if [ "$version" = "latest" ]; then
    printf 'https://github.com/%s/releases/latest/download/%s' "$repo" "$asset"
  else
    printf 'https://github.com/%s/releases/download/%s/%s' "$repo" "$version" "$asset"
  fi
}

path_contains_dir() {
  dir="$1"
  old_ifs="$IFS"
  IFS=:
  for item in ${PATH:-}; do
    if [ "$item" = "$dir" ]; then
      IFS="$old_ifs"
      return 0
    fi
  done
  IFS="$old_ifs"
  return 1
}

require_command() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "$1 is required to install Termix" >&2
    exit 1
  fi
}

cleanup_tmp_dir() {
  if [ -n "${tmp_dir:-}" ] && [ -d "$tmp_dir" ]; then
    rm -rf "$tmp_dir"
  fi
}

archive_termix_member() {
  archive="$1"
  member=""
  members="$(tar -tzf "$archive")"
  while IFS= read -r item; do
    case "$item" in
      termix | ./termix)
        member="$item"
        break
        ;;
    esac
  done <<EOF
$members
EOF
  printf '%s' "$member"
}

main() {
  uname_os="$(uname -s)"
  uname_arch="$(uname -m)"
  os="$(normalize_os "$uname_os")"
  arch="$(normalize_arch "$uname_arch")"
  if [ "$os" = "unsupported" ]; then
    echo "unsupported operating system: $uname_os" >&2
    exit 1
  fi
  if [ "$arch" = "unsupported" ]; then
    echo "unsupported CPU architecture: $uname_arch" >&2
    exit 1
  fi

  require_command curl
  require_command tar
  require_command mktemp

  asset="$(asset_name "$os" "$arch")"
  url="$(download_url "$TERMIX_REPO" "$TERMIX_VERSION" "$asset")"
  tmp_dir="$(mktemp -d)"
  trap 'cleanup_tmp_dir' EXIT
  trap 'cleanup_tmp_dir; exit 1' HUP INT TERM

  echo "Downloading $url"
  curl -fsSL "$url" -o "$tmp_dir/$asset"
  termix_member="$(archive_termix_member "$tmp_dir/$asset")"
  if [ -z "$termix_member" ]; then
    echo "downloaded archive does not contain termix" >&2
    exit 1
  fi
  extract_dir="$tmp_dir/extract"
  mkdir -p "$extract_dir"
  tar -xzf "$tmp_dir/$asset" -C "$extract_dir" "$termix_member"
  termix_file="$extract_dir/termix"
  if [ -L "$termix_file" ]; then
    echo "downloaded archive termix is a symlink" >&2
    exit 1
  fi
  if [ ! -f "$termix_file" ]; then
    echo "downloaded archive termix is not a regular file" >&2
    exit 1
  fi

  mkdir -p "$TERMIX_INSTALL_DIR"
  cp "$termix_file" "$TERMIX_INSTALL_DIR/termix"
  chmod +x "$TERMIX_INSTALL_DIR/termix"

  echo "Installed termix to $TERMIX_INSTALL_DIR/termix"
  "$TERMIX_INSTALL_DIR/termix" --version

  if ! path_contains_dir "$TERMIX_INSTALL_DIR"; then
    echo ""
    echo "Add Termix to your PATH:"
    echo "  export PATH=\"$TERMIX_INSTALL_DIR:\$PATH\""
  fi
}

if [ "${TERMIX_INSTALL_TEST:-0}" != "1" ]; then
  main "$@"
fi
