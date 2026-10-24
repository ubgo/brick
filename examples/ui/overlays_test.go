package main

import (
	"net/http"
	"regexp"
	"strings"
	"testing"
)

// The Feedback and Overlays pages render their live examples: the kit
// markup each demo is there to show.
func TestOverlaysPages(t *testing.T) {
	_, h := showcase(t)
	pages := map[string][]string{
		"/components/alert/":           {`class="bk-alert bk-alert--danger" role="alert"`, `class="bk-alert bk-alert--info" role="status"`, `<div class="bk-alert__actions">`, `class="bk-alert__close" aria-label="Dismiss" hidden`, `data-brick-type="Alert"`},
		"/components/banner/":          {`class="bk-banner bk-banner--warning" role="region" aria-label="Maintenance notice"`, `id="kit-demo-launch"`, `class="bk-banner__close" aria-label="Dismiss" hidden`, `bk-banner:kit-demo-launch`},
		"/components/toast/":           {`class="bk-toast bk-toast--danger bk-toast--inline" role="alert"`, `data-toast="success"`, `data-brick-type="ToastFromCode"`, `brick:click="save"`, `class="bk-toaster bk-toaster--bottom-right"`, `aria-live="polite"`},
		"/components/toaster/":         {`<a href="/components/toast/">`, `data-brick-type="Toaster"`, `<template data-variant="danger">`},
		"/components/progress/":        {`role="progressbar" aria-label="Uploading report.pdf" aria-valuemin="0" aria-valuemax="100" aria-valuenow="32"`, `<span class="bk-progress__value">60%</span>`, `bk-progress--indeterminate`, `bk-progress--lg bk-progress--danger`},
		"/components/radial-progress/": {`class="bk-radial bk-radial--info"`, `<span class="bk-radial__value">9/12</span>`, `aria-valuenow="95" aria-valuetext="95%"`},
		"/components/spinner/":         {`class="bk-spinner bk-spinner--xl" role="status" aria-label="Loading"`, `aria-label="Loading comments"`, `bk-spinner--danger`},
		"/components/status/":          {`bk-status--away`, `>In a meeting until 15:00</span>`, `bk-status--info bk-status--pulse`, `role="img" aria-label="Busy"`},
		"/components/dialog/":          {`<dialog id="demo-terms" class="bk-dialog bk-dialog--lg" aria-labelledby="demo-terms-title"`, `data-bk-open="demo-terms"`, `commandfor="demo-terms" command="show-modal"`, `<form method="dialog" class="bk-dialog__x">`, `bk-dialog--full`, `brick:submit="save"`, `data-dismissible="false"`},
		"/components/confirm-dialog/":  {`<dialog id="demo-del-2" class="bk-confirm bk-confirm--danger" role="alertdialog"`, `brick:click="remove(2)"`, `Grace Hopper will be removed from your contacts.`, `form="demo-close-account">Close account</button>`, `bk-confirm--primary`},
		"/components/drawer/":          {`class="bk-drawer bk-drawer--left bk-drawer--md"`, `class="bk-drawer bk-drawer--bottom bk-drawer--sm"`, `class="bk-drawer bk-drawer--right bk-drawer--sm"`, `Show 24 results`},
		"/components/popover/":         {`popovertarget=`, `role="dialog" aria-label="Share this page"`, `bk-popover__panel--left bk-popover__panel--center`, `bk-popover__panel--bottom bk-popover__panel--end`},
		"/components/tooltip/":         {`role="tooltip"`, `bk-tooltip--left`, `>Delete draft</span>`, `aria-label="Insert link"`},
		"/components/dropdown-menu/":   {`class="bk-dropdown__menu" role="menu" aria-label="My account"`, `<kbd class="bk-menu-item__kbd">⌘,</kbd>`, `role="menuitem" aria-disabled="true"`, `bk-menu-item--danger`, `brick:click="set_status(&#34;busy&#34;)"`, `bk-dropdown--end`},
		"/components/menu-item/":       {`<a href="/components/dropdown-menu/">`, `<kbd class="bk-menu-item__kbd">⌘S</kbd>`},
		"/components/menu-separator/":  {`role="separator"`},
		"/components/menu-label/":      {`class="bk-menu-label" role="presentation" >A MenuLabel</div>`},
		"/components/hover-card/":      {`class="bk-hovercard__trigger"><a href="/apps/contacts/"`, `Joined December 1843`, `bk-hovercard--top bk-hovercard--center`},
		"/components/command-palette/": {`class="bk-cmdk__trigger" commandfor=`, `role="combobox"`, `data-group="Commands"`, `data-value="copy-link"`, `data-keywords="crud people"`, `data-hotkey="false"`, `data-brick-type="CommandPaletteCommands"`},
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

// overlayAction runs one action of the single stateful demo on a page, as
// the runtime would, and returns the re-rendered component.
func overlayAction(t *testing.T, h http.Handler, page, action string, args ...any) string {
	t.Helper()
	_, body := get(t, h, page)
	handle := regexp.MustCompile(`data-brick-state="([^"]+)"`).FindAllStringSubmatch(body, -1)
	token := regexp.MustCompile(`data-brick-token="([^"]+)"`).FindStringSubmatch(body)
	if len(handle) != 1 || token == nil {
		t.Fatalf("%s: want one stateful demo, got %d", page, len(handle))
	}
	code, out := postAction(t, h, handle[0][1], token[1], action, args...)
	if code != http.StatusOK {
		t.Fatalf("%s %s: status %d\n%s", page, action, code, out)
	}
	return out
}

// The stateful overlay demos work on the server: an action renders a Toast
// (which the Toaster adopts in the browser), a dialog re-renders open with
// errors and closed after a valid save, a confirmed delete removes the row,
// and a menu item's action changes the row's status.
func TestOverlaysActions(t *testing.T) {
	_, h := showcase(t)

	out := overlayAction(t, h, "/components/toast/", "save")
	mustContain(t, "toast save", out, `class="bk-toast bk-toast--success" role="status" aria-atomic="true" data-duration="5000" id="saved-1">`, `Rendered by the server after save #1.`, `Saved 1 times`)

	out = overlayAction(t, h, "/components/dialog/", "edit")
	mustContain(t, "dialog edit", out, `id="demo-profile"`, `data-dismissible="false" open >`, `value="Ada Lovelace"`)
	out = overlayAction(t, h, "/components/dialog/", "save", map[string]string{"name": "", "email": "nope"})
	mustContain(t, "dialog invalid save", out, `data-dismissible="false" open >`, "Enter at least two letters.", "Enter an e-mail address like ada@example.com.", `value="nope"`, `<b>Ada Lovelace</b>`)
	out = overlayAction(t, h, "/components/dialog/", "save", map[string]string{"name": "Grace Hopper", "email": "grace@example.com"})
	mustContain(t, "dialog valid save", out, `<b>Grace Hopper</b>`, `id="profile-saved-1"`, `Grace Hopper · grace@example.com`)
	if strings.Contains(out, `data-dismissible="false" open`) {
		t.Errorf("a valid save must close the dialog:\n%s", out)
	}

	out = overlayAction(t, h, "/components/confirm-dialog/", "remove", 2)
	mustContain(t, "confirm delete", out, "Ada Lovelace", "Alan Turing", "Grace Hopper was removed.", `id="deleted-1"`)
	if strings.Contains(out, `id="demo-del-2"`) {
		t.Errorf("the deleted contact's row is still there:\n%s", out)
	}

	out = overlayAction(t, h, "/components/dropdown-menu/", "set_status", "busy")
	mustContain(t, "menu action", out, `bk-status--busy`, `>Busy</span>`)
}
