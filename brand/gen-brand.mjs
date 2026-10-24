// satl brand kit generator — single source of truth for one repo's identity.
// Run: node gen-brand.mjs  → writes all SVG marks + design tokens into this dir.
// Rasterize with the sibling Taskfile (`task raster`).
//
// HOW TO ADAPT for a new repo:
//   1. Fill M (the manifest) from the repo's brand.json.
//   2. Swap the GLYPH() body for this project's concept (the only per-brand art).
//      Everything else — tiles, variants, favicon, wordmark, lockups, OG, tokens —
//      is generic and reflows from M automatically.
import { writeFileSync } from "node:fs"
import { dirname } from "node:path"
import { fileURLToPath } from "node:url"

const OUT = dirname(fileURLToPath(import.meta.url))

// ── Manifest (from brand.json) ───────────────────────────────────────────────
const M = {
  name: "brick",
  tile: ["#6366F1", "#3730A3"],     // [gradientStart, gradientEnd] — the color tile
  fg: "#FFFFFF",                     // glyph foreground on the color tile
  accent: "#FBBF24",                 // spark / satellite / highlight
  radius: 112,                       // tile corner radius on the 512 master
  fonts: { display: "'Space Grotesk', 'Geist', ui-sans-serif, system-ui, sans-serif" },
  // derived dark/light tiles + glyph foregrounds (tweak if your palette needs it)
  dark: ["#312E81", "#1E1B4B"], darkFg: "#EEF2FF",
  light: ["#EEF2FF", "#E0E7FF"], lightFg: "#3730A3", lightAccent: "#D97706",
  glyphAccent: "#D97706",            // accent for the no-tile glyph on white
}

// ── GLYPH (the ONLY per-concept art — swap this body per project) ────────────
// Draw inside a 0..512 box. `fg` fills the mark, `accent` the highlight.
// `detail:false` is the favicon path — drop thin/small detail for 16px legibility.
// Concept: a running-bond brick wall with one accent brick (the component you drop in).
function GLYPH(fg, accent) {
  // Running-bond brick wall: three rows, the middle one offset by half a brick, with one
  // accent brick (the "component" you dropped in). Same geometry in every variant, so the
  // favicon needs no detail removed: the bricks are already large enough at 16px.
  return `<g fill="${fg}">
    <rect x="112" y="156" width="136" height="56" rx="12"/>
    <rect x="264" y="156" width="136" height="56" rx="12"/>
    <rect x="112" y="228" width="56" height="56" rx="12"/>
    <rect x="344" y="228" width="56" height="56" rx="12"/>
    <rect x="112" y="300" width="136" height="56" rx="12"/>
    <rect x="264" y="300" width="136" height="56" rx="12"/>
  </g>
  <rect x="184" y="228" width="144" height="56" rx="12" fill="${accent}"/>`
}

// ── Generic machinery (rarely needs editing) ─────────────────────────────────
const grad = (id, a, b, x2 = 512, y2 = 512) =>
  `<linearGradient id="${id}" x1="0" y1="0" x2="${x2}" y2="${y2}" gradientUnits="userSpaceOnUse"><stop stop-color="${a}"/><stop offset="1" stop-color="${b}"/></linearGradient>`
const open = (w, h, label) =>
  `<svg width="${w}" height="${h}" viewBox="0 0 ${w} ${h}" fill="none" xmlns="http://www.w3.org/2000/svg" role="img" aria-label="${label}">`
const W = (f, s) => writeFileSync(`${OUT}/${f}`, s)

const iconFile = (id, a, b, fg, accent, opts) =>
  `${open(512, 512, M.name)}
  <defs>${grad(id, a, b)}</defs>
  <rect width="512" height="512" rx="${M.radius}" fill="url(#${id})"/>
  ${GLYPH(fg, accent, opts)}
</svg>
`

const n = M.name
W(`${n}-icon.svg`,       iconFile("g-color", M.tile[0], M.tile[1], M.fg, M.accent))
W("icon.svg",            iconFile("g-c2",    M.tile[0], M.tile[1], M.fg, M.accent))
W(`${n}-icon-dark.svg`,  iconFile("g-dark",  M.dark[0], M.dark[1], M.darkFg, M.accent))
W(`${n}-icon-light.svg`, iconFile("g-light", M.light[0], M.light[1], M.lightFg, M.lightAccent))
W("favicon.svg",         iconFile("g-fav",   M.tile[0], M.tile[1], M.fg, M.accent, { detail: false }))
W(`${n}-glyph.svg`,      `${open(512, 512, `${n} glyph`)}\n  ${GLYPH(M.lightFg, M.glyphAccent)}\n</svg>\n`)
W(`${n}-mono.svg`,       `${open(512, 512, n)}\n  ${GLYPH("currentColor", "currentColor")}\n</svg>\n`)

const FONT = M.fonts.display
const wordmark = (fill) =>
  `<text x="0" y="104" font-family="${FONT}" font-size="132" font-weight="600" letter-spacing="-6" fill="${fill}">${n}</text>`
W(`${n}-wordmark.svg`, `${open(300, 140, n)}\n  ${wordmark("#0B0B12")}\n</svg>\n`)

const lockup = (id, textFill) =>
  `${open(620, 200, n)}
  <defs>${grad(id, M.tile[0], M.tile[1])}</defs>
  <g transform="translate(20,36) scale(0.25)">
    <rect width="512" height="512" rx="${M.radius}" fill="url(#${id})"/>
    ${GLYPH(M.fg, M.accent)}
  </g>
  <text x="176" y="132" font-family="${FONT}" font-size="116" font-weight="600" letter-spacing="-5" fill="${textFill}">${n}</text>
</svg>
`
W(`${n}-lockup.svg`,      lockup("l-color", "#0B0B12"))
W(`${n}-lockup-dark.svg`, lockup("l-dark", M.light[0]))

const og = (file, bgA, bgB, main, sub1, sub2, line, tagline, subline) =>
  W(file, `${open(1200, 630, n)}
  <defs>${grad("og-bg", bgA, bgB, 1200, 630)}${grad("og-ic", M.tile[0], M.tile[1])}</defs>
  <rect width="1200" height="630" fill="url(#og-bg)"/>
  <g transform="translate(96,180) scale(0.52)"><rect width="512" height="512" rx="${M.radius}" fill="url(#og-ic)"/>${GLYPH(M.fg, M.accent)}</g>
  <text x="412" y="292" font-family="${FONT}" font-size="132" font-weight="600" letter-spacing="-5" fill="${main}">${n}</text>
  <rect x="416" y="324" width="86" height="8" rx="4" fill="${line}"/>
  <text x="414" y="392" font-family="${FONT}" font-size="40" font-weight="500" fill="${sub1}">${tagline}</text>
  <text x="414" y="446" font-family="${FONT}" font-size="29" font-weight="400" fill="${sub2}">${subline}</text>
</svg>
`)
const TAGLINE = "Server-rendered HTML components for Go"
const SUBLINE = "On html/template · no build step · no new language"
og("og-cover.svg",       M.dark[0], M.dark[1], "#FFFFFF", M.light[1], M.light[0], M.accent, TAGLINE, SUBLINE)
og("og-cover-light.svg", M.light[0], M.light[1], M.dark[1], M.dark[0], M.lightFg, M.lightFg, TAGLINE, SUBLINE)

W("tokens.css",
`:root{
  --${n}-tile-a:${M.tile[0]}; --${n}-tile-b:${M.tile[1]};
  --${n}-accent:${M.accent}; --${n}-fg:${M.fg};
  --${n}-font-display:${FONT};
  --${n}-font-sans:'Inter', ui-sans-serif, system-ui, sans-serif;
  --${n}-font-mono:'JetBrains Mono', ui-monospace, SFMono-Regular, Menlo, monospace;
  --${n}-radius:16px;
}
[data-theme="dark"], .dark{ --${n}-bg:${M.dark[1]}; --${n}-fg:${M.light[0]}; }
`)

W("palette.json", `${JSON.stringify({ name: n, tile: M.tile, accent: M.accent, fg: M.fg, dark: M.dark, light: M.light, glyphAccent: M.glyphAccent }, null, 2)}\n`)
W("tokens.json", `${JSON.stringify({ name: n, color: { tile: M.tile, accent: M.accent }, font: { display: "Space Grotesk", sans: "Inter", mono: "JetBrains Mono" }, radius: { tile: `${M.radius}@512` }, icon: { favicon: [16, 32, 48, 180, 512], og: [1200, 630] } }, null, 2)}\n`)

console.log(`✓ ${n} brand kit written to ${OUT}`)
