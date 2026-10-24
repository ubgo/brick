package main

import (
	"net/http"
	"strings"
	"testing"
)

// The Getting started, Foundations, Actions and Layout pages render their
// live examples: the kit markup each demo is there to show.
func TestFoundationsPages(t *testing.T) {
	_, h := showcase(t)
	pages := map[string][]string{
		"/": {
			`<h1>brick kit</h1>`, `class="intro-pill"`, `bk-accent-picker`, `class="bk-card bk-card--default bk-card--sm"`,
			`href="/components/`, `roots: [components, kit/components]`, `data-brick-type="CopyButton"`,
		},
		"/theming/":                  {`<h1>Theming</h1>`, `--bk-primary: var(--bk-swatch-rose)`, `data-brick-type="AccentPicker"`, `--bk-z-dropdown`, `class="bk-prose bk-prose--sm bk-prose--wide"`},
		"/components/theme/":         {`class="theme-colors__chip" style="background: var(--bk-primary); color: var(--bk-primary-fg)"`, `--bk-radius-full`},
		"/components/icon/":          {`data-brick-type="IconGallery"`, `data-name="arrow-right"`, `data-name="zap"`, `role="img" aria-label="Private"`},
		"/components/theme-toggle/":  {`class="bk-theme-toggle"`, `localStorage.getItem(&#34;bk-theme&#34;)`},
		"/components/accent-picker/": {`role="radiogroup" aria-label="Accent colour" hidden`, `aria-label="Brand colour"`, `value="emerald"`},
		"/components/heading/":       {`<h1 class="bk-heading bk-heading--4xl bk-heading--bold"`, `<h6 class="bk-heading bk-heading--xs"`},
		"/components/text/":          {`bk-text--truncate`, `<strong class="bk-text bk-text--semibold"`, `bk-text--center`},
		"/components/link/":          {`target="_blank" rel="noopener noreferrer"`, `bk-link--always`},
		"/components/kbd/":           {`<kbd class="bk-kbd__key">⌘</kbd><kbd class="bk-kbd__key">K</kbd>`, `role="menu"`},
		"/components/code/":          {`<code class="bk-code bk-code--primary" >data-accent="emerald"</code>`},
		"/components/prose/":         {`<div class="bk-prose" >`, `<blockquote>`, `bk-prose--lg`},
		"/components/separator/":     {`bk-separator--label`, `aria-orientation="vertical"`},
		"/components/button/":        {`aria-busy="true"`, `bk-btn--block`, `aria-label="More actions"`, `aria-disabled="true"`},
		"/components/button-group/":  {`class="bk-button-group" role="group" aria-label="Text style"`, `bk-button-group--vertical`, `aria-pressed="true"`, `aria-current="page"`},
		"/components/copy-button/":   {`data-brick-type="CopyButton"`, `&#34;target&#34;:&#34;#install-cmd&#34;`, `Copy link`},
		"/components/swap/":          {`<input type="checkbox" class="bk-swap__input" aria-label="Toggle menu">`, `bk-swap--flip`, `name="follow"`},
		"/components/container/":     {`bk-container bk-container--sm`},
		"/components/stack/":         {`bk-stack bk-stack--row bk-stack--gap-sm`, `bk-stack--justify-between`},
		"/components/grid/":          {`bk-grid bk-grid--sm-2 bk-grid--lg-4`, `bk-grid__span-full`},
		"/components/card/":          {`<a href="#" class="bk-card bk-card--default"`, `class="bk-card__media"`, `class="bk-card__actions"`, `bk-card--elevated`},
		"/components/hero/":          {`<h1 class="bk-hero__title">Build pages, not pipelines</h1>`, `bk-hero--split`, `src="/demo/app.svg"`},
		"/components/page-header/":   {`aria-label="Breadcrumb"`, `bk-page-header--divider`},
		"/components/section/":       {`bk-section--center`, `aria-labelledby=`},
		"/components/footer/":        {`<footer class="bk-footer bk-footer--muted"`, `bk-footer--minimal`},
		"/components/aspect-ratio/":  {`bk-aspect-ratio--21x9`, `bk-aspect-ratio--rounded`},
		"/components/scroll-area/":   {`tabindex="0" role="region" aria-label="Release notes"`, `bk-scroll-area--horizontal`},
		"/components/indicator/":     {`>99+</span>`, `bk-indicator__badge--ping`, `bk-indicator--bottom-end`},
	}
	for url, wants := range pages {
		code, body := get(t, h, url)
		if code != http.StatusOK {
			t.Errorf("%s: status %d", url, code)
			continue
		}
		mustContain(t, url, body, wants...)
	}
	// The demo illustrations the cards and heroes use are served.
	for _, img := range []string{"/demo/aurora.svg", "/demo/dunes.svg", "/demo/forest.svg", "/demo/app.svg"} {
		if code, body := get(t, h, img); code != http.StatusOK || !strings.Contains(body, "<svg") {
			t.Errorf("%s: status %d", img, code)
		}
	}
}
