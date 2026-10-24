package brick

import (
	"regexp"
	"strings"
	"testing"
)

// nav2Render renders page over the kit.
func nav2Render(t *testing.T, page string) string {
	t.Helper()
	return render(t, newEngine(t, kitOptions(t, page)), "content/a.html", nil)
}

// nav2Has fails for each want missing from got, and each unwant present.
func nav2Has(t *testing.T, got string, wants []string, unwants ...string) {
	t.Helper()
	for _, w := range wants {
		if !strings.Contains(got, w) {
			t.Errorf("missing %q in\n%s", w, got)
		}
	}
	for _, u := range unwants {
		if strings.Contains(got, u) {
			t.Errorf("unexpected %q in\n%s", u, got)
		}
	}
}

// Toggle is a checkbox in a label: pressed checks it, an icon-only toggle
// is named by label, and the caller's attributes reach the checkbox.
func TestKitToggle(t *testing.T) {
	t.Parallel()
	got := nav2Render(t, `<Toggle icon="bold" label="Bold" name="b" pressed data-x="1" /><Toggle variant="outline" size="sm" disabled>Pin</Toggle>`)
	nav2Has(t, got, []string{
		`class="bk-toggle bk-toggle--ghost bk-toggle--icon" title="Bold"><input type="checkbox" class="bk-toggle__input" name="b" value="on" checked aria-label="Bold" data-x="1">`,
		`class="bk-toggle bk-toggle--outline bk-toggle--sm"><input type="checkbox" class="bk-toggle__input" value="on" disabled >`,
		`>Pin</span></label>`,
	})
}

// ToggleGroup: radios for single, checkboxes for multiple; value (a string
// or a list) checks the options; icon options are named by their label.
func TestKitToggleGroup(t *testing.T) {
	t.Parallel()
	got := nav2Render(t, `<ToggleGroup name="a" label="Align" value="c" :options="list (dict `+"`value` `l` `label` `Left` `icon` `align-left`) (dict `value` `c` `label` `Center` `icon` `align-center`)"+`" />`)
	nav2Has(t, got, []string{`<legend class="bk-toggle-group__legend">Align</legend>`, `type="radio" name="a" value="l" aria-label="Left">`, `type="radio" name="a" value="c" checked aria-label="Center">`})
	got = nav2Render(t, `<ToggleGroup name="f" type="multiple" :value="list `+"`b` `u`"+`" :options="list `+"`b` `i` `u` (dict `value` `s` `disabled` true)"+`" />`)
	nav2Has(t, got, []string{`type="checkbox" name="f" value="b" checked>`, `type="checkbox" name="f" value="i">`, `type="checkbox" name="f" value="u" checked>`, `value="s" disabled>`})
}

// Callout: each tone brings its icon unless told otherwise, and it is never
// a live region.
func TestKitCallout(t *testing.T) {
	t.Parallel()
	got := nav2Render(t, `<Callout tone="warning" title="Heads up">Text<brick:prop name="actions"><a href="#">Go</a></brick:prop></Callout><Callout variant="outline" icon="">Plain</Callout><Callout tone="info" icon="zap">Z</Callout>`)
	nav2Has(t, got, []string{
		`class="bk-callout bk-callout--warning bk-callout--soft"`, `<path d="m21.73 18-8-14`, `<p class="bk-callout__title">Heads up</p>`, `<div class="bk-callout__actions"><a href="#">Go</a></div>`,
		`class="bk-callout bk-callout--neutral bk-callout--outline" ><div class="bk-callout__body">`,
	}, `role="alert"`, `role="status"`, `aria-live`)
	if strings.Count(got, `class="bk-icon bk-callout__icon"`) != 2 {
		t.Errorf("two callouts have icons:\n%s", got)
	}
}

// FilterChips: a reset radio with an empty value, checked when nothing is;
// collapse turns it into an ×; multiple gets checkboxes and a reset input.
func TestKitFilterChips(t *testing.T) {
	t.Parallel()
	got := nav2Render(t, `<FilterChips name="s" :options="list `+"`Open` `Closed`"+`" value="Open" /><FilterChips name="z" collapse :options="list `+"`S` `M`"+`" />`)
	nav2Has(t, got, []string{
		`type="radio" name="s" value=""><span class="bk-filter__face">All</span>`, `type="radio" name="s" value="Open" checked>`, `type="radio" name="s" value="Closed">`,
		`class="bk-filter bk-filter--collapse"`, `type="radio" name="z" value="" checked aria-label="All">`,
	})
	got = nav2Render(t, `<FilterChips name="t" type="multiple" reset="Clear" :value="list `+"`go`"+`" :options="list `+"`go` `css`"+`" />`)
	nav2Has(t, got, []string{`type="checkbox" name="t" value="go" checked>`, `type="checkbox" name="t" value="css">`, `<input type="reset" class="bk-filter__clear" value="Clear">`}, `value=""`)
}

// Fab: a link, a button, or a <details> speed dial of labelled actions.
func TestKitFab(t *testing.T) {
	t.Parallel()
	got := nav2Render(t, `<Fab label="New" href="/new/" /><Fab label="Go" position="top-start" :fixed="false" tone="neutral" /><Fab label="Create" :actions="list (dict `+"`label` `Note` `icon` `file-text` `href` `/n/`) (dict `label` `Ping`)"+`" />`)
	nav2Has(t, got, []string{
		`<a class="bk-fab__main" href="/new/" aria-label="New" title="New">`,
		`class="bk-fab bk-fab--top-start bk-fab--inline bk-fab--neutral"`, `<button type="button" class="bk-fab__main" aria-label="Go"`,
		`<details class="bk-fab__dial"><summary class="bk-fab__main" aria-label="Create">`,
		`<a class="bk-fab__action" href="/n/"><span class="bk-fab__action-label">Note</span>`,
		`<button type="button" class="bk-fab__action"><span class="bk-fab__action-label">Ping</span>`,
	})
}

// Brand and Profile: a link by default; a mark-only brand and a compact
// profile keep the name as the accessible name.
func TestKitBrandProfile(t *testing.T) {
	t.Parallel()
	got := nav2Render(t, `<Brand name="acme" /><Brand name="Acme" icon="zap" hideName size="lg" /><Brand name="Acme" href="" logo="/l.svg" subtitle="Team" />`)
	nav2Has(t, got, []string{
		`<a class="bk-brand" href="/" ><span class="bk-brand__mark" aria-hidden="true">A</span><span class="bk-brand__text"><span>acme</span></span></a>`,
		`<a class="bk-brand bk-brand--lg" href="/" aria-label="Acme" >`,
		`<span class="bk-brand" ><img class="bk-brand__mark" src="/l.svg" alt=""><span class="bk-brand__text"><span>Acme</span><span class="bk-brand__sub">Team</span>`,
	})
	got = nav2Render(t, `<Profile name="Ada Lovelace" email="ada@x.io" /><Profile name="Ada" href="/me/" chevron="none" /><Profile name="Ada" compact />`)
	nav2Has(t, got, []string{
		`<button type="button" class="bk-profile" >`, `<span class="bk-profile__name">Ada Lovelace</span><span class="bk-profile__email">ada@x.io</span>`, `bk-profile__chev`,
		`<a class="bk-profile" href="/me/" >`, `<button type="button" class="bk-profile bk-profile--compact" aria-label="Ada" >`,
	})
	if strings.Count(got, "bk-profile__chev") != 1 {
		t.Error("only the first profile has a chevron")
	}
}

var nav2OptRe = regexp.MustCompile(`data-value="([0-9:]+)"[^>]*>([^<]+)</li>`)

// TimePicker: the list holds every step minutes from min to max, labelled
// 12- or 24-hour, the value selected; the input keeps min, max and a step
// in seconds, and the list button waits for the script.
func TestKitTimePicker(t *testing.T) {
	t.Parallel()
	got := nav2Render(t, `<TimePicker name="t" min="09:00" max="11:00" :step="45" hour12 value="09:45" />`)
	var opts []string
	for _, m := range nav2OptRe.FindAllStringSubmatch(got, -1) {
		opts = append(opts, m[1]+"="+m[2])
	}
	if strings.Join(opts, ",") != "09:00=9:00 AM,09:45=9:45 AM,10:30=10:30 AM" {
		t.Errorf("options: %v", opts)
	}
	nav2Has(t, got, []string{`type="time" class="bk-timepicker__input" value="09:45" min="09:00" max="11:00" step="2700"`, `name="t"`, `aria-selected="true">9:45 AM`, `aria-haspopup="listbox" aria-expanded="false"`, `hidden><svg`})
	got = nav2Render(t, `<TimePicker name="u" min="11:30" max="13:00" :step="30" />`)
	if n := strings.Count(got, `role="option"`); n != 4 || !strings.Contains(got, `>12:30</li>`) {
		t.Errorf("24-hour 11:30…13:00 by 30: %d options\n%s", n, got)
	}
}

// MultiSelect: without JavaScript a native select multiple with the values
// selected; the enhanced parts are hidden and the chip is a template.
func TestKitMultiSelect(t *testing.T) {
	t.Parallel()
	got := nav2Render(t, `<MultiSelect name="s" :value="list `+"`b`"+`" :options="list `+"`a` `b` (dict `value` `c` `label` `Cee`)"+`" />`)
	nav2Has(t, got, []string{
		`<select multiple class="bk-multiselect__native" data-placeholder="Select…" size="3" name="s">`,
		`<option value="a">a</option><option value="b" selected>b</option><option value="c">Cee</option>`,
		`<div class="bk-multiselect__ui" hidden>`, `role="listbox" aria-multiselectable="true"`, `data-value="c" data-label="Cee"`,
		`<template class="bk-multiselect__tpl">`,
	})
}

// Editor: the value is the textarea's text (escaped: it is data), the
// toolbar has the tools asked for, and only toggling tools are pressable.
func TestKitEditor(t *testing.T) {
	t.Parallel()
	got := nav2Render(t, `<Editor name="body" :tools="list `+"`bold` `link` `quote`"+`"><brick:prop name="value" raw><p>Hi <b>there</b></p><script>x()</script></brick:prop></Editor>`)
	nav2Has(t, got, []string{
		`<textarea class="bk-editor__source" rows="6" placeholder="Write something…" name="body">&lt;p&gt;Hi &lt;b&gt;there&lt;/b&gt;&lt;/p&gt;&lt;script&gt;x()&lt;/script&gt;</textarea>`,
		`role="toolbar" aria-label="Formatting"`, `data-tool="bold" aria-label="Bold" title="Bold" aria-pressed="false"`,
		`data-tool="link" aria-label="Link" title="Link">`, `data-tool="quote"`, `class="bk-editor__content" role="textbox" aria-multiline="true"`,
	}, `data-tool="italic"`, `<script>x()`)
}

// With JavaScript: TimePicker, MultiSelect, Editor and Fab
// (runtime/test/kit_nav2.mjs).
func TestKitNav2InBrowser(t *testing.T) {
	runKitJS(t, `<TimePicker name="t" min="09:00" max="10:00" :step="30" value="09:30" />`+
		`<MultiSelect name="m" :value="list `+"`b`"+`" :options="list `+"`apple` `b` `cherry`"+`" />`+
		`<Editor name="e"><brick:prop name="value" raw><p onclick="x()">Hi <b>there</b><script>bad()</script> <a href="javascript:alert(1)">x</a> <a href="https://ok.test/">ok</a></p></brick:prop></Editor><Field label="Notes"><Editor name="n" /></Field>`+
		`<Fab label="Create" :actions="list (dict `+"`label` `Note` `href` `#n`"+`)" />{{ assets }}`, "kit_nav2.mjs")
}

// Card's title is a heading at level (h3 by default), so a page can keep
// its outline without skipped levels: a card directly under the h1 is h2.
func TestKitCardTitleLevel(t *testing.T) {
	t.Parallel()
	got := nav2Render(t, `<Card title="A" /><Card title="B" :level="2" /><Card title="C" :level="6" />`)
	nav2Has(t, got, []string{`<h3 class="bk-card__title">A</h3>`, `<h2 class="bk-card__title">B</h2>`, `<h6 class="bk-card__title">C</h6>`})
	if err := renderErr(t, newEngine(t, kitOptions(t, `<Card title="D" :level="1" />`)), "content/a.html"); !strings.Contains(err.Error(), "level") {
		t.Errorf("level 1 (the page's own h1): %v", err)
	}
}
