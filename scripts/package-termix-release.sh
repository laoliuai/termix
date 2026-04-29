#!/usr/bin/env sh
set -eu

VERSION="${VERSION:-dev}"
OUT_DIR="${OUT_DIR:-dist/release}"
ROOT_DIR="$(CDPATH= cd "$(dirname "$0")/.." && pwd)"

case "$OUT_DIR" in
  /*) RELEASE_DIR="$OUT_DIR" ;;
  *) RELEASE_DIR="$ROOT_DIR/$OUT_DIR" ;;
esac

mkdir -p "$RELEASE_DIR"

CURRENT_WORK_DIR=
CURRENT_CHILD_PID=

cleanup_current() {
  if [ -n "$CURRENT_CHILD_PID" ]; then
    kill "$CURRENT_CHILD_PID" 2>/dev/null || true
    CURRENT_CHILD_PID=
  fi
  if [ -n "$CURRENT_WORK_DIR" ]; then
    rm -rf "$CURRENT_WORK_DIR"
    CURRENT_WORK_DIR=
  fi
}

trap 'cleanup_current' EXIT
trap 'cleanup_current; exit 129' HUP
trap 'cleanup_current; exit 130' INT
trap 'cleanup_current; exit 143' TERM

run_command() {
  "$@" &
  CURRENT_CHILD_PID=$!
  set +e
  wait "$CURRENT_CHILD_PID"
  status=$?
  set -e
  CURRENT_CHILD_PID=
  return "$status"
}

package_one() {
  os="$1"
  arch="$2"
  artifact_arch="$3"
  goos="$(printf '%s' "$os" | tr '[:upper:]' '[:lower:]')"
  work_dir="$RELEASE_DIR/termix_${os}_${artifact_arch}"
  archive="$RELEASE_DIR/termix_${os}_${artifact_arch}.tar.gz"

  CURRENT_WORK_DIR="$work_dir"
  rm -rf "$work_dir"
  mkdir -p "$work_dir"

  run_command env GOOS="$goos" GOARCH="$arch" \
    go build -ldflags "-X main.version=$VERSION" -o "$work_dir/termix" ./cmd/termix

  run_command cp "$ROOT_DIR/README.md" "$work_dir/README.md"
  if [ -f "$ROOT_DIR/LICENSE" ]; then
    run_command cp "$ROOT_DIR/LICENSE" "$work_dir/LICENSE"
  fi

  run_command tar -C "$work_dir" -czf "$archive" .
  cleanup_current
  printf 'wrote %s\n' "$archive"
}

cd "$ROOT_DIR/go"
package_one Darwin amd64 x86_64
package_one Darwin arm64 arm64
package_one Linux amd64 x86_64
package_one Linux arm64 arm64
