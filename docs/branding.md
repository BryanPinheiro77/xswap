# Brand assets

XSwap uses the purple mascot from the first design proposal: a charcoal body,
curled ears, ivory eyes, and small feet on a violet tile. Preserve this character
and palette when producing new assets.

## Placement

- The README displays a 112 px mascot beside the project title. Its short loop
  blinks and moves the ears, with long pauses between movements. A `picture`
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
| [xswap-mascot.gif](assets/branding/xswap-mascot.gif) | 192 × 192 px, eight-frame, 5.72-second README loop, approximately 153 KiB. |
| [github-social-preview.png](assets/branding/github-social-preview.png) | 1280 × 640 px social card, approximately 230 KiB. |
| [mascot-frames.png](assets/branding/mascot-frames.png) | Four animation poses, arranged in a two-by-two grid. |

The illustrations were generated with OpenAI image generation from the selected
proposal. The [icon prompt](assets/branding/icon-prompt.txt) and
[animation prompt](assets/branding/animation-prompt.txt) record the production
instructions. Typography on the social card is rendered separately to keep the
project name and product description exact.

## Rebuild

These are optional maintainer tools, not application dependencies. On macOS,
Swift and AppKit render the social card with the system Avenir Next font:

```sh
swift scripts/render-social-preview.swift
```

With FFmpeg installed, rebuild the GIF from the checked-in animation poses:

```sh
sh scripts/render-mascot-gif.sh
```

Inspect any regenerated asset before committing it. Keep the README animation
small, preserve the long pauses, and retain the reduced-motion PNG source.

## Upload the social preview

Committing the card does not change the repository's social preview. A
maintainer must upload it:

1. Open the repository's **Settings → General**.
2. Under **Social preview**, choose **Edit → Upload an image**.
3. Select `docs/assets/branding/github-social-preview.png`.

GitHub recommends 1280 × 640 px and requires an image smaller than 1 MB. The
checked-in card meets both requirements. See
[GitHub's social preview documentation](https://docs.github.com/en/repositories/managing-your-repositorys-settings-and-features/customizing-your-repository/customizing-your-repositorys-social-media-preview).
