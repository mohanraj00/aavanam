# Brand kit

The mark shows three stacked sheets: the versions of a collection. The sheets use only the neutral ink tones. The dot on the top sheet is the lock: one exact point that a run can name.

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

Aavanam uses the same neutral base as the sibling projects entelos and groundgate. Each project in the family owns one accent. The accent of Aavanam is Seal, and the mark uses it only for the lock dot. Seal is a different hue from the entelos Telos green and the groundgate evidence yellow.

| Name | Hex | Use |
|---|---|---|
| Ink 900 | `#12181B` | Text on paper. The top sheet on light. The dark ground. |
| Ink 500 | `#50595E` | Secondary text. The middle sheet on light, the bottom sheet on dark. |
| Ink 300 | `#A9B0B3` | The bottom sheet on light, the middle sheet on dark. Not for text on paper. |
| Line 200 | `#D5D9D8` | Hairline borders. Never text. |
| Paper 50 | `#F5F5F0` | The light ground. |
| Light on dark | `#EEF0EC` | Text on ink. The top sheet on dark. |
| Seal | `#56C2DB` | The lock dot on the ink top sheet (light mark). |
| Seal deep | `#0B6E8A` | The lock dot on the light top sheet (dark mark). Links on paper. |

The design canvas of this kit is a private Claude Design artifact of the owner. The SVG files in this folder are the source of record.

## Type

The name and the headings use IBM Plex Sans, weight 600. Body text uses IBM Plex Sans, weight 400. Code uses IBM Plex Mono. IBM Plex is free under the SIL Open Font License.

The SVG files use live text with the fallback stack `'IBM Plex Sans', 'Helvetica Neue', Arial, sans-serif`. Thus the name looks a little different on a system without IBM Plex Sans. The files contain no outlined text and no embedded font.

## Rules

- Write the name in lowercase in the wordmark and as "Aavanam" in text.
- Keep a clear space around the mark of one sheet height, minimum.
- Do not change the colors of the sheets, rotate the mark or add effects.
