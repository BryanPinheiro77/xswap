# Brand assets

XSwap uses the purple mascot from the first design proposal: a charcoal body,
curled ears, ivory eyes, and small feet on a violet tile. Preserve this character
and palette when producing new assets.

## Placement

- The README displays a 112 px mascot beside the project title. Its four-second
  loop has two frontal hops with alternating foot support, followed by three
  waves of the screen-left ear tip. A `picture`
  source selects the static PNG when the reader requests reduced motion.
- The GitHub social preview uses a static card with the project name, purpose,
  supported platforms, and mascot. It is intended for shared repository links.
- These illustrations do not replace a screenshot of the terminal panel. A
  screenshot with safe example data remains separate work tracked in
  [issue #45](https://github.com/BryanPinheiro77/xswap/issues/45).

## Files

| Asset | Purpose |
| --- | --- |
| [xswap-icon.png](assets/branding/xswap-icon.png) | Canonical static mascot with transparency outside the tile. |
| [xswap-mascot.png](assets/branding/xswap-mascot.png) | 192 × 192 px static README alternative, matching the first animation frame. |
| [xswap-mascot.gif](assets/branding/xswap-mascot.gif) | 192 × 192 px, 200-frame, four-second README loop at 50 fps, approximately 2.9 MiB. |
| [github-social-preview.png](assets/branding/github-social-preview.png) | 1280 × 640 px social card, approximately 230 KiB. |
| [mascot-character-source.png](assets/branding/mascot-character-source.png) | Transparent character source for the feet. |
| [mascot-body-source.png](assets/branding/mascot-body-source.png) | Intact body layer, including the face and ears. |
| [mascot-tile-source.png](assets/branding/mascot-tile-source.png) | Stationary violet background tile. |

The illustrations were generated with OpenAI image generation from the selected
proposal. The [icon prompt](assets/branding/icon-prompt.txt),
[animation prompts](assets/branding/animation-prompt.txt), and
[tile prompt](assets/branding/tile-prompt.txt) record the production instructions.
Typography on the social card is rendered separately to keep the
project name and product description exact.

## Rebuild

These are optional maintainer tools, not application dependencies. On macOS,
Swift and AppKit render the social card with the system Avenir Next font:

```sh
swift scripts/render-social-preview.swift
```

With Swift/AppKit and FFmpeg installed, rebuild the GIF and static fallback:

```sh
sh scripts/render-mascot-gif.sh
```

The renderer samples one continuous animation from fixed artwork rather than
morphing generated poses. Feet move behind the intact body so their lift cannot
pull a notch into the belly. Damped springs provide secondary ear motion. The
loop returns to its starting pose and uses uniform 20 ms GIF frame delays.

Inspect any regenerated asset before committing it. Keep the character facing
the reader, preserve the smooth belly contour, and retain the reduced-motion
PNG source. High-resolution review exports stay outside the README; use the
small GIF here so readers do not have to download the 1080 px animation.

## Upload the social preview

Committing the card does not change the repository's social preview. A
maintainer must upload it:

1. Open the repository's **Settings → General**.
2. Under **Social preview**, choose **Edit → Upload an image**.
3. Select `docs/assets/branding/github-social-preview.png`.

GitHub recommends 1280 × 640 px and requires an image smaller than 1 MB. The
checked-in card meets both requirements. See
[GitHub's social preview documentation](https://docs.github.com/en/repositories/managing-your-repositorys-settings-and-features/customizing-your-repository/customizing-your-repositorys-social-media-preview).
