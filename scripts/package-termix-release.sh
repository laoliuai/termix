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

package_one() {
  (
    os="$1"
    arch="$2"
    artifact_arch="$3"
    goos="$(printf '%s' "$os" | tr '[:upper:]' '[:lower:]')"
    work_dir="$RELEASE_DIR/termix_${os}_${artifact_arch}"
    archive="$RELEASE_DIR/termix_${os}_${artifact_arch}.tar.gz"

    rm -rf "$work_dir"
    mkdir -p "$work_dir"
    trap 'rm -rf "$work_dir"' EXIT HUP INT TERM

    GOOS="$goos" GOARCH="$arch" \
      go build -ldflags "-X main.version=$VERSION" -o "$work_dir/termix" ./cmd/termix

    cp "$ROOT_DIR/README.md" "$work_dir/README.md"
    if [ -f "$ROOT_DIR/LICENSE" ]; then
      cp "$ROOT_DIR/LICENSE" "$work_dir/LICENSE"
    fi

    tar -C "$work_dir" -czf "$archive" .
    printf 'wrote %s\n' "$archive"
  )
}

cd "$ROOT_DIR/go"
package_one Darwin amd64 x86_64
package_one Darwin arm64 arm64
package_one Linux amd64 x86_64
package_one Linux arm64 arm64
