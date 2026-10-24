package brick

import (
	"strings"
	"testing"
)

// fndRender renders page with the kit and returns the HTML.
func fndRender(t *testing.T, page string) string {
	t.Helper()
	return render(t, newEngine(t, kitOptions(t, page)), "content/a.html", nil)
}

// fndHas fails for each want missing from got.
func fndHas(t *testing.T, got string, wants ...string) {
	t.Helper()
	for _, w := range wants {
		if !strings.Contains(got, w) {
			t.Errorf("missing %q in\n%s", w, got)
		}
	}
}

// Foundations: the level picks the element and the size picks the look;
// text, links, keys and code merge the caller's class and pass attributes.
func TestKitFoundations(t *testing.T) {
	t.Parallel()
	got := fndRender(t, `<Heading :level="1">A</Heading>|<Heading :level="3" size="sm" id="h" class="x">B</Heading>|<Heading>C</Heading>|`+
		`<Text tone="muted" size="sm" as="span" truncate>T</Text>|<Text>P</Text>|`+
		`<Link href="/x" data-k="1">in</Link>|<Link href="https://go.dev" external tone="muted">ext</Link>|`+
		`<Kbd keys="⌘ K" />|<Kbd size="sm">Esc</Kbd>|<Code tone="primary">go</Code>|`+
		`<Prose size="sm" id="p"><h2>P</h2></Prose>|<Separator />|<Separator label="or" align="start" />|<Separator orientation="vertical" />`)
	fndHas(t, got,
		`<h1 class="bk-heading bk-heading--2xl" >A</h1>`,
		`<h3 class="bk-heading bk-heading--sm x" id="h">B</h3>`,
		`<h2 class="bk-heading bk-heading--xl" >C</h2>`,
		`<span class="bk-text bk-text--sm bk-text--muted bk-text--truncate" >T</span>`,
		`<p class="bk-text" >P</p>`,
		`<a href="/x" class="bk-link" data-k="1">in</a>`,
		`class="bk-link bk-link--muted" target="_blank" rel="noopener noreferrer"`, `(opens in a new tab)`,
		`<kbd class="bk-kbd" ><kbd class="bk-kbd__key">⌘</kbd><kbd class="bk-kbd__key">K</kbd></kbd>`,
		`<kbd class="bk-kbd bk-kbd--single bk-kbd--sm" >Esc</kbd>`,
		`<code class="bk-code bk-code--primary" >go</code>`,
		`<div class="bk-prose bk-prose--sm" id="p"><h2>P</h2></div>`,
		`<hr class="bk-separator" >`,
		`<div role="separator" class="bk-separator bk-separator--label bk-separator--start" >or</div>`,
		`role="separator" aria-orientation="vertical" class="bk-separator bk-separator--vertical"`,
	)
	if err := renderErr(t, newEngine(t, kitOptions(t, `<Heading :level="7">x</Heading>`)), "content/a.html"); err == nil {
		t.Error("level 7 must be an error")
	}
}

// Inline components render no whitespace around themselves, so they sit
// inside running text without adding spaces.
func TestKitInlineNoWhitespace(t *testing.T) {
	t.Parallel()
	got := fndRender(t, `[<Code>x</Code>][<Kbd keys="A B" />][<Link href="/">l</Link>][<Text as="span">t</Text>]`)
	want := `[<code class="bk-code" >x</code>][<kbd class="bk-kbd" ><kbd class="bk-kbd__key">A</kbd><kbd class="bk-kbd__key">B</kbd></kbd>][<a href="/" class="bk-link" >l</a>][<span class="bk-text" >t</span>]`
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

// Actions: client-enhanced controls are hidden until mounted; Swap works
// with a checkbox; ButtonGroup is a labelled group.
func TestKitActions(t *testing.T) {
	t.Parallel()
	got := fndRender(t, `<AccentPicker size="sm" :accents="list `+"`indigo` `rose`"+`" id="ap" />|`+
		`<CopyButton text="hi" label="Copy key" />|<CopyButton target="#t" showLabel variant="outline" />|`+
		`<Swap label="Menu" iconOff="menu" iconOn="x" name="m" checked />|`+
		`<Swap label="F"><brick:prop name="off">Follow</brick:prop><brick:prop name="on">Following</brick:prop></Swap>|`+
		`<ButtonGroup label="Style" orientation="vertical"><Button>a</Button></ButtonGroup>|<Button href="/x" disabled>L</Button>`)
	fndHas(t, got,
		`data-brick-type="AccentPicker"`, `class="bk-accent-picker bk-accent-picker--sm" role="radiogroup" aria-label="Accent colour" hidden id="ap"`,
		`value="indigo"`, `value="rose"`, `bk-accent-picker__dot--rose`,
		`data-brick-type="CopyButton"`, `&#34;text&#34;:&#34;hi&#34;`, `aria-label="Copy key" title="Copy key" hidden`,
		`&#34;target&#34;:&#34;#t&#34;`, `class="bk-copy-button bk-copy-button--outline" hidden`, `<span class="bk-copy-button__text">Copy</span>`, `aria-live="polite"`,
		`<input type="checkbox" class="bk-swap__input" aria-label="Menu" name="m" value="on" checked>`,
		`<span class="bk-swap__off" aria-hidden="true">Follow</span><span class="bk-swap__on" aria-hidden="true">Following</span>`,
		`class="bk-button-group bk-button-group--vertical" role="group" aria-label="Style"`,
		`<a href="/x" class="bk-btn bk-btn--primary" aria-disabled="true" tabindex="-1"`,
	)
	if n := strings.Count(got, `bk-accent-picker__swatch`); n != 2 {
		t.Errorf("accents: %d swatches, want 2", n)
	}
	if strings.Contains(got[strings.Index(got, "bk-copy-button--outline"):], `aria-label="Copy"`) {
		t.Error("a labelled CopyButton needs no aria-label")
	}
}

// Layout: variants become classes, blocks render only when passed, and
// href turns a card into one link.
func TestKitLayout(t *testing.T) {
	t.Parallel()
	got := fndRender(t, `<Container size="sm" :padding="false">c</Container>|<Stack direction="row" gap="sm" align="start" justify="between" wrap>s</Stack>|<Stack>d</Stack>|`+
		`<Grid :cols="2" md="3" :lg="4" gap="lg">g</Grid>|`+
		`<Card title="T" description="D" variant="elevated" padding="lg" hx-get="/c">body<brick:prop name="footer">F</brick:prop><brick:prop name="actions">A</brick:prop></Card>|`+
		`<Card href="/p" title="L" />|<Card>only</Card>|`+
		`<Hero title="H" subtitle="S" layout="split" :backdrop="false"><brick:prop name="media">M</brick:prop></Hero>|`+
		`<PageHeader title="P" divider><brick:prop name="breadcrumbs">a</brick:prop><brick:prop name="actions">b</brick:prop></PageHeader>|`+
		`<Section title="S" :level="3" align="center">x</Section>|<Footer variant="muted"><div>c</div><brick:prop name="bottom">b</brick:prop></Footer>|`+
		`<AspectRatio ratio="4/3" rounded>i</AspectRatio>|<AspectRatio>j</AspectRatio>|<ScrollArea label="Log" height="sm" bordered>x</ScrollArea>|`+
		`<Indicator :count="120" label="many">i</Indicator>|<Indicator :count="5" dot tone="success" position="bottom-start">d</Indicator>|<Indicator text="New">n</Indicator>|<Indicator>none</Indicator>`)
	fndHas(t, got,
		`<div class="bk-container bk-container--sm bk-container--flush" >c</div>`,
		`<div class="bk-stack bk-stack--row bk-stack--wrap bk-stack--gap-sm bk-stack--align-start bk-stack--justify-between" >s</div>`,
		`<div class="bk-stack" >d</div>`,
		`<div class="bk-grid bk-grid--cols-2 bk-grid--md-3 bk-grid--lg-4 bk-grid--gap-lg" >g</div>`,
		`<div class="bk-card bk-card--elevated bk-card--lg" hx-get="/c">`,
		`<h3 class="bk-card__title">T</h3><p class="bk-card__description">D</p>`,
		`<div class="bk-card__footer">F<div class="bk-card__actions">A</div></div>`,
		`<a href="/p" class="bk-card bk-card--default" >`,
		`<div class="bk-card bk-card--default" >`, `<div class="bk-card__body">only</div>`,
		`<section class="bk-hero bk-hero--split" >`, `<h1 class="bk-hero__title">H</h1>`, `<div class="bk-hero__media">M</div>`,
		`<header class="bk-page-header bk-page-header--divider" >`, `<nav class="bk-page-header__crumbs" aria-label="Breadcrumb">a</nav>`, `<h1 class="bk-page-header__title">P</h1>`,
		`class="bk-section bk-section--center" aria-labelledby=`, `<h3 class="bk-section__title" id=`,
		`<footer class="bk-footer bk-footer--muted" >`, `<div class="bk-footer__cols"><div>c</div></div>`, `<div class="bk-footer__bottom">b</div>`,
		`<div class="bk-aspect-ratio bk-aspect-ratio--4x3 bk-aspect-ratio--rounded" >i</div>`, `<div class="bk-aspect-ratio" >j</div>`,
		`class="bk-scroll-area bk-scroll-area--vertical bk-scroll-area--sm bk-scroll-area--bordered" tabindex="0" role="region" aria-label="Log"`,
		`aria-hidden="true">99+</span><span class="bk-sr-only">many</span>`,
		`bk-indicator--bottom-start`, `bk-indicator__badge--success bk-indicator__badge--dot" aria-hidden="true"></span>`,
		`aria-hidden="true">New</span>`,
		`<span class="bk-indicator" >none</span>`,
	)
	for _, absent := range []string{"bk-card__media", "bk-hero__actions", "bk-hero__eyebrow", "bk-footer__brand"} {
		if strings.Contains(got, absent) {
			t.Errorf("%s rendered without its block", absent)
		}
	}
	if err := renderErr(t, newEngine(t, kitOptions(t, `<Grid :cols="13">x</Grid>`)), "content/a.html"); err == nil {
		t.Error("13 columns must be an error")
	}
}

// AccentPicker in a browser: shows itself, checks the current accent, and a
// choice sets <html data-accent>, saves it and syncs every other picker.
func TestKitAccentPickerInBrowser(t *testing.T) {
	runKitJS(t, `<AccentPicker id="a" /><AccentPicker id="b" size="sm" />{{ assets }}`, "kit_accent_picker.mjs")
}

// CopyButton in a browser: copies its text or its target's text, shows the
// check and the copied label, and resets after two seconds.
func TestKitCopyButtonInBrowser(t *testing.T) {
	runKitJS(t, `<CopyButton id="a" text="go get brick" /><pre id="src">  npm i brick  </pre><CopyButton id="b" target="#src" showLabel copiedLabel="Done" />{{ assets }}`, "kit_copy_button.mjs")
}
