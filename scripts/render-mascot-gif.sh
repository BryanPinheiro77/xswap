#!/bin/sh
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
assets="$root/docs/assets/branding"
staging=$(mktemp -d)
trap 'rm -rf "$staging"' EXIT HUP INT TERM

for frame in open half closed ears; do
  case "$frame" in
    open) position='0:0' ;;
    half) position='iw/2:0' ;;
    closed) position='0:ih/2' ;;
    ears) position='iw/2:ih/2' ;;
  esac
  ffmpeg -hide_banner -loglevel error -y -i "$assets/mascot-frames.png" \
    -vf "crop=iw/2:ih/2:$position:exact=1,scale=192:192:flags=lanczos" \
    -frames:v 1 "$staging/$frame.png"
done

cp "$staging/open.png" "$assets/xswap-mascot.png"

cat > "$staging/loop.ffconcat" <<'EOF'
ffconcat version 1.0
file 'open.png'
duration 2.8
file 'half.png'
duration 0.08
file 'closed.png'
duration 0.12
file 'half.png'
duration 0.08
file 'open.png'
duration 1.2
file 'ears.png'
duration 0.2
file 'open.png'
duration 1.2
file 'open.png'
EOF

ffmpeg -hide_banner -loglevel error -y -safe 0 -f concat -i "$staging/loop.ffconcat" \
  -filter_complex '[0:v]split[a][b];[a]palettegen=reserve_transparent=1[p];[b][p]paletteuse=dither=sierra2_4a:diff_mode=rectangle' \
  -fps_mode vfr -loop 0 "$assets/xswap-mascot.gif"
printf 'Wrote %s\n' "$assets/xswap-mascot.gif"
