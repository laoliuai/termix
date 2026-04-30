#!/usr/bin/env sh
set -eu

ROOT_DIR="$(CDPATH= cd "$(dirname "$0")/.." && pwd)"
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

assert_eq "$(download_url laoliuai/termix latest termix_Linux_x86_64.tar.gz)" \
  "https://github.com/laoliuai/termix/releases/latest/download/termix_Linux_x86_64.tar.gz" \
  "latest url"

assert_eq "$(download_url laoliuai/termix v0.1.0 termix_Linux_x86_64.tar.gz)" \
  "https://github.com/laoliuai/termix/releases/download/v0.1.0/termix_Linux_x86_64.tar.gz" \
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

TEST_TMP="${TMPDIR:-/tmp}/termix-install-test.$$"
mkdir -p "$TEST_TMP"
trap 'rm -rf "$TEST_TMP"' EXIT HUP INT TERM

make_fake_tools() {
  fake_bin="$TEST_TMP/fake-bin"
  mkdir -p "$fake_bin"

  cat >"$fake_bin/uname" <<'EOF'
#!/usr/bin/env sh
case "${1:-}" in
  -s) printf '%s\n' "${TERMIX_TEST_UNAME_S:-Linux}" ;;
  -m) printf '%s\n' "${TERMIX_TEST_UNAME_M:-x86_64}" ;;
  *) printf '%s\n' "${TERMIX_TEST_UNAME_S:-Linux}" ;;
esac
EOF

  cat >"$fake_bin/curl" <<'EOF'
#!/usr/bin/env sh
out=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    -o)
      shift
      out="$1"
      ;;
  esac
  shift
done
if [ -z "$out" ]; then
  echo "fake curl missing -o" >&2
  exit 1
fi
cp "$TERMIX_TEST_ARCHIVE" "$out"
EOF

  cat >"$fake_bin/mktemp" <<'EOF'
#!/usr/bin/env sh
if [ "${1:-}" != "-d" ]; then
  echo "fake mktemp only supports -d" >&2
  exit 1
fi
count=0
if [ -f "$TERMIX_TEST_MKTEMP_COUNTER" ]; then
  count="$(cat "$TERMIX_TEST_MKTEMP_COUNTER")"
fi
count=$((count + 1))
printf '%s\n' "$count" >"$TERMIX_TEST_MKTEMP_COUNTER"
dir="$TERMIX_TEST_MKTEMP_ROOT/tmp.$count"
mkdir -p "$dir"
printf '%s\n' "$dir"
EOF

  chmod +x "$fake_bin/uname" "$fake_bin/curl" "$fake_bin/mktemp"
}

make_regular_archive() {
  archive="$1"
  stage="$TEST_TMP/regular-stage"
  rm -rf "$stage"
  mkdir -p "$stage"
  cat >"$stage/termix" <<'EOF'
#!/usr/bin/env sh
if [ "${1:-}" = "--version" ]; then
  echo "termix test-version"
else
  echo "unexpected termix invocation: $*" >&2
  exit 1
fi
EOF
  chmod +x "$stage/termix"
  (cd "$stage" && tar -czf "$archive" termix)
}

make_dot_termix_archive() {
  archive="$1"
  stage="$TEST_TMP/dot-termix-stage"
  rm -rf "$stage"
  mkdir -p "$stage"
  cat >"$stage/termix" <<'EOF'
#!/usr/bin/env sh
if [ "${1:-}" = "--version" ]; then
  echo "termix test-version"
else
  echo "unexpected termix invocation: $*" >&2
  exit 1
fi
EOF
  chmod +x "$stage/termix"
  (cd "$stage" && tar -czf "$archive" ./termix)
}

make_missing_archive() {
  archive="$1"
  stage="$TEST_TMP/missing-stage"
  rm -rf "$stage"
  mkdir -p "$stage"
  echo "not termix" >"$stage/README.md"
  (cd "$stage" && tar -czf "$archive" README.md)
}

make_symlink_archive() {
  archive="$1"
  stage="$TEST_TMP/symlink-stage"
  rm -rf "$stage"
  mkdir -p "$stage"
  echo "target" >"$stage/target"
  ln -s target "$stage/termix"
  (cd "$stage" && tar -czf "$archive" termix target)
}

run_installer_main() {
  archive="$1"
  install_dir="$2"
  path_value="$3"
  output_file="$4"
  mktemp_root="$TEST_TMP/mktemp"
  counter="$TEST_TMP/mktemp-counter"
  rm -rf "$mktemp_root" "$counter"
  mkdir -p "$mktemp_root"

  TERMIX_TEST_ARCHIVE="$archive" \
  TERMIX_TEST_MKTEMP_ROOT="$mktemp_root" \
  TERMIX_TEST_MKTEMP_COUNTER="$counter" \
  TERMIX_INSTALL_DIR="$install_dir" \
  PATH="$fake_bin:$path_value" \
  sh <"$ROOT_DIR/install.sh" >"$output_file" 2>&1
}

assert_output_contains() {
  file="$1"
  needle="$2"
  label="$3"
  if ! grep -F "$needle" "$file" >/dev/null 2>&1; then
    fail "$label: expected output to contain '$needle'"
  fi
}

assert_output_not_contains() {
  file="$1"
  needle="$2"
  label="$3"
  if grep -F "$needle" "$file" >/dev/null 2>&1; then
    fail "$label: expected output not to contain '$needle'"
  fi
}

assert_no_tmp_dirs() {
  label="$1"
  set -- "$TEST_TMP/mktemp"/*
  if [ "$1" != "$TEST_TMP/mktemp/*" ]; then
    fail "$label: installer temporary directories were not cleaned up"
  fi
}

make_fake_tools
regular_archive="$TEST_TMP/termix-regular.tar.gz"
dot_termix_archive="$TEST_TMP/termix-dot.tar.gz"
missing_archive="$TEST_TMP/termix-missing.tar.gz"
symlink_archive="$TEST_TMP/termix-symlink.tar.gz"
make_regular_archive "$regular_archive"
make_dot_termix_archive "$dot_termix_archive"
make_missing_archive "$missing_archive"
make_symlink_archive "$symlink_archive"

install_dir_with_spaces="$TEST_TMP/install dir with spaces"
output="$TEST_TMP/success-with-path-hint.out"
run_installer_main "$regular_archive" "$install_dir_with_spaces" "/usr/bin:/bin" "$output"
if [ ! -x "$install_dir_with_spaces/termix" ]; then
  fail "successful install did not create executable termix in install dir with spaces"
fi
assert_output_contains "$output" "termix test-version" "successful install runs termix --version"
assert_output_contains "$output" "export PATH=\"$install_dir_with_spaces:\$PATH\"" "path hint when install dir absent"
assert_no_tmp_dirs "successful install cleanup"

install_dir_on_path="$TEST_TMP/install-on-path"
output="$TEST_TMP/success-without-path-hint.out"
run_installer_main "$regular_archive" "$install_dir_on_path" "$install_dir_on_path:/usr/bin:/bin" "$output"
assert_output_not_contains "$output" "Add Termix to your PATH:" "no path hint when install dir present"
assert_no_tmp_dirs "path-present install cleanup"

output="$TEST_TMP/dot-termix-success.out"
run_installer_main "$dot_termix_archive" "$TEST_TMP/dot-termix-install" "/usr/bin:/bin" "$output"
assert_output_contains "$output" "termix test-version" "dot-slash termix archive installs"
assert_no_tmp_dirs "dot-slash termix install cleanup"

output="$TEST_TMP/missing-failure.out"
if run_installer_main "$missing_archive" "$TEST_TMP/missing-install" "/usr/bin:/bin" "$output"; then
  fail "archive missing termix succeeded"
fi
assert_output_contains "$output" "downloaded archive does not contain termix" "missing termix failure"
assert_no_tmp_dirs "missing archive failure cleanup"

output="$TEST_TMP/symlink-failure.out"
if run_installer_main "$symlink_archive" "$TEST_TMP/symlink-install" "/usr/bin:/bin" "$output"; then
  fail "archive with symlink termix succeeded"
fi
assert_output_contains "$output" "downloaded archive termix is a symlink" "symlink termix failure"
assert_no_tmp_dirs "symlink archive failure cleanup"

echo "install.sh tests passed"
