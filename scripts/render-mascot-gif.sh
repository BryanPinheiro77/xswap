#!/bin/sh
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
assets="$root/docs/assets/branding"
staging=$(mktemp -d)
trap 'rm -rf "$staging"' EXIT HUP INT TERM

# Render 200 independent frames from the continuous four-second rig.
swift -module-cache-path "$staging/swift-cache" "$root/scripts/render-mascot.swift" \
  320 50 "$staging/frames"

# A separate palette pass bounds memory while preserving a shared color table.
ffmpeg -hide_banner -loglevel error -y -framerate 50 -i "$staging/frames/frame-%04d.png" \
  -vf 'scale=192:192:flags=lanczos,palettegen=max_colors=256:reserve_transparent=1' \
  -frames:v 1 -update 1 "$staging/palette.png"
ffmpeg -hide_banner -loglevel error -y -framerate 50 -i "$staging/frames/frame-%04d.png" \
  -i "$staging/palette.png" \
  -filter_complex '[0:v]scale=192:192:flags=lanczos[scaled];[scaled][1:v]paletteuse=dither=none:diff_mode=rectangle' \
  -loop 0 -gifflags +offsetting+transdiff "$assets/xswap-mascot.gif"
ffmpeg -hide_banner -loglevel error -y -i "$staging/frames/frame-0000.png" \
  -vf scale=192:192:flags=lanczos -frames:v 1 "$assets/xswap-mascot.png"
printf 'Wrote %s and its static fallback\n' "$assets/xswap-mascot.gif"
