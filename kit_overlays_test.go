package brick

import (
	"io/fs"
	"os"
	"path"
	"regexp"
	"strings"
	"testing"
)

// kitDialogPage is the page the Dialog jsdom test drives: every way to
// open a Dialog, a ConfirmDialog and a Drawer.
const kitDialogPage = `<Dialog id="d1" title="Edit" description="Change it.">
  <brick:prop name="trigger"><button id="t1">Open</button></brick:prop>
  <p>Body <input name="n"></p>
  <brick:prop name="footer"><button data-bk-close>Cancel</button></brick:prop>
</Dialog>
<Dialog id="d2" title="Server-opened" open><p>Edit form</p><button data-bk-close>Done</button></Dialog>
<Dialog id="d3" title="Keep" :dismissible="false"><p>x</p></Dialog>
<button id="open-d2" data-bk-open="d2">Edit</button>
<button id="open-d3" data-bk-open="d3">Keep</button>
<button id="cmd-d2" commandfor="d2" command="show-modal">Cmd</button>
<ConfirmDialog id="c1" title="Delete contact?" confirmLabel="Delete" action="remove(3)">
  <brick:prop name="trigger"><button id="t-confirm">Delete</button></brick:prop>
</ConfirmDialog>
<Drawer id="dr" title="Filters" side="left"><p>f</p></Drawer>
<button id="open-dr" data-bk-open="dr">Filters</button>
<div data-brick-state="h" data-brick-token="t"><Dialog id="d4" title="Server-owned" closeAction="close_form"><p>f</p></Dialog></div>
{{ assets }}`

// With JavaScript, dialogs open, close and return focus: the real
// rendered page and the real modules in jsdom.
func TestKitDialogInBrowser(t *testing.T) {
	runKitJS(t, kitDialogPage, "kit_dialog.mjs")
}

// mustHave fails for each want missing from got.
func mustHave(t *testing.T, got string, wants ...string) {
	t.Helper()
	for _, w := range wants {
		if !strings.Contains(got, w) {
			t.Errorf("missing %q in\n%s", w, got)
		}
	}
}

// overlaysMustLack fails for each unwanted string present in got.
func overlaysMustLack(t *testing.T, got string, unwanted ...string) {
	t.Helper()
	for _, u := range unwanted {
		if strings.Contains(got, u) {
			t.Errorf("unexpected %q in\n%s", u, got)
		}
	}
}

// With JavaScript, DropdownMenu is a keyboard menu button.
func TestKitDropdownMenuInBrowser(t *testing.T) {
	runKitJS(t, `<button id="before">x</button>
<DropdownMenu label="Options" id="m">
  <MenuLabel>Account</MenuLabel>
  <MenuItem href="/p/" icon="user">Profile</MenuItem>
  <MenuItem icon="settings" shortcut="⌘,">Settings</MenuItem>
  <MenuItem disabled>Archive</MenuItem>
  <MenuSeparator />
  <MenuItem danger>Sign out</MenuItem>
  <MenuItem>Share</MenuItem>
</DropdownMenu>
<p id="outside">o</p>{{ assets }}`, "kit_menu.mjs")
}

// With JavaScript, the Toaster adopts server-rendered toasts (on load and
// inserted later) and shows ones emitted from code.
func TestKitToasterInBrowser(t *testing.T) {
	runKitJS(t, `<Toaster /><div id="slot"><Toast variant="success" title="Saved" id="flash" :duration="300" /></div><Toast inline title="Preview" id="preview" /><Toaster id="second" />{{ assets }}`, "kit_toaster.mjs")
}

// kitCmdkPage is a CommandPalette over three groups, links and commands;
// the Components group and Building apps exercise the ranking.
const kitCmdkPage = `---
groups:
  - label: Pages
    items:
      - {label: Dashboard, href: /dashboard/, icon: layout-dashboard, keywords: home stats}
      - {label: Contacts, href: /contacts/, icon: users}
      - {label: Tasks, href: /tasks/, icon: kanban, shortcut: G T}
      - {label: Building apps, href: /building/, keywords: the data layer and tabs}
  - label: Actions
    items:
      - {label: Toggle theme, value: theme, icon: moon}
      - {label: Sign out, href: /logout/, icon: log-out}
  - label: Components
    items:
      - {label: DataTable, href: /data-table/}
      - {label: Table layouts, href: /table/, keywords: data grid}
---
<CommandPalette :groups=".Params.groups" />{{ assets }}`

// With JavaScript, the CommandPalette opens on ⌘K and is driven by keys.
func TestKitCommandPaletteInBrowser(t *testing.T) {
	runKitJS(t, kitCmdkPage, "kit_cmdk.mjs")
}

// With JavaScript, Popover places its panel (the fallback path jsdom
// exercises) and Tooltip wires aria-describedby.
func TestKitPopoverInBrowser(t *testing.T) {
	runKitJS(t, `<Popover label="Filters" id="pp" title="Filters"><button>Apply</button></Popover><p id="outside">o</p>
<Tooltip text="Delete"><button>x</button></Tooltip><Tooltip text="Synced">2m ago</Tooltip>{{ assets }}`, "kit_popover.mjs")
}

// With JavaScript, Alert and Banner dismiss, and a Banner remembers it.
func TestKitAlertInBrowser(t *testing.T) {
	runKitJS(t, `<Alert id="al" title="Saved" dismissible>Done.</Alert><Alert id="plain">Info</Alert>
<Banner id="launch" dismissible>We launched</Banner><Banner id="other" dismissible>Other</Banner>{{ assets }}`, "kit_alert.mjs")
}

// Feedback components render complete, accessible HTML without JavaScript:
// roles, values, variants, and the caller's attributes on the root.
func TestKitFeedbackRender(t *testing.T) {
	t.Parallel()
	e := newEngine(t, kitOptions(t, `<Alert variant="danger" title="Failed" id="a1" class="mt-2" data-x="1">Try again.<brick:prop name="actions"><a href="/retry">Retry</a></brick:prop></Alert>
<Alert variant="success" dismissible>Saved.</Alert>
<Alert noIcon>Plain.</Alert>
<Banner id="b1" variant="warning" dismissible>Maintenance</Banner>
<Progress :value="30" :max="60" label="Upload" showValue class="w" />
<Progress indeterminate label="Working" />
<Progress :value="150" />
<RadialProgress :value="9" :max="12" tone="success" label="Tasks" />
<Spinner size="lg" tone="primary" label="Saving" id="sp" />
<Status status="busy" pulse />
<Status status="offline" dot />
<Status tone="info">Deploying</Status>
<Toast variant="warning" title="Careful" description="Low disk." :duration="0" id="t1" />`))
	got := render(t, e, "content/a.html", nil)
	mustHave(t, got,
		// Alert: role by urgency, the variant's icon, title, body, actions; attrs on the root.
		`class="bk-alert bk-alert--danger mt-2" role="alert" id="a1" data-x="1">`,
		`<div class="bk-alert__title">Failed</div>`, `<div class="bk-alert__text">Try again.</div>`,
		`<div class="bk-alert__actions"><a href="/retry">Retry</a></div>`,
		`class="bk-alert bk-alert--success" role="status"`,
		`class="bk-alert__close" aria-label="Dismiss" hidden>`,
		// Banner: a labelled region; the close button waits for JavaScript.
		`class="bk-banner bk-banner--warning" role="region" aria-label="Announcement" id="b1">`, `class="bk-banner__close" aria-label="Dismiss" hidden>`,
		// Progress: the ARIA values, and a percentage clamped to 0–100.
		`class="bk-progress w" role="progressbar" aria-label="Upload" aria-valuemin="0" aria-valuemax="60" aria-valuenow="30" aria-valuetext="50%"`,
		`<span class="bk-progress__value">50%</span>`, `style="width: 50.00%"`,
		`class="bk-progress bk-progress--indeterminate" role="progressbar" aria-label="Working" >`,
		`style="width: 100.00%"`,
		`class="bk-radial bk-radial--success"`, `aria-valuenow="9" aria-valuetext="75%"`, `stroke-dasharray="75.00 100"`, `<span class="bk-radial__value">75%</span>`,
		`class="bk-spinner bk-spinner--lg bk-spinner--primary" role="status" aria-label="Saving" id="sp">`,
		`class="bk-status bk-status--busy bk-status--pulse" ><span class="bk-status__dot" aria-hidden="true"></span>Busy</span>`,
		`role="img" aria-label="Offline"`,
		`class="bk-status bk-status--online bk-status--info" ><span class="bk-status__dot" aria-hidden="true"></span>Deploying</span>`,
		`class="bk-toast bk-toast--warning" role="status" aria-atomic="true" data-duration="0" id="t1">`,
		`<div class="bk-toast__desc">Low disk.</div>`,
	)
	overlaysMustLack(t, got, `aria-valuenow="0" aria-valuetext="0%" >`)
	if strings.Count(got, `class="bk-alert__icon"`) != 0 || strings.Count(got, "bk-alert__icon") != 2 {
		t.Errorf("noIcon must drop the icon; the other two alerts keep theirs")
	}
	if err := renderErr(t, newEngine(t, kitOptions(t, `<Alert variant="error">x</Alert>`)), "content/a.html"); err == nil || !strings.Contains(err.Error(), "variant") {
		t.Errorf("an unknown variant must be an error: %v", err)
	}
}

// The Toaster is an aria-live region with one server-rendered toast
// template per variant, which its client module clones.
func TestKitToasterRender(t *testing.T) {
	t.Parallel()
	got := render(t, newEngine(t, kitOptions(t, `<Toaster position="top-center" :max="3" id="tt" />`)), "content/a.html", nil)
	mustHave(t, got, `class="bk-toaster bk-toaster--top-center" data-label="Notifications" data-max="3" id="tt">`,
		`<div class="bk-toaster__list" aria-live="polite" aria-relevant="additions"></div>`)
	for _, v := range []string{"neutral", "info", "success", "warning", "danger"} {
		mustHave(t, got, `<template data-variant="`+v+`">`, `bk-toast--`+v)
	}
}

// Overlays render as native elements with their roles, closed unless open
// is set, with the caller's attributes on the dialog or panel.
func TestKitOverlaysRender(t *testing.T) {
	t.Parallel()
	e := newEngine(t, kitOptions(t, `---
groups:
  - label: Pages
    items:
      - {label: Home, href: /, icon: home, shortcut: G H}
      - {label: Theme, value: theme}
---
<Dialog id="edit" title="Edit" description="Change it." size="lg" class="x" hx-target="this">
  <brick:prop name="trigger"><button>Open</button></brick:prop>
  Body
  <brick:prop name="footer"><button data-bk-close>Cancel</button></brick:prop>
</Dialog>
<Dialog title="Opened" open :dismissible="false" :closable="false">Hi</Dialog>
<ConfirmDialog id="del" title="Delete contact?" confirmLabel="Delete" action="remove(3)">Gone for good.</ConfirmDialog>
<ConfirmDialog id="close" form="close-form" variant="primary" />
<Drawer id="dr" side="left" size="sm" title="Filters" open>f</Drawer>
<Popover id="pp" label="Share" side="top" align="end" title="Share">p</Popover>
<Tooltip text="Delete" side="right"><button>x</button></Tooltip>
<DropdownMenu label="Options" id="m" align="end">
  <MenuLabel>Account</MenuLabel>
  <MenuItem href="/p/" icon="user" shortcut="⌘P">Profile</MenuItem>
  <MenuItem danger type="submit" form="f">Delete</MenuItem>
  <MenuSeparator />
  <MenuItem href="/x/" disabled>Gone</MenuItem>
</DropdownMenu>
<HoverCard side="top" align="center" :delay="200"><brick:prop name="trigger"><a href="/u/">@ada</a></brick:prop>Card</HoverCard>
<CommandPalette :groups=".Params.groups" id="cmdk" />
<CommandPalette :groups=".Params.groups" href="/sitemap/" :hotkey="false" />`))
	got := render(t, e, "content/a.html", nil)
	mustHave(t, got,
		// Dialog: a closed native dialog, labelled, attrs on the <dialog>, a no-JS close.
		`<span class="bk-dialog__trigger" data-bk-open="edit"><button>Open</button></span>`,
		`<dialog id="edit" class="bk-dialog bk-dialog--lg x" aria-labelledby="edit-title" aria-describedby="edit-desc" hx-target="this">`,
		`<h2 class="bk-dialog__title" id="edit-title">Edit</h2>`, `<form method="dialog" class="bk-dialog__x">`,
		`<footer class="bk-dialog__footer"><button data-bk-close>Cancel</button></footer>`,
		`data-dismissible="false" open >`,
		// ConfirmDialog: an alertdialog; the confirm button runs the action or submits the form.
		`<dialog id="del" class="bk-confirm bk-confirm--danger" role="alertdialog" aria-labelledby="del-title" aria-describedby="del-msg" data-dismissible="false"`,
		`<div class="bk-confirm__msg" id="del-msg">Gone for good.</div>`,
		`class="bk-btn bk-btn--danger" brick:click="remove(3)" data-bk-close="">Delete</button>`,
		`value="cancel" autofocus>Cancel</button>`,
		`<button type="submit" class="bk-btn bk-btn--primary" data-bk-close="" form="close-form">Confirm</button>`,
		// Drawer: a dialog against the left edge, open.
		`<dialog id="dr" class="bk-drawer bk-drawer--left bk-drawer--sm" aria-labelledby="dr-title" open >`,
		// Popover: native popover and popovertarget, placement classes, labelled panel.
		`popovertarget="pp" aria-controls="pp" style="anchor-name: --pp">`,
		`<div popover id="pp" class="bk-popover__panel bk-popover__panel--top bk-popover__panel--end" role="dialog" aria-label="Share"`,
		// Tooltip.
		`class="bk-tooltip bk-tooltip--right" aria-describedby="`, `class="bk-tooltip__tip" role="tooltip" id="`,
		// DropdownMenu: a details menu with menuitems, a separator and a disabled link without href.
		`<details data-brick=`, `class="bk-dropdown bk-dropdown--end" style="--bk-dropdown-anchor: --`, `-menu" id="m">`, `aria-haspopup="menu"`,
		`<div class="bk-dropdown__menu" role="menu" aria-label="Options">`,
		`<a class="bk-menu-item" role="menuitem" href="/p/" >`, `<kbd class="bk-menu-item__kbd">⌘P</kbd>`,
		`<button type="submit" class="bk-menu-item bk-menu-item--danger" role="menuitem" form="f">`,
		`class="bk-menu-sep" role="separator"`, `class="bk-menu-label" role="presentation" >Account</div>`,
		`<a class="bk-menu-item" role="menuitem" aria-disabled="true" >`,
		// HoverCard.
		`class="bk-hovercard bk-hovercard--top bk-hovercard--center" style="--bk-hc-open: 200ms; --bk-hc-close: 150ms; --bk-hc-anchor: --`,
		// CommandPalette: a trigger opening a dialog with a combobox over a grouped listbox.
		`class="bk-cmdk__trigger" commandfor="cmdk" command="show-modal"`, `<kbd class="bk-cmdk__kbd bk-cmdk__hotkey">⌘K</kbd>`,
		`<dialog id="cmdk" class="bk-cmdk__dialog" aria-label="Search" >`,
		`role="combobox" aria-expanded="true" aria-controls="cmdk-list"`, `role="listbox"`,
		`role="group" aria-labelledby="cmdk-g0" data-group="Pages"`,
		`<a class="bk-cmdk__item" role="option" aria-selected="false" id="cmdk-o0-0" href="/">`,
		`<div class="bk-cmdk__item" role="option" aria-selected="false" id="cmdk-o0-1" data-value="theme">`,
		`<a class="bk-cmdk__trigger" href="/sitemap/">`, `data-hotkey="false"`,
	)
	overlaysMustLack(t, got, `<dialog id="edit" class="bk-dialog bk-dialog--lg x" aria-labelledby="edit-title" aria-describedby="edit-desc" open`, `href="/x/"`)
	if strings.Count(got, "<dialog") != 7 {
		t.Errorf("want 7 dialogs (2 Dialog, 2 ConfirmDialog, 1 Drawer, 2 CommandPalette), got %d", strings.Count(got, "<dialog"))
	}
	// ConfirmDialog and Drawer reuse Dialog's module through a client import.
	for _, name := range []string{"ConfirmDialog", "Drawer", "CommandPalette"} {
		var src string
		for _, p := range listAssetFiles(t, e) {
			if base := path.Base(p); strings.HasPrefix(base, name+".") && strings.HasSuffix(base, ".js") {
				b, err := fs.ReadFile(e.Assets(), p)
				if err != nil {
					t.Fatal(err)
				}
				src = string(b)
			}
		}
		if !strings.Contains(src, `from "./Dialog.`) {
			t.Errorf("%s's module does not import Dialog's:\n%s", name, src)
		}
	}
}

// kitOverlayFiles are the feedback and overlay components this file tests.
var kitOverlayFiles = []string{"Alert", "Banner", "Toast", "Toaster", "Progress", "RadialProgress", "Spinner", "Status", "Dialog", "ConfirmDialog", "Drawer", "Popover", "Tooltip", "DropdownMenu", "MenuItem", "MenuSeparator", "MenuLabel", "HoverCard", "CommandPalette"}

// kitHiddenByJS lists, per component, the classes its client module hides
// by setting .hidden, which the template scan below cannot see.
var kitHiddenByJS = map[string][]string{
	"Banner":         {"bk-banner"},
	"Toaster":        {"bk-toaster"},
	"Popover":        {"bk-popover__panel"},
	"CommandPalette": {"bk-cmdk__group", "bk-cmdk__item", "bk-cmdk__empty"},
}

var (
	hiddenTagRe = regexp.MustCompile(`<[a-z]+ [^>]*class="(bk-[\w-]+)[^"]*"[^>]*\shidden[\s>]`)
	ruleRe      = regexp.MustCompile(`([^{}]+)\{([^{}]*)\}`)
)

// An element the kit hides with the hidden attribute must really hide: a
// class whose CSS sets display overrides the attribute's display: none,
// so it needs a .class[hidden] { display: none } rule. (A close button that
// shows without JavaScript, and does nothing, shipped once this way.)
func TestKitHiddenReallyHides(t *testing.T) {
	t.Parallel()
	for _, name := range kitOverlayFiles {
		b, err := os.ReadFile(path.Join(kitDir, name+".html"))
		if err != nil {
			t.Fatal(err)
		}
		src := string(b)
		var css string
		for _, m := range kitStyleRe.FindAllStringSubmatch(src, -1) {
			css += m[2]
		}
		classes := kitHiddenByJS[name]
		for _, m := range hiddenTagRe.FindAllStringSubmatch(src, -1) {
			classes = append(classes, m[1])
		}
		for _, cls := range classes {
			setsDisplay := false
			for _, r := range ruleRe.FindAllStringSubmatch(css, -1) {
				for sel := range strings.SplitSeq(r[1], ",") {
					if regexp.MustCompile(`\.`+regexp.QuoteMeta(cls)+`(\s*$|:(hover|focus))`).MatchString(strings.TrimSpace(sel)) && strings.Contains(r[2], "display:") && !strings.Contains(r[2], "display: none") && !strings.Contains(r[2], "display: contents") {
						setsDisplay = true
					}
				}
			}
			if setsDisplay && !strings.Contains(css, "."+cls+"[hidden]") {
				t.Errorf("%s: .%s sets display, so the hidden attribute does not hide it: add .%s[hidden] { display: none; }", name, cls, cls)
			}
		}
	}
}
