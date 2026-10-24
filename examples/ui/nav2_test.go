package main

import (
	"context"
	"net/http"
	"regexp"
	"strings"
	"testing"
)

// The second navigation batch's pages render their demos.
func TestNav2Pages(t *testing.T) {
	_, h := showcase(t)
	pages := map[string][]string{
		"/components/toggle/":           {`class="bk-toggle bk-toggle--ghost bk-toggle--icon" title="Bold"`, `name="pinned" value="on"`},
		"/components/toggle-group/":     {`type="radio" name="align" value="left" checked aria-label="Align left"`, `type="checkbox" name="format" value="bold" checked`},
		"/components/callout/":          {`bk-callout--danger`, `<p class="bk-callout__title">Billing failed</p>`, `bk-callout__actions`},
		"/components/time-picker/":      {`type="time"`, `>2:15 PM</li>`, `min="08:00" max="12:00"`},
		"/components/multi-select/":     {`<select multiple class="bk-multiselect__native"`, `<option value="Go" selected>Go</option>`, `<option value="u2" selected>Maya Chen</option>`},
		"/components/editor/":           {`class="bk-editor__source"`, `role="toolbar"`, `brick:submit="save"`},
		"/components/filter-chips/":     {`type="radio" name="status" value="Open" checked`, `bk-filter--collapse`, `<input type="reset" class="bk-filter__clear" value="Clear">`},
		"/components/fab/":              {`<details class="bk-fab__dial">`, `bk-fab--bottom-start`},
		"/components/calendar-heatmap/": {`class="bk-heatmap"`, `commits from Sep 21 2025`, `bk-heatmap--primary`},
		"/components/brand/":            {`<span class="bk-brand__sub">Workspace</span>`, `aria-label="Acme"`},
		"/components/profile/":          {`<span class="bk-profile__email">ada@example.com</span>`, `bk-profile--compact`},
	}
	for url, wants := range pages {
		code, body := get(t, h, url)
		if code != http.StatusOK {
			t.Errorf("%s: status %d", url, code)
			continue
		}
		mustContain(t, url, body, wants...)
	}
}

// The Editor demo's server side: whatever HTML the form submits, the page
// shows it only through sanitize — the script, the handler and the
// javascript: link are gone, safe formatting stays.
func TestNav2EditorSanitizes(t *testing.T) {
	_, h := showcase(t)
	_, page := get(t, h, "/components/editor/")
	i := strings.Index(page, `brick:submit="save"`)
	m := regexp.MustCompile(`data-brick-state="([^"]+)" data-brick-token="([^"]+)"`).FindAllStringSubmatchIndex(page[:i], -1)
	if len(m) == 0 {
		t.Fatal("no stateful editor demo")
	}
	last := m[len(m)-1]
	s := &dashSession{t: t, h: h, handle: page[last[2]:last[3]], token: page[last[4]:last[5]]}
	preview := func(out string) string {
		j := strings.Index(out, `class="bk-editor-demo">`)
		if j < 0 {
			t.Fatalf("no preview in\n%s", out)
		}
		return out[j : j+strings.Index(out[j:], "</div>")]
	}
	got := preview(page)
	mustContain(t, "seeded", got, `<b>bold</b>`, `href="https://example.com"`, `sneaky link`)
	for _, bad := range []string{"<script", "onclick", "javascript:"} {
		if strings.Contains(got, bad) {
			t.Errorf("seeded preview keeps %q: %s", bad, got)
		}
	}
	got = preview(s.act("save", map[string]string{"body": `<h2>Hi</h2><p onmouseover="x()">a <i>b</i><iframe src="//evil"></iframe></p><img src=x onerror=alert(1)>`}))
	mustContain(t, "submitted", got, `<h2>Hi</h2>`, `<i>b</i>`)
	for _, bad := range []string{"onmouseover", "iframe", "onerror"} {
		if strings.Contains(got, bad) {
			t.Errorf("submitted preview keeps %q: %s", bad, got)
		}
	}
}

var nav2CellRe = regexp.MustCompile(`<i class="bk-heatmap__cell( bk-heatmap__cell--out)?"[^>]*?(?: data-level="(\d)" data-date="([\d-]+)" title="([^"]+)")?></i>`)

// CalendarHeatmap's date arithmetic (Starlark): the grid ends on the week
// holding end, values add up per day and shade in quarters of the busiest
// day, days after end are blank, and months are labelled where they start.
func TestNav2CalendarHeatmap(t *testing.T) {
	s, _ := showcase(t)
	heat := func(props map[string]any) (string, [][]string) {
		t.Helper()
		b, err := s.Engine().RenderComponent(context.Background(), "CalendarHeatmap", props, "")
		if err != nil {
			t.Fatal(err)
		}
		var cells [][]string
		for _, m := range nav2CellRe.FindAllStringSubmatch(string(b), -1) {
			if !strings.Contains(m[0], `style="grid-column`) {
				continue // the legend's swatches
			}
			cells = append(cells, m[1:])
		}
		return string(b), cells
	}
	data := []any{
		map[string]any{"date": "2026-09-01", "value": 4},
		map[string]any{"date": "2026-09-02", "value": 1},
		map[string]any{"date": "2026-09-02", "value": 1},
		map[string]any{"date": "2026-09-03", "value": 0},
	}
	// 2026-09-05 is a Saturday: two whole weeks, Sun Aug 23 … Sat Sep 5.
	out, cells := heat(map[string]any{"data": data, "end": "2026-09-05", "weeks": 2, "unit": "commits"})
	if len(cells) != 14 || cells[0][2] != "2026-08-23" || cells[13][2] != "2026-09-05" {
		t.Fatalf("cells: %v", cells)
	}
	level := map[string]string{}
	for _, c := range cells {
		level[c[2]] = c[1]
	}
	if level["2026-09-01"] != "4" || level["2026-09-02"] != "2" || level["2026-09-03"] != "0" || level["2026-08-30"] != "0" {
		t.Errorf("levels: %v", level)
	}
	mustContain(t, "heatmap", out,
		`title="2 commits on Wed, Sep 2 2026"`, `title="No commits on Sun, Aug 30 2026"`,
		`<span class="bk-heatmap__month" style="grid-column: 3" aria-hidden="true">Sep</span>`,
		`6 commits from Aug 23 2026; the busiest day was Sep 1 with 4.`, `--bk-heatmap-weeks: 2`,
		`style="grid-row: 3" aria-hidden="true">Mon</span>`)
	// A Monday start moves the rows; a Wednesday end leaves Thu–Sun blank.
	_, cells = heat(map[string]any{"data": data, "end": "2026-09-02", "weeks": 1, "weekStart": "monday"})
	var out3 int
	for _, c := range cells {
		if c[0] != "" {
			out3++
		}
	}
	if len(cells) != 7 || cells[0][2] != "2026-08-31" || out3 != 4 {
		t.Errorf("Monday start, Wednesday end: %v", cells)
	}
	// By default the grid ends on the latest date in data.
	_, cells = heat(map[string]any{"data": data, "weeks": 1})
	if cells[6][0] == "" || cells[4][2] != "2026-09-03" {
		t.Errorf("default end: %v", cells)
	}
	out, cells = heat(map[string]any{})
	if len(cells) != 0 || !strings.Contains(out, "No contributions.") {
		t.Errorf("empty: %v\n%s", cells, out)
	}
}

// Headings never skip a level (axe heading-order): each example is an h2
// under the page's h1, the dashboard's cards are h2, and the task board's
// columns are h2 with h3 cards.
func TestNav2HeadingOutline(t *testing.T) {
	_, h := showcase(t)
	pages := map[string][]string{
		"/components/card/": {`<h2 class="ex__title">Title, body, footer and actions</h2>`, `<h3 class="bk-card__title">Invite your team</h3>`},
		"/apps/dashboard/":  {`<h2 class="bk-card__title">Revenue</h2>`},
		"/apps/tasks/":      {`<h2 class="tb-col__title">To do</h2>`, `<h3 class="tb-card__title">Write the launch post</h3>`},
		"/apps/settings/":   {`<h2 class="bk-card__title">Profile</h2>`},
	}
	for url, wants := range pages {
		_, body := get(t, h, url)
		mustContain(t, url, body, wants...)
	}
}
