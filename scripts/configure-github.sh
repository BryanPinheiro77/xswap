#!/bin/sh
set -eu

repo=${1:?Usage: sh scripts/configure-github.sh OWNER/REPO [--solo]}
printf '%s\n' "$repo" | grep -Eq '^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$'
solo=${2:-}
if [ -n "$solo" ] && [ "$solo" != '--solo' ]; then
  echo 'The only optional flag is --solo.' >&2
  exit 1
fi
command -v gh >/dev/null
command -v jq >/dev/null
gh auth status >/dev/null

gh api --method PATCH "repos/$repo" \
  -F allow_squash_merge=true -F allow_merge_commit=false -F allow_rebase_merge=false \
  -F delete_branch_on_merge=true -F allow_auto_merge=false >/dev/null
gh api --method PUT "repos/$repo/private-vulnerability-reporting" >/dev/null

temporary=$(mktemp)
trap 'rm -f "$temporary"' EXIT HUP INT TERM
for source in .github/rulesets/*.json; do
  if [ "$solo" = '--solo' ]; then
    jq '(.rules[] | select(.type == "pull_request") | .parameters.required_approving_review_count) = 0' "$source" > "$temporary"
  else
    cp "$source" "$temporary"
  fi
  name=$(jq -r '.name' "$temporary")
  id=$(gh api --paginate "repos/$repo/rulesets" | jq -r --arg name "$name" '.[] | select(.name == $name) | .id' | head -n 1)
  if [ -n "$id" ]; then
    gh api --method PUT "repos/$repo/rulesets/$id" --input "$temporary" >/dev/null
  else
    gh api --method POST "repos/$repo/rulesets" --input "$temporary" >/dev/null
  fi
done

gh label create bug --repo "$repo" --color d73a4a --description 'A reproducible problem' --force
gh label create enhancement --repo "$repo" --color a2eeef --description 'A focused feature proposal' --force
gh label create documentation --repo "$repo" --color 0075ca --description 'Documentation improvements' --force
gh label create skip-changelog --repo "$repo" --color e4e669 --description 'Exclude from generated release notes' --force
echo "Repository settings and rulesets applied to $repo."
