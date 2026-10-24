package brick

import (
	"regexp"
	"strings"
	"testing"
	"testing/fstest"
)

// formsRender renders page with the kit and fails on an error.
func formsRender(t *testing.T, page string) string {
	t.Helper()
	return render(t, newEngine(t, kitOptions(t, page)), "content/a.html", nil)
}

// formsTagOf returns the first start tag in html that contains marker, so an
// assertion names the element an attribute must land on, not just the page.
func formsTagOf(t *testing.T, html, marker string) string {
	t.Helper()
	for _, tag := range regexp.MustCompile(`<[a-zA-Z][^<>]*>`).FindAllString(html, -1) {
		if strings.Contains(tag, marker) {
			return tag
		}
	}
	t.Fatalf("no tag containing %q in\n%s", marker, html)
	return ""
}

// formsHasAll fails for each want missing from s.
func formsHasAll(t *testing.T, what, s string, wants ...string) {
	t.Helper()
	for _, w := range wants {
		if !strings.Contains(s, w) {
			t.Errorf("%s: missing %q in\n%s", what, w, s)
		}
	}
}

// A control's attributes reach the real <input>, <select> or <textarea>;
// the caller's class goes on the wrapper box. A brick:model can also arrive
// through brick:spread, as a wrapper forwarding .Attrs passes it on.
func TestKitFormsAttrsReachTheControl(t *testing.T) {
	t.Parallel()
	got := formsRender(t, "<Input name=\"email\" type=\"email\" value=\"a@b.c\" placeholder=\"Mail\" hx-get=\"/check\" data-x=\"1\" class=\"w-64\" brick:spread=\"dict `brick:model` `email`\" />"+
		`<Textarea name="bio" value="fish & chips" class="t" autocomplete="off" />`+
		`<Select name="role" class="s" hx-post="/r" :options="list `+"`a`"+`" />`+
		`<Checkbox name="agree" value="yes" class="c" />`+
		`<PasswordInput name="pw" autocomplete="new-password" class="p" />`+
		`<NumberInput name="qty" value="2" min="1" max="5" class="n" />`+
		`<SearchInput name="q" hx-get="/s" class="q" />`+
		`<DatePicker name="due" value="2026-10-01" class="d" />`+
		`<TagInput name="tags" value="a,b" class="g" />`+
		`<OTPInput name="code" class="o" />`+
		`<Combobox name="c" :options="list `+"`x`"+`" class="cb" />`+
		`<FileInput name="f" accept="image/*" class="fi" />`+
		`<Dropzone name="dz" multiple class="z" />`)
	formsHasAll(t, "Input", formsTagOf(t, got, `name="email"`), `class="bk-input__control"`, `type="email"`, `value="a@b.c"`, `placeholder="Mail"`, `hx-get="/check"`, `data-x="1"`, `brick:model="email"`)
	formsHasAll(t, "Input box", got, `<div class="bk-input w-64">`)
	formsHasAll(t, "Textarea", got, `name="bio" autocomplete="off">fish &amp; chips</textarea>`, `class="bk-textarea t"`)
	formsHasAll(t, "Select", formsTagOf(t, got, `name="role"`), `<select class="bk-select__control"`, `hx-post="/r"`)
	formsHasAll(t, "Checkbox", formsTagOf(t, got, `name="agree"`), `type="checkbox"`, `value="yes"`)
	for _, c := range []struct{ name, want, box string }{
		{`name="pw"`, `autocomplete="new-password"`, `bk-password p`},
		{`name="qty"`, `min="1"`, `bk-number n`},
		{`name="q"`, `hx-get="/s"`, `bk-search q`},
		{`name="due"`, `value="2026-10-01"`, `bk-datepicker d`},
		{`name="tags"`, `value="a,b"`, `bk-tags g`},
		{`name="code"`, `autocomplete="one-time-code"`, `bk-otp o`},
		{`name="c"`, `<select class="bk-combobox__native"`, `bk-combobox cb`},
		{`name="dz"`, `multiple`, `bk-dropzone z`},
	} {
		tag := formsTagOf(t, got, c.name)
		formsHasAll(t, c.name, tag, c.want)
		if strings.Contains(tag, `class="`+c.box) {
			t.Errorf("%s: the caller's class landed on the control, not the wrapper: %s", c.name, tag)
		}
		formsHasAll(t, c.name, got, `class="`+c.box+`"`)
	}
	// FileInput has no wrapper: the <input type="file"> is the root.
	formsHasAll(t, "FileInput", formsTagOf(t, got, `name="f"`), `class="bk-file fi"`, `accept="image/*"`)
	if n := strings.Count(formsTagOf(t, got, `name="pw"`), "class="); n != 1 {
		t.Errorf("PasswordInput control has %d class attributes", n)
	}
}

// Inside a stateful component, brick:model written on a kit control lands
// on the real control, where the runtime binds it.
func TestKitFormsModelOnControls(t *testing.T) {
	t.Parallel()
	opts := kitOptions(t, `<Signup />`)
	opts.States = NewMemoryStates(0, 0)
	opts.FS.(fstest.MapFS)["kit/components/Signup.html"] = &fstest.MapFile{Data: []byte("---\nstate:\n  email: \"\"\n  bio: \"\"\n  plan: \"\"\n---\n<form><Input name=\"email\" brick:model=\"email\" /><Textarea name=\"bio\" brick:model.lazy=\"bio\" /><Select name=\"plan\" brick:model=\"plan\" :options=\"list `a`\" /><PasswordInput name=\"pw\" brick:model=\"email\" /></form>")}
	got := render(t, newEngine(t, opts), "content/a.html", nil)
	formsHasAll(t, "Input", formsTagOf(t, got, `name="email"`), `class="bk-input__control"`, `brick:model="email"`)
	formsHasAll(t, "Textarea", formsTagOf(t, got, `name="bio"`), `class="bk-textarea__control"`, `brick:model.lazy="bio"`)
	formsHasAll(t, "Select", formsTagOf(t, got, `name="plan"`), `class="bk-select__control"`, `brick:model="plan"`)
	formsHasAll(t, "PasswordInput forwards it", formsTagOf(t, got, `name="pw"`), `class="bk-input__control"`, `brick:model="email"`)
}

// A Field wires the label, hint and error to its control without the page
// writing an id: for= matches id=, aria-describedby names both messages,
// and an error makes the control aria-invalid.
func TestKitFieldWiring(t *testing.T) {
	t.Parallel()
	got := formsRender(t, `<Field label="Email" hint="Never shared." error="Bad address" required><Input name="email" /></Field>`+
		`<Field label="Bio" for="bio"><Textarea name="bio" /></Field>`+
		`<Field label="Plan" hint="Pick one."><RadioGroup name="plan" :options="list `+"`a` `b`"+`" value="b" /></Field>`+
		`<Field label="Nick"><Input name="nick" /></Field>`)
	id := regexp.MustCompile(`<label class="bk-label" for="([^"]+)"`).FindStringSubmatch(got)
	if id == nil {
		t.Fatalf("no label for= in\n%s", got)
	}
	f := id[1]
	formsHasAll(t, "Field input", formsTagOf(t, got, `name="email"`), `id="`+f+`"`, `aria-describedby="`+f+`-hint `+f+`-error"`, `aria-invalid="true"`, ` required`)
	formsHasAll(t, "Field messages", got, `<p class="bk-field__hint" id="`+f+`-hint">Never shared.</p>`, `id="`+f+`-error" role="alert"`, `Bad address</p>`, `bk-label__req`)
	formsHasAll(t, "for=", got, `for="bio"`)
	formsHasAll(t, "for= reaches the control", formsTagOf(t, got, `name="bio"`), `id="bio"`)
	group := formsTagOf(t, got, `role="radiogroup"`)
	formsHasAll(t, "RadioGroup in a Field", group, `aria-labelledby="`, `-label"`, `aria-describedby="`)
	for _, radio := range regexp.MustCompile(`<input type="radio"[^>]*>`).FindAllString(got, -1) {
		if strings.Contains(radio, " id=") || strings.Contains(radio, "aria-describedby") {
			t.Errorf("a radio took the Field's id or description: %s", radio)
		}
	}
	nick := formsTagOf(t, got, `name="nick"`)
	if strings.Contains(nick, "aria-invalid") || strings.Contains(nick, "aria-describedby") || strings.Contains(nick, "required") {
		t.Errorf("a Field without hint or error added them: %s", nick)
	}
	// Two Fields get two different ids.
	if ids := regexp.MustCompile(`<label class="bk-label" for="([^"]+)"`).FindAllStringSubmatch(got, -1); len(ids) != 4 || ids[0][1] == ids[3][1] {
		t.Errorf("Field ids not distinct: %v", ids)
	}
	// A nil error (an .Errors key that is not set) shows nothing.
	got = formsRender(t, `<Field label="X" :error=".Params.nope"><Input name="x" /></Field>`)
	if strings.Contains(got, "bk-field__error") || strings.Contains(got, "aria-invalid") {
		t.Errorf("nil error rendered as an error:\n%s", got)
	}
}

// Options render from strings, maps and children, with the selected or
// checked state from value=.
func TestKitFormsOptionsAndState(t *testing.T) {
	t.Parallel()
	got := formsRender(t, `<Select name="s" placeholder="Pick" value="b" :options="list `+"`a` (dict `value` `b` `label` `Bee`) (dict `value` `c` `label` `Sea` `disabled` true)"+`"><option value="z">Zed</option></Select>`+
		`<Select name="r" placeholder="Pick" required :options="list `+"`a`"+`" />`+
		`<RadioGroup name="p" value="pro" variant="card" :columns="3" :options="list `+"(dict `value` `free` `label` `Free`) (dict `value` `pro` `label` `Pro` `description` `$12`)"+`" />`+
		`<Checkbox name="k" checked /><Checkbox name="m" indeterminate /><Switch name="w" checked />`+
		`<Combobox name="cb" value="u2" :options="list `+"(dict `value` `u1` `label` `Ava`) (dict `value` `u2` `label` `Maya`)"+`" />`+
		`<Rating name="stars" :value="3" /><Rating :value="2" :max="4" readonly />`+
		`<ColorPicker name="col" value="#059669" /><ColorPicker name="cus" :colors="list `+"`#111111`"+`" custom value="#abcdef" />`+
		`<Slider name="v" :min="0" :max="200" :value="50" unit="%" :marks="list 0 100 200" />`+
		`<Slider name="price" :min="5" :max="80" :value="79" prefix="$" :marks="list 5 80" />`)
	formsHasAll(t, "Select", got, `<option value="">Pick</option>`, `<option value="a">a</option>`, `<option value="b" selected>Bee</option>`, `<option value="c" disabled>Sea</option>`, `<option value="z">Zed</option>`)
	formsHasAll(t, "Select placeholder selected when empty", got, `<select class="bk-select__control" required name="r"><option value="" selected disabled>Pick</option>`)
	formsHasAll(t, "RadioGroup", got, `value="pro"`, `bk-radio--card`, `style="--bk-cols: 3"`, `<span class="bk-radio__desc">$12</span>`)
	formsHasAll(t, "RadioGroup checked", formsTagOf(t, got, `value="pro"`), `checked`)
	if strings.Contains(formsTagOf(t, got, `value="free"`), "checked") {
		t.Error("RadioGroup checked the wrong radio")
	}
	formsHasAll(t, "Checkbox", formsTagOf(t, got, `name="k"`), ` checked`)
	formsHasAll(t, "indeterminate", formsTagOf(t, got, `name="m"`), `data-indeterminate`)
	formsHasAll(t, "Switch", formsTagOf(t, got, `name="w"`), `role="switch"`, ` checked`)
	formsHasAll(t, "Combobox", got, `<option value="u2" selected>Maya</option>`, `role="combobox"`, `aria-expanded="false"`, `role="listbox"`, `data-value="u2" data-label="Maya" aria-selected="true"`, `data-value="u1" data-label="Ava" aria-selected="false"`)
	formsHasAll(t, "Rating", formsTagOf(t, got, `value="3"`), `checked`, `name="stars"`)
	formsHasAll(t, "Rating readonly", got, `role="img" aria-label="2 out of 4"`)
	if n := strings.Count(got, "bk-rating__star--on"); n != 2 {
		t.Errorf("readonly rating lit %d stars, want 2", n)
	}
	formsHasAll(t, "ColorPicker", formsTagOf(t, got, `value="#059669"`), `checked`, `name="col"`)
	formsHasAll(t, "ColorPicker custom", formsTagOf(t, got, `aria-label="Custom colour"`), `value="#abcdef"`, `checked`)
	formsHasAll(t, "Slider", got, `style="--bk-slider-p: 25%"`, `min="0" max="200" step="1" value="50"`, `>50%</output>`, `--bk-mark-p: 0.5">100%</li>`)
	formsHasAll(t, "Slider prefix", got, `data-prefix="$"`, `>$79</output>`, `">$80</li>`)
}

// Without JavaScript every enhanced control is complete: the value
// carriers are visible and the enhancements are hidden.
func TestKitFormsWithoutJS(t *testing.T) {
	t.Parallel()
	got := formsRender(t, `<OTPInput name="code" :length="4" :group="2" /><TagInput name="t" value="a,b" /><Combobox name="c" :options="list `+"`x`"+`" /><PasswordInput name="p" /><NumberInput name="n" /><SearchInput name="s" shortcut="⌘K" /><DatePicker name="d" />`)
	otp := formsTagOf(t, got, `name="code"`)
	if strings.Contains(otp, "hidden") {
		t.Errorf("the OTP value input is hidden without JS: %s", otp)
	}
	formsHasAll(t, "OTP", otp, `maxlength="4"`, `pattern="[0-9]{4}"`, `inputmode="numeric"`)
	formsHasAll(t, "OTP boxes", got, `class="bk-otp__boxes" role="group" aria-label="Verification code" hidden`)
	if n := strings.Count(got, `class="bk-otp__box"`); n != 4 {
		t.Errorf("OTP has %d boxes, want 4", n)
	}
	if n := strings.Count(got, `bk-otp__sep`); n != 1 {
		t.Errorf("OTP group=2 of 4 has %d separators, want 1", n)
	}
	if strings.Contains(formsTagOf(t, got, `name="t"`), "hidden") {
		t.Error("TagInput value input is hidden without JS")
	}
	if strings.Contains(formsTagOf(t, got, `name="c"`), "hidden") {
		t.Error("Combobox select is hidden without JS")
	}
	formsHasAll(t, "hidden enhancements", got, `class="bk-combobox__ui" hidden`, `class="bk-password__toggle" aria-label="Show password" aria-pressed="false" hidden`, `data-step="-1" aria-label="Decrease" tabindex="-1" hidden`, `class="bk-search__clear" aria-label="Clear search" hidden`, `class="bk-datepicker__toggle"`, `data-shortcut="⌘K"`)
	if n := strings.Count(got, `class="bk-datepicker__day"`); n != 42 {
		t.Errorf("DatePicker grid has %d cells, want 42", n)
	}
}

// With JavaScript each enhanced control does its job: the real rendered
// page and the real modules in jsdom.
func TestKitFormsInBrowser(t *testing.T) {
	t.Parallel()
	cases := []struct{ page, script string }{
		{`<OTPInput name="code" /><OTPInput name="k" :length="4" mode="alphanumeric" value="ab" />`, "kit_otp_input.mjs"},
		{`<TagInput name="tags" value="go, design" :max="4" />`, "kit_tag_input.mjs"},
		{`<Field label="Owner"><Combobox name="owner" :options="list ` + "(dict `value` `u1` `label` `Ava Patel`) (dict `value` `u2` `label` `Maya Chen`) (dict `value` `u3` `label` `Noah Kim`) `Zed`" + `" /></Field>`, "kit_combobox.mjs"},
		{`<NumberInput name="qty" value="9" min="1" max="10" /><NumberInput name="w" value="1.5" step="0.5" min="0" />`, "kit_number_input.mjs"},
		{`<PasswordInput name="pw" value="secret" />`, "kit_password_input.mjs"},
		{`<Slider name="v" :min="0" :max="200" :value="50" unit="%" /><Slider name="p" :min="5" :max="80" :value="79" prefix="$" />`, "kit_slider.mjs"},
		{`<Textarea name="t" :maxlength="10" counter autosize value="abc" />`, "kit_textarea.mjs"},
		{`<SearchInput name="q" value="maya" shortcut="⌘K" />`, "kit_search_input.mjs"},
		{`<Checkbox name="all" indeterminate />`, "kit_checkbox.mjs"},
		{`<Dropzone name="files" multiple />`, "kit_dropzone.mjs"},
		{`<DatePicker name="due" value="2026-02-14" min="2026-02-03" max="2026-03-20" />`, "kit_date_picker.mjs"},
		{`<ColorPicker name="c" :colors="list ` + "`#111111` `#222222`" + `" custom />`, "kit_color_picker.mjs"},
	}
	for _, c := range cases {
		t.Run(c.script, func(t *testing.T) {
			t.Parallel()
			runKitJS(t, c.page+"{{ assets }}", c.script)
		})
	}
}
