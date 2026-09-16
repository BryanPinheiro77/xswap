#!/bin/sh
set -eu

VERSION=${VERSION:?Set VERSION to a semantic version tag, such as v0.1.0}
printf '%s\n' "$VERSION" | grep -Eq '^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z.-]+)?$'
COMMIT=${COMMIT:-unknown}
REPOSITORY=${REPOSITORY:-}
if [ -n "$REPOSITORY" ]; then
  printf '%s\n' "$REPOSITORY" | grep -Eq '^[A-Za-z0-9][A-Za-z0-9-]*/[A-Za-z0-9_.-]*[A-Za-z0-9_][A-Za-z0-9_.-]*$'
fi
BUILD_DATE=${BUILD_DATE:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}
staging=$(mktemp -d)
trap 'rm -rf "$staging"' EXIT HUP INT TERM
output=$(pwd)/dist
mkdir -p "$output"

for platform in darwin/arm64 darwin/amd64 linux/arm64 linux/amd64; do
  goos=${platform%/*}
  goarch=${platform#*/}
  name="xswap_${VERSION}_${goos}_${goarch}"
  mkdir -p "$staging/$name"
  CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" go build -trimpath \
    -ldflags "-s -w -X main.version=$VERSION -X main.commit=$COMMIT -X main.buildDate=$BUILD_DATE -X main.releaseRepo=$REPOSITORY" \
    -o "$staging/$name/xswap" ./cmd/xswap
  cp README.md LICENSE "$staging/$name/"
  tar -czf "$output/$name.tar.gz" -C "$staging/$name" xswap README.md LICENSE
done

for platform in windows/amd64 windows/arm64; do
  goos=${platform%/*}
  goarch=${platform#*/}
  name="xswap_${VERSION}_${goos}_${goarch}"
  mkdir -p "$staging/$name"
  CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" go build -trimpath \
    -ldflags "-s -w -X main.version=$VERSION -X main.commit=$COMMIT -X main.buildDate=$BUILD_DATE -X main.releaseRepo=$REPOSITORY" \
    -o "$staging/$name/xswap.exe" ./cmd/xswap
  cp README.md LICENSE "$staging/$name/"
  (cd "$staging/$name" && zip -q "$output/$name.zip" xswap.exe README.md LICENSE)
done

(
  cd "$output"
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum ./xswap_"$VERSION"_*.tar.gz ./xswap_"$VERSION"_*.zip > checksums.txt
  else
    shasum -a 256 ./xswap_"$VERSION"_*.tar.gz ./xswap_"$VERSION"_*.zip > checksums.txt
  fi
)
