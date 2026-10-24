package main

import (
	"net/http"
	"strings"
	"testing"
)

// The Data display pages render their demos, including the ones that load
// rows from the SQLite database through Starlark.
func TestDataDisplayPages(t *testing.T) {
	_, h := showcase(t)
	pages := map[string][]string{
		"/components/avatar/":           {`class="bk-avatar bk-avatar--xs`, `alt="Liam Kim (away)"`, `src="/demo/data-portrait-1.svg"`},
		"/components/avatar-group/":     {`role="group" aria-label="Project members"`, `aria-label="2 more">+2</span>`, `aria-label="45 more">+45</span>`},
		"/components/badge/":            {`bk-badge--info bk-badge--outline`, `<span class="bk-badge__dot" aria-hidden="true"></span>Active`},
		"/components/tag/":              {`<a class="bk-tag__remove" href="?drop=status"`, `aria-labelledby=`},
		"/components/stat/":             {`bk-stat__delta--good`, `bk-stat__delta--flat`},
		"/components/stats/":            {`bk-stats--joined`, `<div class="bk-stat__value">72</div>`, `<div class="bk-stat__value">$6,084</div>`},
		"/components/table/":            {`<caption class="bk-table__caption">Striped, compact and bordered</caption>`, `bk-table--sticky`, `No members yet`, `<td class="bk-table__right">9</td><td>Nora Novak</td>`, `<b>9</b>–<b>16</b> of <b>64</b>`},
		"/components/data-table/":       {`data-brick-type="DataTable"`, `name="selected" value="64"`, `<option selected>5</option>`},
		"/components/pagination/":       {`href="#page-7"`, `aria-current="page">16</a>`, `href="/search?q=brick&amp;page=5" rel="next"`, `No results`},
		"/components/list/":             {`bk-list--bordered`, `aria-current="page">Launch video.mp4</a>`, `href="#contact-`},
		"/components/list-item/":        {`bk-list-item__lead`, `<div class="bk-list-item__extra">`},
		"/components/description-list/": {`<dt>Salary expectation</dt><dd>$120,000</dd>`, `style="--bk-dl-term: 9rem"`, `<div class="bk-dl__pair"><dt>Region</dt>`},
		"/components/timeline/":         {`bk-timeline__item--warning`, `aria-label="Order history"`, `bk-timeline--sm`},
		"/components/chat-bubble/":      {`bk-chat--end`, `bk-chat__bubble--danger`, `· Delivered`},
		"/components/countdown/":        {`datetime="2027-01-01T00:00:00Z">until Jan 1, 2027 00:00</time>`, `bk-countdown--lg`},
		"/components/diff/":             {`src="/demo/data-mountain-mono.svg"`, `bk-diff--21-9`},
		"/components/code-block/":       {`<span class="bk-code-block__title">demos/demo_data.star</span>`, `bk-code-block__line--hl`, `def contacts_page(input):`},
		"/components/empty-state/":      {`bk-empty--bordered`, `No invoices yet`},
		"/components/skeleton/":         {`bk-skeleton--circle`, `aria-busy="true"`},
		"/components/tree/":             {`aria-current="page"><span class="bk-tree__spacer"></span>`, `aria-label="Repository"`},
		"/components/accordion/":        {`<details class="bk-accordion" open name="faq">`, `bk-accordion--separated`},
		"/components/carousel/":         {`bk-carousel--per-3`, `&#34;autoplay&#34;:4000`, `class="bk-track" tabindex="0">`},
		"/components/figure/":           {`bk-figure--r1-1 bk-figure--framed bk-figure--center`, `<figcaption class="bk-figure__caption">Fig. 1`},
	}
	for url, wants := range pages {
		code, body := get(t, h, url)
		if code != http.StatusOK {
			t.Errorf("%s: status %d", url, code)
			continue
		}
		mustContain(t, url, body, wants...)
		if strings.Contains(body, "ZgotmplZ") {
			t.Errorf("%s: a value was rejected by html/template (ZgotmplZ)", url)
		}
	}
}
