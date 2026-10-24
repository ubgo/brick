package main

import (
	"net/http"
	"regexp"
	"strings"
	"testing"
)

// The second overlay batch's pages render their live examples.
func TestOverlays2Pages(t *testing.T) {
	_, h := showcase(t)
	pages := map[string][]string{
		"/components/context-menu/":    {`class="bk-context-menu__target" tabindex="0" aria-haspopup="menu"`, `role="menu" aria-label="Canvas actions" popover hidden`, `brick:click="star(1)"`, `Restore the files`},
		"/components/menubar/":         {`role="menubar" aria-label="Editor"`, `<kbd class="bk-menu-item__kbd">⇧⌘S</kbd>`},
		"/components/navigation-menu/": {`class="bk-navmenu" aria-label="Site"`, `<span class="bk-navmenu__desc">Server state from a click, patched in place.</span>`, `--bk-navmenu-cols: 1`, `aria-current="page">Pricing</a>`},
		"/components/resizable/":       {`id="kit-demo-editor"`, `data-persist`, `aria-label="Resize Files"`, `bk-resizable--vertical`, `class="bk-resizable__grip"`},
		"/components/resizable-panel/": {`<a href="/components/resizable/">`, `data-label="Editor"`},
		"/components/collapsible/":     {`<summary class="bk-collapsible__trigger">`, `Advanced options`, `Show 3 more`},
		"/components/sortable/":        {`data-action="move"`, `aria-label="Reorder Write the launch post"`, `Order on the server: 1, 2, 3, 4, 5`, `bk-sortable--horizontal`, `data-id="E-mail"`},
		"/components/sortable-item/":   {`<a href="/components/sortable/">`},
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

// ovl2Order lists the Sortable demo's item ids in rendered order.
func ovl2Order(html string) string {
	var ids []string
	for _, m := range regexp.MustCompile(`class="bk-sortable__item" data-id="(\d+)"`).FindAllStringSubmatch(html, -1) {
		ids = append(ids, m[1])
	}
	return strings.Join(ids, ",")
}

// The stateful demos' actions: Sortable's move puts an item at an index
// (clamped), and the ContextMenu demo stars and deletes files.
func TestOverlays2Actions(t *testing.T) {
	_, h := showcase(t)
	// The Sortable page has two stateful demos (priorities, then the grouped
	// board): act on a fresh copy of the first each time.
	sortable := func(args ...any) string {
		s, _ := dashOpen(t, h, "/components/sortable/")
		return s.act("move", args...)
	}
	out := sortable(1, 2)
	if got := ovl2Order(out); got != "2,3,1,4,5" {
		t.Errorf("move(1, 2): order %s", got)
	}
	mustContain(t, "move", out, "Order on the server: 2, 3, 1, 4, 5 · 1 moves", `brick:click="move(1, 1)"`)
	out = sortable(5, 99)
	if got := ovl2Order(out); got != "1,2,3,4,5" {
		t.Errorf("move(5, 99) clamps to the end: order %s", got)
	}
	out = overlayAction(t, h, "/components/context-menu/", "star", 2)
	if strings.Count(out, `aria-label="Starred"`) != 2 {
		t.Errorf("star(2): want two starred files\n%s", out)
	}
	out = overlayAction(t, h, "/components/context-menu/", "remove", 2)
	if strings.Contains(out, "Pricing.xlsx") {
		t.Error("remove(2) left the file")
	}
}
