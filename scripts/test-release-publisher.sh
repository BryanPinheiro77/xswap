#!/bin/sh
set -eu

root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
temporary=$(mktemp -d)
trap 'rm -rf "$temporary"' EXIT HUP INT TERM
mkdir -p "$temporary/bin"
cp "$root/scripts/testdata/fake-gh.sh" "$temporary/bin/gh"
chmod +x "$temporary/bin/gh"

make_dist() {
  directory=$1
  mkdir -p "$directory"
  for name in \
    xswap_v9.9.9_darwin_amd64.tar.gz \
    xswap_v9.9.9_darwin_arm64.tar.gz \
    xswap_v9.9.9_linux_amd64.tar.gz \
    xswap_v9.9.9_linux_arm64.tar.gz \
    xswap_v9.9.9_windows_amd64.zip \
    xswap_v9.9.9_windows_arm64.zip xswap.rb; do
    printf 'fixture for %s\n' "$name" > "$directory/$name"
  done
  (
    cd "$directory"
    if command -v sha256sum >/dev/null 2>&1; then
      sha256sum ./xswap_v9.9.9_*.tar.gz ./xswap_v9.9.9_*.zip > checksums.txt
    else
      shasum -a 256 ./xswap_v9.9.9_*.tar.gz ./xswap_v9.9.9_*.zip > checksums.txt
    fi
  )
}

run_publisher() {
  PATH="$temporary/bin:$PATH" RELEASE_TAG=v9.9.9 GITHUB_REPOSITORY=owner/xswap \
    DIST_DIR=$1 FAKE_GH_STATE=$2 FAKE_FAIL_ONCE_NAME=${3:-} \
    sh "$root/scripts/publish-release-draft.sh"
}

dist=$temporary/dist
make_dist "$dist"

retry_state=$temporary/retry-state
if ! run_publisher "$dist" "$retry_state" xswap_v9.9.9_linux_amd64.tar.gz > "$temporary/retry.log" 2>&1; then
  cat "$temporary/retry.log" >&2
  exit 1
fi
test "$(cat "$retry_state/create-count")" = 1
test "$(cat "$retry_state/attempts/xswap_v9.9.9_linux_amd64.tar.gz")" = 2
test "$(find "$retry_state/assets" -type f | wc -l | tr -d ' ')" = 8
test -s "$retry_state/deleted"
grep -q 'Draft 101 contains all eight verified assets.' "$temporary/retry.log"

resume_state=$temporary/resume-state
mkdir -p "$resume_state/assets" "$resume_state/attempts"
touch "$resume_state/release"
correct=xswap_v9.9.9_darwin_amd64.tar.gz
correct_digest=$(shasum -a 256 "$dist/$correct" | awk '{print $1}')
printf '301|uploaded|sha256:%s|%s\n' "$correct_digest" "$(wc -c < "$dist/$correct" | tr -d ' ')" > "$resume_state/assets/$correct"
printf '302|uploaded|sha256:wrong|1\n' > "$resume_state/assets/xswap.rb"
if ! run_publisher "$dist" "$resume_state" > "$temporary/resume.log" 2>&1; then
  cat "$temporary/resume.log" >&2
  exit 1
fi
test ! -f "$resume_state/create-count"
test ! -f "$resume_state/attempts/$correct"
test -f "$resume_state/attempts/xswap.rb"
grep -q '^302$' "$resume_state/deleted"
grep -q "Reusing verified asset $correct." "$temporary/resume.log"

duplicate_state=$temporary/duplicate-state
if PATH="$temporary/bin:$PATH" RELEASE_TAG=v9.9.9 GITHUB_REPOSITORY=owner/xswap \
  DIST_DIR=$dist FAKE_GH_STATE=$duplicate_state FAKE_MULTIPLE_DRAFTS=1 \
  sh "$root/scripts/publish-release-draft.sh" > "$temporary/duplicate.log" 2>&1; then
  echo 'publisher accepted duplicate drafts' >&2
  exit 1
fi
grep -q 'multiple drafts exist for v9.9.9' "$temporary/duplicate.log"

echo 'release publisher tests passed'
