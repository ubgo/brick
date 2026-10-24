package main

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

// formsPages are the Forms pages and a fragment each must render, so a
// page that silently drops its demo fails here.
var formsPages = map[string][]string{
	"label":           {`class="bk-label" for="lb-name"`, `bk-label__req`, `bk-label__opt`},
	"field":           {`class="bk-field__hint"`, `That username is taken.`, `bk-field--horizontal`, `Forgot password?`},
	"input":           {`class="bk-input__addon">https://</span>`, `bk-input--invalid`, `aria-invalid="true"`},
	"textarea":        {`class="bk-textarea__count" aria-hidden="true">0 / 280</span>`, `bk-textarea--autosize`},
	"select":          {`<option value="Editor" selected>Editor</option>`, `<option value="" selected disabled>Choose a country</option>`},
	"checkbox":        {`data-indeterminate`, `bk-check__desc`},
	"radio":           {`class="bk-radio__dot" checked name="plan" value="free"`},
	"radio-group":     {`role="radiogroup"`, `bk-radio--card`, `aria-labelledby=`},
	"switch":          {`role="switch"`, `bk-switch--sm`},
	"slider":          {`type="range"`, `<output class="bk-slider__value"`, `bk-slider__mark`},
	"number-input":    {`type="number"`, `data-step="1"`},
	"password-input":  {`type="password"`, `bk-password__toggle`},
	"search-input":    {`type="search"`, `<kbd class="bk-search__kbd" aria-hidden="true">⌘K</kbd>`},
	"input-group":     {`role="group" class="bk-input-group"`, `bk-input-group__text`},
	"file-input":      {`type="file" class="bk-file"`},
	"dropzone":        {`class="bk-dropzone__input"`, `<template class="bk-dropzone__tpl">`},
	"rating":          {`role="img" aria-label="4 out of 5"`, `value="4" checked`},
	"otpinput":        {`autocomplete="one-time-code"`, `class="bk-otp__sep"`},
	"tag-input":       {`value="design,go,html"`, `<template class="bk-tags__tpl">`},
	"combobox":        {`role="combobox"`, `role="listbox"`, `<option value="u2" selected>Maya Chen</option>`},
	"date-picker":     {`type="date"`, `value="2026-10-01"`, `bk-datepicker__pop`},
	"calendar":        {`<h3 class="bk-calendar__title">February 2026</h3>`, `href="?date=2026-09-18"`, `href="?month=2026-10"`, `aria-current="date"`},
	"color-picker":    {`--bk-swatch: #059669`, `bk-color__custom--set`},
	"fieldset":        {`<legend class="bk-fieldset__legend">Profile</legend>`, `--bk-cols: 2`},
	"form-actions":    {`bk-form-actions--between`, `bk-form-actions--divider`},
	"form-validation": {`brick:submit="save"`, `data-brick-state=`, `novalidate`},
}

// Every Forms page renders its demos.
func TestFormsPages(t *testing.T) {
	_, h := showcase(t)
	for slug, wants := range formsPages {
		url := "/components/" + slug + "/"
		code, body := get(t, h, url)
		if code != http.StatusOK {
			t.Errorf("%s: status %d", url, code)
			continue
		}
		mustContain(t, url, body, wants...)
	}
}

// The Calendar computes its month grid in Starlark: February 2026 starts on
// a Sunday and fills four exact weeks; February 2024 has 29 days (a leap
// year); a Monday start moves the first column.
func TestFormsCalendarGrid(t *testing.T) {
	s, _ := showcase(t)
	cal := func(props map[string]any) string {
		t.Helper()
		b, err := s.Engine().RenderComponent(context.Background(), "Calendar", props, "")
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	days := regexp.MustCompile(`class="bk-calendar__day[^"]*"[^>]*>(\d+)<`)
	cells := func(html string) []string {
		var out []string
		for _, m := range days.FindAllStringSubmatch(html, -1) {
			out = append(out, m[1])
		}
		return out
	}
	feb26 := cal(map[string]any{"month": "2026-02", "selected": "2026-02-14", "today": "2026-02-10"})
	got := cells(feb26)
	if len(got) != 28 || got[0] != "1" || got[27] != "28" {
		t.Errorf("2026-02: want 28 cells 1…28 starting on Sunday, got %v", got)
	}
	mustContain(t, "2026-02", feb26, `<th scope="col" abbr="Sunday">Su</th>`, `bk-calendar__day--selected">14<`, `bk-calendar__day--today" aria-current="date">10<`)
	feb24 := cal(map[string]any{"month": "2024-02"})
	got = cells(feb24)
	// 2024-02-01 is a Thursday: four January days lead, then 1…29, then two
	// March days fill the last week.
	if len(got) != 35 || strings.Join(got[:5], ",") != "28,29,30,31,1" || got[32] != "29" || got[34] != "2" {
		t.Errorf("2024-02: got %v", got)
	}
	if n := strings.Count(feb24, "bk-calendar__day--out"); n != 6 {
		t.Errorf("2024-02: %d muted days, want 6", n)
	}
	mon := cal(map[string]any{"month": "2026-03", "weekStart": "monday"})
	if got := cells(mon); got[0] != "23" || got[6] != "1" {
		t.Errorf("2026-03 from Monday: want 23 Feb … 1 Mar in the first week, got %v", got[:7])
	}
	mustContain(t, "monday", mon, `<th scope="col" abbr="Monday">Mo</th>`)
	if _, err := s.Engine().RenderComponent(context.Background(), "Calendar", map[string]any{"month": "2026-13"}, ""); err == nil || !strings.Contains(err.Error(), "out of range") {
		t.Errorf("month 13: %v", err)
	}
}

// The validation demo is a real stateful form: an empty submit returns the
// errors inline, and a valid one the success state.
func TestFormsValidationAction(t *testing.T) {
	_, h := showcase(t)
	_, page := get(t, h, "/components/form-validation/")
	handle := regexp.MustCompile(`data-brick-state="([^"]+)"`).FindStringSubmatch(page)
	token := regexp.MustCompile(`data-brick-token="([^"]+)"`).FindStringSubmatch(page)
	if handle == nil || token == nil {
		t.Fatalf("no state handle or token in the page")
	}
	act := func(fields map[string]string) (int, string) {
		t.Helper()
		return postAction(t, h, handle[1], token[1], "save", fields)
	}
	code, out := act(map[string]string{"name": "A", "email": "nope", "plan": "pro", "seats": "99"})
	if code != http.StatusOK {
		t.Fatalf("invalid submit: status %d\n%s", code, out)
	}
	mustContain(t, "invalid submit", out,
		"Enter at least two letters.", "Enter an e-mail address like ada@example.com.", "Seats must be a whole number from 1 to 50.", "Please accept the terms to continue.",
		`aria-invalid="true"`, `class="bk-field__error"`, `value="nope"`, `value="99"`)
	if strings.Contains(out, "Choose a plan.") || strings.Contains(out, "Account created") {
		t.Errorf("invalid submit: a valid plan was flagged, or it succeeded:\n%s", out)
	}
	code, out = act(map[string]string{"name": "Ada Lovelace", "email": "ada@example.com", "plan": "team", "seats": "5", "agree": "on"})
	if code != http.StatusOK {
		t.Fatalf("valid submit: status %d\n%s", code, out)
	}
	mustContain(t, "valid submit", out, "Account created", "Welcome, Ada Lovelace.", "5 seats on the team plan", `brick:click="restart"`)
	if strings.Contains(out, "bk-field__error") {
		t.Errorf("valid submit still shows errors:\n%s", out)
	}
}

// Every doc page's api: rows are {name, type, default, desc} maps. A name
// with a comma written unquoted in a YAML flow map ({name: a, b, type: …})
// silently splits into extra keys and drops the text after the comma; this
// catches that on every page.
func TestFormsAPIRowsParse(t *testing.T) {
	files, err := filepath.Glob("content/components/*.html")
	if err != nil {
		t.Fatal(err)
	}
	allowed := map[string]bool{"name": true, "type": true, "default": true, "desc": true}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		s := string(b)
		end := strings.Index(s[4:], "\n---")
		if !strings.HasPrefix(s, "---\n") || end < 0 {
			continue
		}
		var fm struct {
			API []map[string]any `yaml:"api"`
		}
		if err := yaml.Unmarshal([]byte(s[4:4+end]), &fm); err != nil {
			t.Errorf("%s: frontmatter: %v", f, err)
			continue
		}
		for i, row := range fm.API {
			for k, v := range row {
				if !allowed[k] {
					t.Errorf("%s: api row %d has key %q (value %v): quote a name or desc that contains a comma", f, i, k, v)
				}
			}
			if row["name"] == nil || row["name"] == "" {
				t.Errorf("%s: api row %d has no name", f, i)
			}
		}
	}
}
