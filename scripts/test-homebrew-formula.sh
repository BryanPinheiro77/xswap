#!/bin/sh
set -eu

directory=$(mktemp -d)
trap 'rm -rf "$directory"' EXIT HUP INT TERM
version=v9.8.7
checksums="$directory/checksums.txt"
formula="$directory/xswap.rb"

cat > "$checksums" <<EOF
aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa  ./xswap_${version}_darwin_amd64.tar.gz
bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb  ./xswap_${version}_darwin_arm64.tar.gz
cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc  ./xswap_${version}_linux_amd64.tar.gz
dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd  ./xswap_${version}_linux_arm64.tar.gz
EOF

VERSION=$version CHECKSUMS=$checksums OUTPUT=$formula sh scripts/render-homebrew-formula.sh
ruby -c "$formula" >/dev/null
grep -Fq 'version "9.8.7"' "$formula"
grep -Fq 'XSWAP_PACKAGE_MANAGER=homebrew' "$formula"
grep -Fq 'opt/xswap/bin/xswap' "$formula"
grep -Fq 'brew upgrade xswap' "$formula"
grep -Fq 'xswap uninstall' "$formula"
