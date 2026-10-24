package brick

import (
	"strings"
	"testing"
)

// ovl2Menu is the menu the ContextMenu and Menubar tests share.
const ovl2Menu = `<MenuItem icon="pencil" shortcut="F2">Rename</MenuItem><MenuItem icon="copy">Duplicate</MenuItem><MenuSeparator /><MenuItem icon="trash" danger>Delete</MenuItem>`

// ovl2Nav is a NavigationMenu with two panels and a plain link.
const ovl2Nav = `---
nav:
  - label: Products
    columns: 2
    description: Build and ship.
    links:
      - {label: Kit, href: /kit/, icon: grid, description: 100 components.}
      - {label: Actions, href: /actions/, icon: zap, description: Server state.}
  - label: Docs
    links:
      - {label: Guide, href: /guide/, icon: file}
  - {label: Pricing, href: /pricing/}
---
<NavigationMenu :items=".Params.nav" current="/pricing/" label="Site" />`

// With JavaScript, ContextMenu opens on right-click, Shift+F10 and a long
// press, and is a keyboard menu.
func TestKitContextMenuInBrowser(t *testing.T) {
	runKitJS(t, `<ContextMenu label="File actions"><brick:prop name="target"><div>report.pdf</div></brick:prop>`+ovl2Menu+`</ContextMenu><p id="outside">o</p>{{ assets }}`, "kit_ctxmenu.mjs")
}

// With JavaScript, Menubar is one WAI-ARIA menubar over its DropdownMenus.
func TestKitMenubarInBrowser(t *testing.T) {
	runKitJS(t, `<Menubar label="Editor"><DropdownMenu label="File"><MenuItem>New file</MenuItem><MenuItem>Save</MenuItem></DropdownMenu><DropdownMenu label="Edit"><MenuItem>Undo</MenuItem><MenuItem>Redo</MenuItem></DropdownMenu><DropdownMenu label="View"><MenuItem>Zoom in</MenuItem></DropdownMenu></Menubar>{{ assets }}`, "kit_menubar.mjs")
}

// With JavaScript, NavigationMenu opens panels on hover intent and keys.
func TestKitNavigationMenuInBrowser(t *testing.T) {
	runKitJS(t, ovl2Nav+`<p id="outside">o</p>{{ assets }}`, "kit_navmenu.mjs")
}

// With JavaScript, Resizable's handles move, clamp and persist.
func TestKitResizableInBrowser(t *testing.T) {
	runKitJS(t, `<Resizable id="split" persist><ResizablePanel :size="30" :min="20" :max="60" label="Files">a</ResizablePanel><ResizablePanel :size="70" :min="30">b</ResizablePanel></Resizable>
<Resizable id="vert" direction="vertical"><ResizablePanel :size="50">a</ResizablePanel><ResizablePanel :size="50">b</ResizablePanel></Resizable>{{ assets }}`, "kit_resizable.mjs")
}

// With JavaScript, Sortable reorders by keyboard and pointer and reports;
// grouped lists trade items (kit_sortable.mjs, the second half).
func TestKitSortableInBrowser(t *testing.T) {
	runKitJS(t, `<div data-brick-state="h" data-brick-token="t"><Sortable action="move" label="Tasks">
<SortableItem :id="1" label="Write the post" :index="0">Write the post</SortableItem>
<SortableItem :id="2" label="Fix the test" :index="1">Fix the test</SortableItem>
<SortableItem :id="3" label="Design pricing" :index="2">Design pricing</SortableItem>
<SortableItem :id="4" label="Ship it" :index="3">Ship it</SortableItem>
</Sortable>
<Sortable id="todo" group="board" list="todo" label="To do" action="move"><SortableItem :id="11" label="Eleven" :index="0">Eleven</SortableItem><SortableItem :id="12" label="Twelve" :index="1">Twelve</SortableItem></Sortable>
<Sortable id="done" group="board" list="done" label="Done" action="move"><SortableItem :id="13" label="Thirteen" :index="0">Thirteen</SortableItem></Sortable>
<Sortable id="empty" group="board" list="later" label="Later" action="move"></Sortable></div>
<Sortable id="plain" direction="horizontal"><li data-id="a">A</li><li data-id="b">B</li><li data-id="c">C</li></Sortable>{{ assets }}`, "kit_sortable.mjs")
}

// The second overlay batch renders complete HTML without JavaScript: roles,
// the no-JS fallbacks (hidden menu, details panels, default sizes, move
// buttons) and the caller's attributes.
func TestKitOverlays2Render(t *testing.T) {
	t.Parallel()
	e := newEngine(t, kitOptions(t, ovl2Nav+`
<ContextMenu label="File actions" id="ctx" class="files"><brick:prop name="target"><div>report.pdf</div></brick:prop>`+ovl2Menu+`</ContextMenu>
<ContextMenu :focusable="false"><brick:prop name="target"><button>b</button></brick:prop><MenuItem>x</MenuItem></ContextMenu>
<Menubar label="Editor" id="mb"><DropdownMenu label="File"><MenuItem>New</MenuItem></DropdownMenu></Menubar>
<Resizable id="split" persist grip direction="vertical"><ResizablePanel :size="30" :min="20" label="Files">a</ResizablePanel><ResizablePanel>b</ResizablePanel></Resizable>
<Collapsible title="More" open name="g" id="col">hidden text</Collapsible>
<Collapsible :chevron="false"><brick:prop name="trigger"><b>3 more</b></brick:prop>x</Collapsible>
<Sortable action="move" label="Tasks" id="srt"><SortableItem :id="7" label="Seven" :index="0">Seven</SortableItem><SortableItem id="k" :index="1">K</SortableItem></Sortable>
<Sortable><SortableItem :id="1">no action, no buttons</SortableItem></Sortable>`))
	got := render(t, e, "content/a.html", nil)
	mustHave(t, got,
		// ContextMenu: the region gets the attributes; the menu is a hidden popover.
		`class="bk-context-menu__target files" tabindex="0" aria-haspopup="menu" id="ctx">`,
		`<div class="bk-context-menu__menu" role="menu" aria-label="File actions" popover hidden>`,
		`<div class="bk-context-menu__target" aria-haspopup="menu" >`,
		// Menubar.
		`class="bk-menubar" role="menubar" aria-label="Editor" aria-orientation="horizontal" id="mb">`,
		// NavigationMenu: details panels sharing a name, a grid of links, current marked.
		`class="bk-navmenu" aria-label="Site" >`, `<summary class="bk-navmenu__trigger">Products`,
		`style="--bk-navmenu-cols: 2"`, `<p class="bk-navmenu__intro">Build and ship.</p>`,
		`<a class="bk-navmenu__link" href="/kit/"><span class="bk-navmenu__icon">`, `<span class="bk-navmenu__desc">100 components.</span>`,
		`<a class="bk-navmenu__trigger" href="/pricing/" aria-current="page">Pricing</a>`,
		// Resizable: default sizes as flex, a separator between panels, equal share for a panel without size.
		`class="bk-resizable bk-resizable--vertical" data-step="5" data-persist id="split">`,
		`style="flex: 30 1 0px" data-min="20" data-max="90" tabindex="0" data-label="Files" role="region" aria-label="Files"`,
		`role="separator" aria-orientation="horizontal" aria-label="Resize Files" aria-valuenow="30"`, `class="bk-resizable__grip"`,
		`style="flex: 1 1 0px" data-min="10" data-max="90" tabindex="0"`,
		// Collapsible.
		`<details class="bk-collapsible" open name="g" id="col">`, `<div class="bk-collapsible__content">hidden text</div>`, `<summary class="bk-collapsible__trigger"><b>3 more</b></summary>`,
		// Sortable: data-action, the list, the live region, items with handles and move buttons.
		`class="bk-sortable bk-sortable--vertical" data-action="move" id="srt">`, `<ul class="bk-sortable__list" role="list" aria-label="Tasks">`,
		`aria-live="assertive"`, `data-id="7" data-label="Seven"`, `aria-label="Reorder Seven"`,
		`aria-label="Move Seven up" disabled>`, `brick:click="move(7, 1)"`, `brick:click="move(&#34;k&#34;, 0)"`,
	)
	if n := strings.Count(got, `<details class="bk-navmenu__details" name="`); n != 2 {
		t.Errorf("want two panels sharing a name, got %d", n)
	}
	if strings.Count(got, "bk-sortable__moves") != 2 {
		t.Errorf("move buttons render only with an action and an index")
	}
}
