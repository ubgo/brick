package main

import (
	"net/http"
	"strings"
	"testing"
)

// The Navigation, Layout, Charts and Mockups pages: each renders its
// demos, and the demos that read the database show the seeded numbers.
func TestNavChartsMockupsPages(t *testing.T) {
	_, h := showcase(t)
	pages := map[string][]string{
		"/components/tabs/": {
			`role="tablist" aria-label="Install"`, `bk-tabs--pills`, `bk-tabs--boxed`, `bk-tabs--segmented`,
			`<span class="bk-tabs__badge">12</span>`, `disabled>`, `bk-tabs--full`,
			`<a class="bk-tabs__tab" href="/components/tabs/" aria-current="page">Tabs</a>`,
		},
		"/components/segmented-control/": {`type="radio" name="view" value="Board" checked>`, `aria-label="Align left"`, `<form action="/components/segmented-control/" method="get"`},
		"/components/breadcrumbs/":       {`<li aria-current="page">Breadcrumbs</li>`, `bk-breadcrumbs--slash`, `<summary aria-label="Show 3 more">`},
		"/components/navbar/":            {`class="bk-navbar__nav"`, `popover aria-label="Site"`, `href="/components/navbar/" aria-current="page"`},
		"/components/sidebar/":           {`href="/apps/tasks/" aria-current="page"`, `bk-sidebar--collapsed`, `aria-current="page" title="Inbox"`},
		"/components/sidebar-group/":     {`<details class="bk-sidebar-group" open >`, `<details class="bk-sidebar-group" >`},
		"/components/sidebar-item/":      {`href="/docs/guide/" aria-current="page"`, `<details class="bk-sidebar-item__sub" open >`},
		"/components/app-shell/":         {`class="bk-appshell"`, `popover>`, `<div class="bk-appshell__main">`},
		"/components/steps/":             {`aria-current="step"`, `bk-step--error`, `bk-steps--vertical`},
		"/components/menu/":              {`href="/settings/billing/" aria-current="page"`, `<span class="bk-menu__badge">New</span>`},
		"/components/dock/":              {`href="/home/" aria-current="page"`, `<span class="bk-dock__badge">3</span>`},
		"/components/bar-chart/": {
			`aria-label="Desktop visitors per month"`, `bk-bar-chart--stacked`, `bk-bar-chart--horizontal`,
			// From the database: paid revenue in June, and the stacked status counts.
			`title="Jun&#10;Revenue: $1,349"`, `data-tones="success warning danger"`,
		},
		"/components/line-chart/":      {`class="bk-line-chart__line"`, `class="bk-line-chart__area"`, `bk-line-chart--nopoints`, `aria-label="Paid orders per month"`},
		"/components/donut-chart/":     {`<span class="bk-donut-chart__value">120</span>`, `data-tone="success"`, `stroke-width="92.000"`},
		"/components/sparkline/":       {`aria-label="Signups: 4 to 15, low 4, high 15"`, `bk-sparkline__bars`},
		"/components/chart-legend/":    {`bk-chart-legend--line`, `bk-chart-legend--dot bk-chart-legend--column`, `<span class="bk-chart-legend__pct">`},
		"/components/browser-mockup/":  {`<figure class="bk-browser"`, `acme.com/pricing</span>`},
		"/components/phone-mockup/":    {`class="bk-phone bk-phone--notch"`, `<span>12:30</span>`},
		"/components/window-mockup/":   {`bk-window--windows`, `bk-window__lights`},
		"/components/terminal-mockup/": {`<span class="bk-terminal__prompt">$</span><span class="bk-terminal__text">brick serve</span>`, `bk-terminal--typing`},
	}
	for url, wants := range pages {
		code, body := get(t, h, url)
		if code != http.StatusOK {
			t.Errorf("%s: status %d", url, code)
			continue
		}
		mustContain(t, url, body, wants...)
		if strings.Contains(body, "brick-error") {
			t.Errorf("%s shows a render error", url)
		}
	}
}
