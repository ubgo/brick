# brick brand kit

The identity for brick: a running-bond brick wall with one amber brick, the component you drop in. Everything here is generated from `gen-brand.mjs`; edit that file (or `brand.json` for copy), never the outputs.

## Regenerate

```sh
task -t brand/Taskfile.yml all    # SVG marks + tokens, then PNG rasters (needs node, rsvg-convert, magick)
```

`png/` and `favicon.ico` are derived and safe to delete and rebuild.

## Files

| File | Use |
|---|---|
| `icon.svg`, `brick-icon.svg` | Primary indigo tile, 512 master |
| `brick-icon-dark.svg`, `brick-icon-light.svg` | Tile variants for bright and dark surfaces |
| `brick-glyph.svg` | The wall without a tile, for white backgrounds |
| `brick-mono.svg` | One colour via `currentColor`, for terminals and print |
| `favicon.svg`, `favicon.ico`, `png/favicon-*.png` | Small sizes; the bricks are large enough that no detail is dropped |
| `brick-wordmark.svg`, `brick-lockup.svg`, `brick-lockup-dark.svg` | Name and icon + name, horizontal |
| `og-cover.svg`, `og-cover-light.svg` | 1200x630 social cards |
| `png/og-social-1280.png` | 1280x640 GitHub social preview (upload by hand: Settings, General, Social preview) |
| `palette.json`, `tokens.json`, `tokens.css` | Colours, fonts and CSS variables |

## Colour and type

Tile gradient `#6366F1` to `#3730A3`, accent `#FBBF24`, foreground `#FFFFFF`. Display font Space Grotesk, body Inter, code JetBrains Mono (all OFL). The wordmark and cards declare the display font with a system fallback, so a machine without it renders the fallback.

## Don'ts

- Don't recolour outside the palette; add a tile variant instead.
- Don't stretch or rotate the mark, and don't add shadows or bevels; it is flat.
- Keep clear space of at least 25% of the icon width.
