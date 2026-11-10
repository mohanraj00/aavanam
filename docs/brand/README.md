# Brand kit

The mark shows three stacked sheets: the versions of a collection. The dot on the top sheet is the lock: one exact point that a run can name.

## Files

| File | Use |
|---|---|
| [mark-light.svg](mark-light.svg) | The mark on a light background. |
| [mark-dark.svg](mark-dark.svg) | The mark on a dark background. |
| [wordmark-light.svg](wordmark-light.svg) | The mark and the name on a light background. The README uses it. |
| [wordmark-dark.svg](wordmark-dark.svg) | The mark and the name on a dark background. The README uses it in dark mode. |
| [social-preview.svg](social-preview.svg) | The source of the social preview. |
| [social-preview.png](social-preview.png) | The GitHub social preview, 1280 x 640. |

The SVG files are the source. The PNG is a render of `social-preview.svg`, because GitHub accepts only a raster image for a social preview. To make the PNG again after a change to the SVG, use a browser in headless mode at 1280 x 640, for example:

```sh
chrome --headless --hide-scrollbars --force-device-scale-factor=1 --window-size=1280,640 \
  --screenshot=docs/brand/social-preview.png "file://$PWD/docs/brand/social-preview.svg"
```

The owner uploads the PNG in the repository settings. That upload is an outreach step and needs owner approval.

## Palette

| Name | Light background | Dark background | Use |
|---|---|---|---|
| Ink | `#0F3D3E` | `#E8F1EF` | The top sheet, the name and the body text. |
| Teal | `#14807A` | `#4FB8AE` | The middle sheet and the tagline. |
| Mist | `#9CCFC8` | `#2E6F69` | The bottom sheet. |
| Amber | `#E3A33B` | `#F2B84B` | The lock dot. Use it only for one small accent. |
| Paper | `#F7F4EC` | `#0E1A1B` | The background. |
| Slate | `#4A5B5B` | `#A9B8B6` | Secondary text. |

## Type

The name and the headings use IBM Plex Sans, weight 600. Body text uses IBM Plex Sans, weight 400. Code uses IBM Plex Mono. IBM Plex is free under the SIL Open Font License.

The SVG files use live text with the fallback stack `'IBM Plex Sans', 'Helvetica Neue', Arial, sans-serif`. Thus the name looks a little different on a system without IBM Plex Sans. The files contain no outlined text and no embedded font.

## Rules

- Write the name in lowercase in the wordmark and as "Aavanam" in text.
- Keep a clear space around the mark of one sheet height, minimum.
- Do not change the colors of the sheets, rotate the mark or add effects.
