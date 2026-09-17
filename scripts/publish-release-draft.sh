#!/bin/sh
set -eu

tag=${RELEASE_TAG:?Set RELEASE_TAG to the version tag}
repository=${GITHUB_REPOSITORY:?Set GITHUB_REPOSITORY to OWNER/REPO}
dist_dir=${DIST_DIR:-dist}
max_attempts=${UPLOAD_ATTEMPTS:-4}

printf '%s\n' "$tag" | grep -Eq '^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z.-]+)?$'
printf '%s\n' "$repository" | grep -Eq '^[A-Za-z0-9][A-Za-z0-9-]*/[A-Za-z0-9_.-]+$'
printf '%s\n' "$max_attempts" | grep -Eq '^[1-9][0-9]*$'
command -v gh >/dev/null
command -v jq >/dev/null

assets="
xswap_${tag}_darwin_amd64.tar.gz
xswap_${tag}_darwin_arm64.tar.gz
xswap_${tag}_linux_amd64.tar.gz
xswap_${tag}_linux_arm64.tar.gz
xswap_${tag}_windows_amd64.zip
xswap_${tag}_windows_arm64.zip
checksums.txt
xswap.rb"

for name in $assets; do
  test -f "$dist_dir/$name"
done

if command -v sha256sum >/dev/null 2>&1; then
  (cd "$dist_dir" && sha256sum -c checksums.txt)
else
  (cd "$dist_dir" && shasum -a 256 -c checksums.txt)
fi

sha256() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  else
    shasum -a 256 "$1" | awk '{print $1}'
  fi
}

api() {
  gh api -H 'Accept: application/vnd.github+json' -H 'X-GitHub-Api-Version: 2022-11-28' "$@"
}

releases=$(api --paginate "repos/$repository/releases?per_page=100")
published_count=$(printf '%s' "$releases" | jq --arg tag "$tag" '[.[] | select(.tag_name == $tag and .draft == false)] | length')
if test "$published_count" -ne 0; then
  echo "release $tag is already published" >&2
  exit 1
fi

draft_ids=$(printf '%s' "$releases" | jq -r --arg tag "$tag" '.[] | select(.tag_name == $tag and .draft == true) | .id')
draft_count=$(printf '%s\n' "$draft_ids" | awk 'NF {count++} END {print count+0}')
if test "$draft_count" -gt 1; then
  echo "multiple drafts exist for $tag; refusing to guess: $draft_ids" >&2
  exit 1
fi

if test "$draft_count" -eq 1; then
  release_id=$draft_ids
  echo "Resuming draft $release_id for $tag."
else
  release_id=$(api --method POST "repos/$repository/releases" \
    -f tag_name="$tag" -f name="$tag" -F draft=true -F prerelease=false \
    -F generate_release_notes=true --jq .id)
  echo "Created draft $release_id for $tag."
fi

release_assets() {
  api --paginate "repos/$repository/releases/$release_id/assets?per_page=100"
}

matching_asset() {
  release_assets | jq -c --arg name "$1" '.[] | select(.name == $name)'
}

delete_matching_assets() {
  existing=$(matching_asset "$1")
  printf '%s\n' "$existing" | jq -r 'select(length > 0) | .id' | while IFS= read -r asset_id; do
    test -n "$asset_id" && api --method DELETE "repos/$repository/releases/assets/$asset_id" >/dev/null
  done
}

asset_matches() {
  match_json=$(matching_asset "$1")
  match_count=$(printf '%s\n' "$match_json" | jq -s 'length')
  test "$match_count" -eq 1 || return 1
  remote_state=$(printf '%s\n' "$match_json" | jq -r '.state')
  remote_digest=$(printf '%s\n' "$match_json" | jq -r '.digest // ""')
  test "$remote_state" = uploaded && test "$remote_digest" = "sha256:$2"
}

for name in $assets; do
  file=$dist_dir/$name
  digest=$(sha256 "$file")
  if asset_matches "$name" "$digest"; then
    echo "Reusing verified asset $name."
    continue
  fi

  delete_matching_assets "$name"
  attempt=1
  uploaded=false
  while test "$attempt" -le "$max_attempts"; do
    echo "Uploading $name (attempt $attempt/$max_attempts)."
    if api --method POST \
      "https://uploads.github.com/repos/$repository/releases/$release_id/assets?name=$name" \
      -H 'Content-Type: application/octet-stream' --input "$file" >/dev/null; then
      uploaded=true
      break
    fi
    if asset_matches "$name" "$digest"; then
      uploaded=true
      break
    fi
    delete_matching_assets "$name"
    if test "$attempt" -lt "$max_attempts"; then
      sleep $((attempt * 2))
    fi
    attempt=$((attempt + 1))
  done

  if test "$uploaded" != true || ! asset_matches "$name" "$digest"; then
    observed=$(matching_asset "$name")
    echo "failed to upload and verify $name; expected sha256:$digest, observed: ${observed:-missing}" >&2
    exit 1
  fi
done

remote_assets=$(release_assets)
remote_count=$(printf '%s' "$remote_assets" | jq 'length')
if test "$remote_count" -ne 8; then
  echo "draft $release_id contains $remote_count assets; expected 8" >&2
  exit 1
fi

for name in $assets; do
  digest=$(sha256 "$dist_dir/$name")
  if ! asset_matches "$name" "$digest"; then
    echo "final verification failed for $name" >&2
    exit 1
  fi
done

echo "Draft $release_id contains all eight verified assets."
