#!/usr/bin/env sh
set -eu

ROOT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
TERMIX_INSTALL_TEST=1 . "$ROOT_DIR/install.sh"

fail() {
  echo "FAIL: $1" >&2
  exit 1
}

assert_eq() {
  actual="$1"
  expected="$2"
  label="$3"
  if [ "$actual" != "$expected" ]; then
    fail "$label: expected '$expected', got '$actual'"
  fi
}

assert_eq "$(normalize_os Darwin)" "Darwin" "darwin os"
assert_eq "$(normalize_os darwin)" "Darwin" "lowercase darwin os"
assert_eq "$(normalize_os Linux)" "Linux" "linux os"
assert_eq "$(normalize_os linux)" "Linux" "lowercase linux os"
assert_eq "$(normalize_os FreeBSD)" "unsupported" "unsupported os"

assert_eq "$(normalize_arch x86_64)" "x86_64" "x86_64 arch"
assert_eq "$(normalize_arch amd64)" "x86_64" "amd64 arch"
assert_eq "$(normalize_arch arm64)" "arm64" "arm64 arch"
assert_eq "$(normalize_arch aarch64)" "arm64" "aarch64 arch"
assert_eq "$(normalize_arch riscv64)" "unsupported" "unsupported arch"

assert_eq "$(asset_name Darwin x86_64)" "termix_Darwin_x86_64.tar.gz" "darwin x86 asset"
assert_eq "$(asset_name darwin amd64)" "termix_Darwin_x86_64.tar.gz" "lowercase darwin amd64 asset"
assert_eq "$(asset_name Linux arm64)" "termix_Linux_arm64.tar.gz" "linux arm asset"
assert_eq "$(asset_name linux aarch64)" "termix_Linux_arm64.tar.gz" "lowercase linux aarch64 asset"
assert_eq "$(asset_name Plan9 x86_64)" "unsupported" "unsupported asset"

assert_eq "$(download_url termix/termix latest termix_Linux_x86_64.tar.gz)" \
  "https://github.com/termix/termix/releases/latest/download/termix_Linux_x86_64.tar.gz" \
  "latest url"

assert_eq "$(download_url termix/termix v0.1.0 termix_Linux_x86_64.tar.gz)" \
  "https://github.com/termix/termix/releases/download/v0.1.0/termix_Linux_x86_64.tar.gz" \
  "version url"

old_path="$PATH"
PATH="/usr/bin:/bin"
if path_contains_dir "$HOME/.local/bin"; then
  fail "path_contains_dir returned true for absent dir"
fi
PATH="$HOME/.local/bin:/usr/bin:/bin"
if ! path_contains_dir "$HOME/.local/bin"; then
  fail "path_contains_dir returned false for leading dir"
fi
PATH="/usr/bin:$HOME/.local/bin:/bin"
if ! path_contains_dir "$HOME/.local/bin"; then
  fail "path_contains_dir returned false for middle dir"
fi
PATH="/usr/bin:/bin:$HOME/.local/bin"
if ! path_contains_dir "$HOME/.local/bin"; then
  fail "path_contains_dir returned false for trailing dir"
fi
PATH="$old_path"

echo "install.sh helper tests passed"
