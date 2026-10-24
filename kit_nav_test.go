package brick

import (
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// navRender renders page over the kit.
func navRender(t *testing.T, page string) string {
	t.Helper()
	return render(t, newEngine(t, kitOptions(t, page)), "content/a.html", nil)
}

// navWantAll fails for each want missing from got, and each unwant present.
func navWantAll(t *testing.T, got string, wants []string, unwants ...string) {
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

// Tabs: the variant and size become classes, selected is decided on the
// server (aria-selected, roving tabindex and hidden panels agree), icons,
// badges and disabled tabs come from the Tab children.
func TestKitTabsPanels(t *testing.T) {
	t.Parallel()
	got := navRender(t, `<Tabs variant="segmented" size="sm" :selected="1" full><Tab title="A" icon="inbox" badge="3">one</Tab><Tab title="B">two</Tab><Tab title="C" disabled>three</Tab></Tabs>`)
	navWantAll(t, got, []string{
		`class="bk-tabs bk-tabs--segmented bk-tabs--sm bk-tabs--full"`,
		"aria-selected=\"false\" tabindex=\"-1\"><svg class=\"bk-icon\"", `A<span class="bk-tabs__badge">3</span></button>`,
		`aria-selected="true" tabindex="0">B</button>`,
		`tabindex="-1" disabled>C</button>`,
		`tabindex="0" hidden><h3 class="bk-tabs__ptitle">A</h3>one</div>`,
		`tabindex="0"><h3 class="bk-tabs__ptitle">B</h3>two</div>`,
		`<noscript>`,
	})
}

// Link mode: any Tab with href makes the set a nav of real links. The one
// matching current is aria-current and the only body rendered; there is no
// tablist for the script to drive.
func TestKitTabsLinks(t *testing.T) {
	t.Parallel()
	got := navRender(t, `<Tabs current="/b/" label="Settings"><Tab title="A" href="/a/">body a</Tab><Tab title="B" href="/b/">body b</Tab></Tabs>`)
	navWantAll(t, got, []string{
		`<nav class="bk-tabs__list" aria-label="Settings">`,
		`<a class="bk-tabs__tab" href="/a/">A</a>`,
		`<a class="bk-tabs__tab" href="/b/" aria-current="page">B</a>`,
		`<div class="bk-tabs__panels">body b</div>`,
	}, `body a`, `role="tablist"`, `role="tabpanel"`)
	// Without current, selected picks the link.
	got = navRender(t, `<Tabs :selected="0"><Tab title="A" href="/a/">body a</Tab><Tab title="B" href="/b/">body b</Tab></Tabs>`)
	navWantAll(t, got, []string{`href="/a/" aria-current="page"`, `>body a</div>`}, `body b`)
}

// SegmentedControl is native radios: one name, the value checked, a
// disabled option, and no script at all.
func TestKitSegmentedControl(t *testing.T) {
	t.Parallel()
	got := navRender(t, `<SegmentedControl name="view" label="View" :options="list `+"`A` `B` (dict `value` `c` `label` `C` `icon` `grid` `hideLabel` true `disabled` true)"+`" value="B" />{{ assets }}`)
	navWantAll(t, got, []string{
		`<fieldset class="bk-segmented"`, `<legend class="bk-segmented__legend">View</legend>`,
		`type="radio" name="view" value="A">`, `type="radio" name="view" value="B" checked>`,
		`value="c" disabled aria-label="C">`,
	}, `/_brick/c/SegmentedControl.`+"js", `SegmentedControl.js`)
	if strings.Count(got, `type="radio"`) != 3 {
		t.Errorf("want 3 radios:\n%s", got)
	}
}

// Breadcrumbs: the last item is the current page and not a link; past
// max, the middle folds into a <details> holding exactly the hidden links.
func TestKitBreadcrumbs(t *testing.T) {
	t.Parallel()
	items := "list (dict `label` `Home` `href` `/`) (dict `label` `A` `href` `/a`) (dict `label` `B` `href` `/b`) (dict `label` `C` `href` `/c`) (dict `label` `D` `href` `/d`) (dict `label` `Here`)"
	got := navRender(t, `<Breadcrumbs :items="`+items+`" />`)
	navWantAll(t, got, []string{`<li aria-current="page">Here</li>`, `<a href="/b">B</a>`}, `bk-breadcrumbs__more`)
	got = navRender(t, `<Breadcrumbs separator="slash" :max="3" :items="`+items+`" />`)
	navWantAll(t, got, []string{
		`class="bk-breadcrumbs bk-breadcrumbs--slash"`,
		`<summary aria-label="Show 3 more">…</summary><div class="bk-breadcrumbs__menu"><a href="/a">A</a><a href="/b">B</a><a href="/c">C</a></div>`,
		`<li><a href="/">Home</a></li>`, `<li><a href="/d">D</a></li>`, `<li aria-current="page">Here</li>`,
	})
	if n := strings.Count(got, "<li"); n != 4 {
		t.Errorf("max=3 shows the first, a fold and the last two: %d items\n%s", n, got)
	}
}

// Sidebar provides current; each item compares its own href. A nested
// parent opens around the current page; collapsed adds tooltips; an item
// outside a Sidebar is active only when told.
func TestKitSidebarActive(t *testing.T) {
	t.Parallel()
	got := navRender(t, `<Sidebar current="/p/b/"><SidebarGroup title="W"><SidebarItem href="/a/" label="A" badge="2" /><SidebarItem label="P" href="/p/"><SidebarItem href="/p/b/" label="B" /></SidebarItem><SidebarItem label="Go" /></SidebarGroup></Sidebar>`)
	navWantAll(t, got, []string{
		`<nav class="bk-sidebar" aria-label="Sidebar"`,
		`role="group" aria-labelledby=`,
		`<a class="bk-sidebar-item" href="/a/" ><span class="bk-sidebar-item__label">A</span><span class="bk-sidebar-item__badge">2</span></a>`,
		`<details class="bk-sidebar-item__sub" open >`,
		`href="/p/b/" aria-current="page"`,
		`<button type="button" class="bk-sidebar-item" ><span class="bk-sidebar-item__label">Go</span></button>`,
	})
	if n := strings.Count(got, `aria-current="page"`); n != 1 {
		t.Errorf("exactly one active item, got %d", n)
	}
	got = navRender(t, `<Sidebar current="/a/" collapsed><SidebarItem href="/a/" label="A" icon="inbox" /></Sidebar><SidebarItem href="/a/" label="Alone" /><SidebarItem href="/x/" label="Told" active />`)
	navWantAll(t, got, []string{`bk-sidebar--collapsed`, `aria-current="page" title="A"`, `href="/a/" ><span class="bk-sidebar-item__label">Alone`, `href="/x/" aria-current="page" >`})
	got = navRender(t, `<SidebarGroup title="X" collapsible :open="false"><SidebarItem label="i" /></SidebarGroup>`)
	navWantAll(t, got, []string{`<details class="bk-sidebar-group" ><summary class="bk-sidebar-group__title">`})
}

// Steps: statuses follow current unless an item says otherwise; the
// current step is aria-current and every state is spelled out in text.
func TestKitSteps(t *testing.T) {
	t.Parallel()
	got := navRender(t, `<Steps :current="1" :items="list (dict `+"`title` `A`) (dict `title` `B`) (dict `title` `C`) (dict `title` `D` `status` `error`)"+`" orientation="vertical" />`)
	navWantAll(t, got, []string{
		`class="bk-steps bk-steps--vertical"`,
		`<li class="bk-step bk-step--complete">`, `<li class="bk-step bk-step--current" aria-current="step"><span class="bk-step__dot" aria-hidden="true">2</span>`,
		`<li class="bk-step bk-step--upcoming"><span class="bk-step__dot" aria-hidden="true">3</span>`,
		`<li class="bk-step bk-step--error">`, `(complete)`, `(current)`, `(upcoming)`, `(error)`,
	})
}

// Navbar, AppShell, Menu and Dock: the no-JavaScript menus are popovers
// whose button targets them by an id unique to the instance; current marks
// exactly the matching link.
func TestKitNavLayouts(t *testing.T) {
	t.Parallel()
	got := navRender(t, `<Navbar current="/b" sticky :items="list (dict `+"`label` `A` `href` `/a`) (dict `label` `B` `href` `/b`)"+`"><brick:prop name="brand">Acme</brick:prop></Navbar><Navbar />`)
	ids := regexp.MustCompile(`id="([^"]+)-menu" popover`).FindAllStringSubmatch(got, -1)
	if len(ids) != 2 || ids[0][1] == ids[1][1] {
		t.Fatalf("two navbars need two menu ids: %v", ids)
	}
	navWantAll(t, got, []string{`bk-navbar--sticky`, `popovertarget="` + ids[0][1] + `-menu"`, `href="/b" aria-current="page">B</a>`, `<div class="bk-navbar__brand">Acme</div>`}, `href="/a" aria-current`)

	got = navRender(t, `<AppShell><brick:prop name="sidebar">S</brick:prop><brick:prop name="topbar">T</brick:prop>M</AppShell><AppShell :landmark="false">N</AppShell>`)
	id := regexp.MustCompile(`id="([^"]+)-nav" popover`).FindStringSubmatch(got)
	if id == nil {
		t.Fatalf("no drawer popover:\n%s", got)
	}
	navWantAll(t, got, []string{`popover>S</aside>`, `popovertarget="` + id[1] + `-nav"`, `T</header>`, `<main class="bk-appshell__main">M</main>`, `<div class="bk-appshell__main">N</div>`})

	got = navRender(t, `<Menu current="/b" :items="list (dict `+"`section` `S`) (dict `label` `A` `href` `/a` `kbd` `K`) (dict `divider` true) (dict `label` `B` `href` `/b`) (dict `label` `C` `disabled` true)"+`" />`)
	navWantAll(t, got, []string{`<li class="bk-menu__section">S</li>`, `<kbd class="bk-menu__kbd">K</kbd>`, `<li class="bk-menu__divider" aria-hidden="true"></li>`, `href="/b" aria-current="page"`, `<button type="button" class="bk-menu__item" disabled>`}, `href="/a" aria-current`, `role="presentation"`, `role="separator"`)

	got = navRender(t, `<Dock current="/b" :fixed="false" :items="list (dict `+"`label` `A` `href` `/a` `icon` `home` `badge` `2`) (dict `label` `B` `href` `/b`)"+`" />`)
	navWantAll(t, got, []string{`<nav class="bk-dock" aria-label="Primary"`, `<span class="bk-dock__badge">2</span>`, `href="/b" aria-current="page"`}, `bk-dock--fixed`, `href="/a" aria-current`)
}

// Mockups: figures with their chrome marked decorative; the terminal's
// raw body keeps markup as text and splits commands from output.
func TestKitMockups(t *testing.T) {
	t.Parallel()
	got := navRender(t, `<BrowserMockup url="acme.com">x</BrowserMockup><WindowMockup title="Prefs" controls="windows">y</WindowMockup><PhoneMockup notch="notch" time="12:30">z</PhoneMockup>`)
	navWantAll(t, got, []string{
		`<figure class="bk-browser"`, `<div class="bk-browser__bar" aria-hidden="true">`, `acme.com</span>`, `<figcaption class="bk-sr-only">acme.com</figcaption>`, `<div class="bk-browser__content">x</div>`,
		`class="bk-window bk-window--windows" aria-labelledby=`, `<span class="bk-window__ctl" aria-hidden="true">`, `<div class="bk-window__content">y</div>`,
		`class="bk-phone bk-phone--notch"`, `<span>12:30</span><i class="bk-phone__island"></i>`, `<div class="bk-phone__content">z</div>`,
	})
	got = navRender(t, "<TerminalMockup title=\"sh\" typing>\n$ echo \"<b>{{ x }}</b>\"\n<b>{{ x }}</b>\n</TerminalMockup>")
	navWantAll(t, got, []string{
		`class="bk-terminal bk-terminal--typing"`, `<span class="bk-terminal__title">sh</span>`,
		`<span class="bk-terminal__prompt">$</span><span class="bk-terminal__text">echo &#34;&lt;b&gt;{{ x }}&lt;/b&gt;&#34;</span>`,
		`bk-terminal__out" style="--start: `, `&lt;b&gt;{{ x }}&lt;/b&gt;</span>`,
	})
}

var navPctRe = regexp.MustCompile(`(height|width|bottom|left): ([0-9.]+)%`)

// navPercents returns every "prop: N%" of one property in s.
func navPercents(s, prop string) []float64 {
	var out []float64
	for _, m := range navPctRe.FindAllStringSubmatch(s, -1) {
		if m[1] == prop {
			f, _ := strconv.ParseFloat(m[2], 64)
			out = append(out, f)
		}
	}
	return out
}

// BarChart geometry: bar heights are the values' share of a round axis
// top, ticks are evenly spaced round numbers, a stack's segments add up
// and its total is the label, and horizontal bars use widths.
func TestKitBarChartGeometry(t *testing.T) {
	t.Parallel()
	got := navRender(t, `<BarChart :data="list (dict `+"`label` `a` `value` 0) (dict `label` `b` `value` 50) (dict `label` `c` `value` 100)"+`" labels prefix="$" />`)
	bars := strings.Split(got, `class="bk-bar-chart__bar"`)[1:]
	var hs []float64
	for _, b := range bars {
		hs = append(hs, navPercents(b, "height")[0])
	}
	if len(hs) != 3 || hs[0] != 0 || hs[1] != 50 || hs[2] != 100 {
		t.Errorf("bar heights for 0/50/100: %v", hs)
	}
	navWantAll(t, got, []string{`<span>$0</span>`, `<span>$100</span>`, `data-v="$50"`, `title="b&#10;value: $50"`, `<caption>Bar chart</caption>`, `<td>$100</td>`})
	if ticks := navPercents(strings.Split(got, `bk-bar-chart__cols`)[0], "bottom"); len(ticks) != 6 || ticks[1] != 20 {
		t.Errorf("0..100 in at most 5 steps of 20: %v", ticks)
	}

	// A round top: 412 fits 5 steps of 100, so the axis ends at 500.
	got = navRender(t, `<BarChart orientation="horizontal" :data="list (dict `+"`label` `a` `value` 412) (dict `label` `b` `value` 103)"+`" />`)
	navWantAll(t, got, []string{`<span>500</span>`, `width: 82.40%`, `width: 20.60%`, `class="bk-bar-chart__rl">a</span>`}, `<span>600</span>`)

	// Stacked: segments share the axis, zero segments are left out, the
	// top segment carries the stack total, and two series get a legend.
	got = navRender(t, `<BarChart stacked labels :data="list (dict `+"`label` `a` `x` 30 `y` 10) (dict `label` `b` `x` 20 `y` 0)"+`" :series="list (dict `+"`key` `x` `label` `X`) (dict `key` `y` `label` `Y` `tone` `danger`)"+`" />`)
	a := strings.Split(strings.Split(got, `title="a`)[1], `title="b`)[0]
	if h := navPercents(a, "height"); len(h) != 2 || h[0] != 75 || h[1] != 25 {
		t.Errorf("stack a (30+10 of 40): %v", h)
	}
	navWantAll(t, a, []string{`data-tone="danger" style="height: 25.00%" data-v="40"`})
	b := strings.Split(got, `title="b`)[1]
	if h := navPercents(strings.Split(b, "</div></div>")[0], "height"); len(h) != 1 {
		t.Errorf("stack b has one non-zero segment: %v", h)
	}
	navWantAll(t, got, []string{`class="bk-chart bk-chart-legend"`, `data-tones="1 danger"`})
}

// LineChart: one path point per datum (M then L per step, or C when
// smooth), one marker per datum and series, and areas drawn before lines.
func TestKitLineChartGeometry(t *testing.T) {
	t.Parallel()
	data := "list (dict `label` `a` `v` 1 `w` 2) (dict `label` `b` `v` 3 `w` 1) (dict `label` `c` `v` 2 `w` 4) (dict `label` `d` `v` 4 `w` 3)"
	got := navRender(t, `<LineChart :data="`+data+`" :series="list (dict `+"`key` `v` `label` `V`) (dict `key` `w` `label` `W`)"+`" area />`)
	lines := regexp.MustCompile(`class="bk-line-chart__line" d="([^"]+)"`).FindAllStringSubmatch(got, -1)
	if len(lines) != 2 {
		t.Fatalf("two lines: %d", len(lines))
	}
	for _, l := range lines {
		if n := len(regexp.MustCompile(`[ML]`).FindAllString(l[1], -1)); n != 4 {
			t.Errorf("path %q: %d points, want 4", l[1], n)
		}
	}
	if strings.LastIndex(got, "bk-line-chart__area") > strings.Index(got, "bk-line-chart__line") {
		t.Error("an area is drawn over a line")
	}
	if n := strings.Count(got, `class="bk-line-chart__pt"`); n != 8 {
		t.Errorf("markers: %d, want 8", n)
	}
	// Point positions: 4 of a 0..4 axis is the top.
	navWantAll(t, got, []string{`style="bottom: 100.00%"`, `title="b&#10;V: 3&#10;W: 1"`, `bk-chart-legend--line`})
	got = navRender(t, `<LineChart :data="`+data+`" y="v" smooth />`)
	d := regexp.MustCompile(`class="bk-line-chart__line" d="([^"]+)"`).FindStringSubmatch(got)[1]
	if strings.Count(d, "C") != 3 || strings.Count(d, "M") != 1 {
		t.Errorf("smooth path: %q", d)
	}
}

// DonutChart: the arcs and the gaps between them add up to the whole
// circle, each arc is its value's share, and zero values are left out.
func TestKitDonutArcs(t *testing.T) {
	t.Parallel()
	got := navRender(t, `<DonutChart :data="list (dict `+"`label` `a` `value` 50) (dict `label` `b` `value` 30) (dict `label` `c` `value` 20) (dict `label` `z` `value` 0)"+`" />`)
	r, _ := strconv.ParseFloat(regexp.MustCompile(`bk-donut-chart__track" cx="100" cy="100" r="([0-9.]+)"`).FindStringSubmatch(got)[1], 64)
	circ := 2 * math.Pi * r
	arcs := regexp.MustCompile(`stroke-dasharray="([0-9.]+) ([0-9.]+)"`).FindAllStringSubmatch(got, -1)
	if len(arcs) != 3 {
		t.Fatalf("three non-zero segments: %d", len(arcs))
	}
	const gap = 2.0
	sum := 0.0
	for i, a := range arcs {
		l, _ := strconv.ParseFloat(a[1], 64)
		sum += l + gap
		if share := []float64{.5, .3, .2}[i]; math.Abs(l+gap-share*circ) > 0.01 {
			t.Errorf("arc %d: %.3f, want %.3f", i, l+gap, share*circ)
		}
	}
	if math.Abs(sum-circ) > 0.01 {
		t.Errorf("arcs + gaps = %.3f, circumference %.3f", sum, circ)
	}
	navWantAll(t, got, []string{`<span class="bk-donut-chart__value">100</span>`, `<span class="bk-chart-legend__pct">50%</span>`, `<td>0%</td>`})
	got = navRender(t, `<DonutChart variant="pie" legend="none" :data="list (dict `+"`label` `a` `value` 1)"+`" />`)
	navWantAll(t, got, []string{`stroke-width="92.000"`, `stroke-dashoffset="-0.000"`}, `bk-donut-chart__center`, `bk-chart-legend`)
}

// Sparkline: numbers or maps, min..max for lines, zero-based bars, and an
// accessible name that states the values.
func TestKitSparkline(t *testing.T) {
	t.Parallel()
	got := navRender(t, `<Sparkline :data="list 2 4 6" label="Sales" /><Sparkline variant="bar" :data="list (dict `+"`value` 1) (dict `value` 4)"+`" />`)
	navWantAll(t, got, []string{`aria-label="Sales: 2 to 6, low 2, high 6"`, `d="M0.00 92.00 L50.00 50.00 L100.00 8.00"`, `style="left: 100.00%; bottom: 92.00%"`, `<i style="height: 25.00%"></i><i style="height: 100.00%"></i>`})
}

// Empty data is an empty state, never a crash or a divide by zero.
func TestKitChartsEmpty(t *testing.T) {
	t.Parallel()
	got := navRender(t, `<BarChart /><BarChart orientation="horizontal" :data="list (dict `+"`label` `a` `value` 0)"+`" /><LineChart empty="Nothing yet" /><DonutChart :data="list (dict `+"`label` `a` `value` 0)"+`" /><Sparkline /><ChartLegend />`)
	if n := strings.Count(got, `class="bk-chart__empty"`); n != 3 {
		t.Errorf("three empty states, got %d:\n%s", n, got)
	}
	navWantAll(t, got, []string{`>Nothing yet</div>`, `width: 0.00%`, `aria-label="Trend"`})
}

// The charts share one palette, tone map and tooltip. Each file carries
// its own copy (a kit file must work alone), so the copies must not drift.
func TestKitChartSharedCode(t *testing.T) {
	t.Parallel()
	block := func(src, from, to string) string {
		i := strings.Index(src, from)
		j := strings.Index(src, to)
		if i < 0 || j < i {
			return ""
		}
		return src[i:j]
	}
	var base, tip string
	for _, name := range []string{"BarChart", "LineChart", "DonutChart", "Sparkline", "ChartLegend"} {
		b, err := os.ReadFile(filepath.Join(kitDir, name+".html"))
		if err != nil {
			t.Fatal(err)
		}
		src := string(b)
		got := block(src, "/* bk-chart base", "/* end bk-chart base */")
		if got == "" {
			t.Errorf("%s: no bk-chart base block", name)
		} else if base == "" {
			base = got
		} else if got != base {
			t.Errorf("%s: the bk-chart base block differs from BarChart's", name)
		}
		if name == "Sparkline" || name == "ChartLegend" {
			continue
		}
		got = block(src, "// The tooltip", "brick.mount(")
		if got == "" {
			t.Errorf("%s: no tooltip", name)
		} else if tip == "" {
			tip = got
		} else if got != tip {
			t.Errorf("%s: the tooltip differs from BarChart's", name)
		}
	}
}

// With JavaScript: the chart tooltip on hover and on the keyboard, for all
// three charts that share it (runtime/test/kit_charts.mjs).
func TestKitChartsInBrowser(t *testing.T) {
	runKitJS(t, `<BarChart prefix="$" :data="list (dict `+"`label` `Jan` `web` 10 `app` 2) (dict `label` `Feb` `web` 30 `app` 5) (dict `label` `Mar` `web` 20 `app` 1)"+`" x="label" :series="list (dict `+"`key` `web` `label` `Web`) (dict `key` `app` `label` `App` `tone` `danger`)"+`" />`+
		`<LineChart :data="list (dict `+"`label` `Jan` `a` 1 `b` 2) (dict `label` `Feb` `a` 3 `b` 4) (dict `label` `Mar` `a` 12 `b` 7)"+`" :series="list (dict `+"`key` `a` `label` `A`) (dict `key` `b` `label` `B`)"+`" />`+
		`<DonutChart name="Visits" :data="list (dict `+"`label` `Chrome` `value` 75) (dict `label` `Safari` `value` 25)"+`" />{{ assets }}`, "kit_charts.mjs")
}

// With JavaScript: Tabs starts on the server's selection, skips disabled
// tabs, leaves link tabs alone and fades an overflowing bar's hidden side
// (runtime/test/kit_nav.mjs).
func TestKitNavInBrowser(t *testing.T) {
	runKitJS(t, `<Tabs :selected="1"><Tab title="A">a</Tab><Tab title="B">b</Tab><Tab title="C" disabled>c</Tab><Tab title="D">d</Tab></Tabs><Tabs current="/b/"><Tab title="A" href="/a/" /><Tab title="B" href="/b/" /></Tabs><Tabs><Tab title="One">1</Tab><Tab title="Two">2</Tab><Tab title="Three">3</Tab></Tabs>{{ assets }}`, "kit_nav.mjs")
}

// prefetch on an item (true, or "visible") writes brick:prefetch on that
// item's link and no other, in every component whose links come from data
// or from Tab props.
func TestKitNavPrefetch(t *testing.T) {
	t.Parallel()
	items := "list (dict `label` `A` `href` `/a` `prefetch` true) (dict `label` `B` `href` `/b` `prefetch` `visible`) (dict `label` `C` `href` `/c`) (dict `label` `D`)"
	for _, page := range []string{
		`<Menu :items="` + items + `" />`,
		`<Dock :items="` + items + `" />`,
		`<Breadcrumbs :items="` + items + `" />`,
		`<Tabs><Tab title="A" href="/a" prefetch /><Tab title="B" href="/b" prefetch="visible" /><Tab title="C" href="/c" /></Tabs>`,
	} {
		got := navRender(t, page)
		navWantAll(t, got, []string{`href="/a" brick:prefetch`, `href="/b" brick:prefetch="visible"`}, `href="/c" brick:prefetch`)
		if n := strings.Count(got, "brick:prefetch"); n != 2 {
			t.Errorf("%s: %d prefetch attributes, want 2\n%s", page, n, got)
		}
	}
}
