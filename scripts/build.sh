#!/usr/bin/env bash
# Cross-compile council (CGO disabled) for all release targets into dist/.
#   scripts/build.sh            build all targets
#   VERSION=v1.0.0 scripts/build.sh
set -euo pipefail
cd "$(dirname "$0")/.."

VERSION=${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo dev)}
TARGETS=${TARGETS:-"darwin/amd64 darwin/arm64 linux/amd64 linux/arm64 windows/amd64"}
OUT=${OUT:-dist}
LDFLAGS="-s -w -X council/internal/cli.Version=${VERSION}"

rm -rf "$OUT"
mkdir -p "$OUT"
for t in $TARGETS; do
	os=${t%/*}
	arch=${t#*/}
	ext=""
	[ "$os" = windows ] && ext=".exe"
	bin="$OUT/council-${os}-${arch}${ext}"
	echo "building $bin"
	CGO_ENABLED=0 GOOS=$os GOARCH=$arch go build -trimpath -ldflags "$LDFLAGS" -o "$bin" ./cmd/council
done
(cd "$OUT" && sha256sum council-* > SHA256SUMS)
echo "done: $(ls "$OUT" | wc -l) files in $OUT/ (version $VERSION)"
