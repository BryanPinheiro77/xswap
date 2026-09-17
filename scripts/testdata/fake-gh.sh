#!/bin/sh
set -eu

test "${1:-}" = api
shift
method=GET
input=
jq_filter=
endpoint=
while test "$#" -gt 0; do
  case "$1" in
    -H|--header|-f|--raw-field|-F|--field)
      shift 2
      ;;
    --method)
      method=$2
      shift 2
      ;;
    --input)
      input=$2
      shift 2
      ;;
    --jq)
      jq_filter=$2
      shift 2
      ;;
    --paginate)
      shift
      ;;
    *)
      endpoint=$1
      shift
      ;;
  esac
done

state=${FAKE_GH_STATE:?}
mkdir -p "$state/assets" "$state/attempts"

sha256() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  else
    shasum -a 256 "$1" | awk '{print $1}'
  fi
}

assets_json() {
  first=true
  printf '['
  for file in "$state"/assets/*; do
    test -f "$file" || continue
    IFS='|' read -r id asset_state digest size < "$file"
    if test "$first" = true; then first=false; else printf ','; fi
    jq -cn --argjson id "$id" --arg name "$(basename "$file")" \
      --arg state "$asset_state" --arg digest "$digest" --argjson size "$size" \
      '{id:$id,name:$name,state:$state,digest:$digest,size:$size}'
  done
  printf ']'
}

case "$method:$endpoint" in
  "GET:repos/owner/xswap/releases?per_page=100")
    if test "${FAKE_MULTIPLE_DRAFTS:-}" = 1; then
      printf '[{"id":101,"tag_name":"v9.9.9","draft":true},{"id":102,"tag_name":"v9.9.9","draft":true}]'
    elif test -f "$state/release"; then
      printf '[{"id":101,"tag_name":"v9.9.9","draft":true}]'
    else
      printf '[]'
    fi
    ;;
  "POST:repos/owner/xswap/releases")
    touch "$state/release"
    count=0
    test ! -f "$state/create-count" || count=$(cat "$state/create-count")
    count=$((count + 1))
    printf '%s\n' "$count" > "$state/create-count"
    if test "$jq_filter" = .id; then printf '101\n'; else printf '{"id":101}'; fi
    ;;
  "GET:repos/owner/xswap/releases/101/assets?per_page=100")
    assets_json
    ;;
  DELETE:repos/owner/xswap/releases/assets/*)
    asset_id=${endpoint##*/}
    for file in "$state"/assets/*; do
      test -f "$file" || continue
      IFS='|' read -r id _ < "$file"
      if test "$id" = "$asset_id"; then
        rm "$file"
        printf '%s\n' "$asset_id" >> "$state/deleted"
      fi
    done
    ;;
  POST:https://uploads.github.com/repos/owner/xswap/releases/101/assets?name=*)
    name=${endpoint##*name=}
    attempt=0
    test ! -f "$state/attempts/$name" || attempt=$(cat "$state/attempts/$name")
    attempt=$((attempt + 1))
    printf '%s\n' "$attempt" > "$state/attempts/$name"
    id=$((200 + $(find "$state/assets" -type f | wc -l | tr -d ' ')))
    if test "$name" = "${FAKE_FAIL_ONCE_NAME:-}" && test "$attempt" -eq 1; then
      printf '%s|starter||0\n' "$id" > "$state/assets/$name"
      echo 'simulated upload failure' >&2
      exit 1
    fi
    digest=$(sha256 "$input")
    size=$(wc -c < "$input" | tr -d ' ')
    printf '%s|uploaded|sha256:%s|%s\n' "$id" "$digest" "$size" > "$state/assets/$name"
    jq -cn --argjson id "$id" --arg name "$name" --arg digest "sha256:$digest" \
      --argjson size "$size" '{id:$id,name:$name,state:"uploaded",digest:$digest,size:$size}'
    ;;
  *)
    echo "unsupported fake gh call: $method $endpoint" >&2
    exit 2
    ;;
esac
