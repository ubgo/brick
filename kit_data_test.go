package brick

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// dataRender renders one page over the kit and returns the HTML.
func dataRender(t *testing.T, page string) string {
	t.Helper()
	return render(t, newEngine(t, kitOptions(t, page)), "content/a.html", nil)
}

// dataHas fails for each want missing from got.
func dataHas(t *testing.T, got string, wants ...string) {
	t.Helper()
	for _, w := range wants {
		if !strings.Contains(got, w) {
			t.Errorf("missing %q in\n%s", w, got)
		}
	}
}

// Table draws a cell from the row's value at the column's key, lets a
// "cell_<key>" slot draw one column and a "cell" slot the rest, and shows
// one empty row when there are no rows.
func TestKitTable(t *testing.T) {
	t.Parallel()
	got := dataRender(t, `---
cols: [{key: name, label: Name}, {key: n, label: Count, align: right, width: 6rem}, {key: status, label: Status}]
rows: [{name: Ava, n: 0, status: active}, {name: Liam, n: 12, status: lead}]
---
<Table :columns=".Params.cols" :rows=".Params.rows" hover striped id="t"><brick:prop name="cell_status"><b>{{ .Slot.value }}</b>/{{ .Slot.row.name }}/{{ .Slot.col.label }}</brick:prop></Table>
<Table :columns=".Params.cols" :rows="list" empty="Nobody here." />
<Table :columns=".Params.cols" :rows=".Params.rows"><brick:prop name="cell">[{{ .Slot.value }}]</brick:prop></Table>
<Table compact><tbody><tr><td>plain</td></tr></tbody></Table>`)
	dataHas(t, got,
		`<div class="bk-table bk-table--striped bk-table--hover" id="t">`,
		`<th scope="col" class="bk-table__right" style="width: 6rem">Count</th>`,
		`<td>Ava</td><td class="bk-table__right">0</td><td><b>active</b>/Ava/Status</td>`,
		`<td class="bk-table__empty" colspan="3">Nobody here.</td>`,
		`<td>[Liam]</td><td class="bk-table__right">[12]</td>`,
		`<div class="bk-table bk-table--compact" >`, `<tbody><tr><td>plain</td></tr></tbody>`,
	)
	// A table with a height scrolls, so keyboard users can reach it and
	// screen readers announce it: a focusable region named by label or caption.
	box := dataRender(t, `<Table :columns="list (dict `+"`key` `a` `label` `A`"+`)" :rows="list" height="10rem" caption="People" /><Table height="5rem" label="Log"><tbody></tbody></Table>`)
	dataHas(t, box, `style="max-height: 10rem" tabindex="0" role="region" aria-label="People"`, `style="max-height: 5rem" tabindex="0" role="region" aria-label="Log"`)
	if strings.Contains(dataRender(t, `<Table><tbody></tbody></Table>`), `tabindex`) {
		t.Error("a table without a height is focusable")
	}
}

// DataTable renders every row with its raw value for sorting, a checkbox
// per row when selectable, and hides the toolbar and pager until the
// client module shows them.
func TestKitDataTable(t *testing.T) {
	t.Parallel()
	got := dataRender(t, `---
cols: [{key: name, label: Name}, {key: n, label: N, sortable: false}]
rows: [{id: 7, name: Ava, n: 3}, {id: 9, name: Liam, n: 12}]
---
<DataTable :columns=".Params.cols" :rows=".Params.rows" selectable :pageSize="25" label="People"><brick:prop name="cell_name"><i>{{ .Slot.value }}</i></brick:prop></DataTable>{{ assets }}`)
	dataHas(t, got,
		`data-brick-type="DataTable"`, `&#34;pageSize&#34;:25`, `role="region" aria-label="People"`,
		`<div class="bk-dt__toolbar" hidden>`, `<div class="bk-dt__footer" hidden>`,
		`<button type="button" class="bk-dt__sort">Name`, `<th scope="col" data-col="1">N</th>`,
		`name="selected" value="7" aria-label="Select Ava"`, `name="selected" value="9"`,
		`<td data-col="0" data-value="Ava"><i>Ava</i></td>`, `<td data-col="1" data-value="12">12</td>`,
		`<tr class="bk-dt__none" hidden><td class="bk-table__empty" colspan="3">`,
		`<option>10</option><option selected>25</option>`, `/_brick/c/DataTable.`, `/_brick/c/Table.`,
		`<nav class="bk-dt__pager" aria-label="People pagination"></nav>`,
	)
	// Without a label the pager keeps the generic name.
	dataHas(t, dataRender(t, `<DataTable :columns="list (dict `+"`key` `a` `label` `A`"+`)" />`), `<nav class="bk-dt__pager" aria-label="Pagination"></nav>`)
}

// dataPaginationLinks returns the page links' text in order, with "…" for a gap.
func dataPaginationLinks(html string) string {
	re := regexp.MustCompile(`<a class="bk-pagination__item" href="[^"]*"( aria-current="page")?>(\d+)</a>|<li class="bk-pagination__gap"`)
	var out []string
	for _, m := range re.FindAllStringSubmatch(html, -1) {
		switch {
		case m[2] == "":
			out = append(out, "…")
		case m[1] != "":
			out = append(out, "["+m[2]+"]")
		default:
			out = append(out, m[2])
		}
	}
	return strings.Join(out, " ")
}

// Pagination: the window keeps the first and last page and the current
// one's siblings, a gap of one page shows that page rather than "…", the
// arrows at the ends are disabled, page is clamped, and the summary covers
// the empty and the last, partial page.
func TestKitPagination(t *testing.T) {
	t.Parallel()
	cases := []struct {
		call, links, summary string
		prevOff, nextOff     bool
	}{
		{`<Pagination :page="1" :total="312" />`, "[1] 2 … 16", "<b>1</b>–<b>20</b> of <b>312</b>", true, false},
		{`<Pagination :page="8" :total="312" />`, "1 … 7 [8] 9 … 16", "<b>141</b>–<b>160</b> of <b>312</b>", false, false},
		{`<Pagination :page="4" :total="312" />`, "1 2 3 [4] 5 … 16", "<b>61</b>–<b>80</b>", false, false},
		{`<Pagination :page="16" :total="312" />`, "1 … 15 [16]", "<b>301</b>–<b>312</b> of <b>312</b>", false, true},
		{`<Pagination :page="99" :total="45" :perPage="10" />`, "1 … 4 [5]", "<b>41</b>–<b>45</b> of <b>45</b>", false, true},
		{`<Pagination :page="1" :total="0" />`, "", "No results", true, true},
		{`<Pagination :page="1" :total="3000" :perPage="1" />`, "[1] 2 … 3000", "<b>1</b>–<b>1</b> of <b>3,000</b>", true, false},
	}
	for _, c := range cases {
		got := dataRender(t, c.call)
		if l := dataPaginationLinks(got); l != c.links {
			t.Errorf("%s: links %q, want %q", c.call, l, c.links)
		}
		dataHas(t, got, c.summary)
		if off := strings.Contains(got, `<span class="bk-pagination__item" aria-disabled="true"><svg`); off != c.prevOff {
			t.Errorf("%s: previous disabled = %v, want %v", c.call, off, c.prevOff)
		}
		if off := strings.Contains(got, `<span class="bk-pagination__item" aria-disabled="true"><span class="bk-pagination__text">Next`); off != c.nextOff {
			t.Errorf("%s: next disabled = %v, want %v", c.call, off, c.nextOff)
		}
	}
	// href patterns: %d replaced; otherwise page= joins the query.
	dataHas(t, dataRender(t, `<Pagination :page="2" :total="100" href="/o/page/%d/" edges />`),
		`href="/o/page/1/" rel="prev"`, `href="/o/page/3/" rel="next"`, `href="/o/page/5/" aria-label="Last page"`)
	dataHas(t, dataRender(t, `<Pagination :page="2" :total="100" href="/s?q=go" />`), `href="/s?q=go&amp;page=3" rel="next"`)
	dataHas(t, dataRender(t, `<Pagination :page="2" :total="100" href="/s" />`), `href="/s?page=3" rel="next"`)
	// action: every page link also calls the action with its page number.
	act := dataRender(t, `<Pagination :page="3" :total="64" :perPage="10" action="goto" href="#p%d" edges />`)
	dataHas(t, act, `href="#p1" brick:click="goto(1)" aria-label="First page"`, `href="#p2" brick:click="goto(2)" rel="prev"`,
		`href="#p3" brick:click="goto(3)" aria-current="page"`, `href="#p4" brick:click="goto(4)" rel="next"`, `href="#p7" brick:click="goto(7)" aria-label="Last page"`)
	if strings.Contains(dataRender(t, `<Pagination :page="3" :total="64" />`), "brick:click") {
		t.Error("a pager without action has brick:click")
	}
}

// Avatar: initials are the first letters of the first and last words (by
// rune, so accents survive), the tint depends only on the name, and a
// picture replaces the initials.
func TestKitAvatar(t *testing.T) {
	t.Parallel()
	got := dataRender(t, `<Avatar name="Maya  van Chen" status="busy" id="a" /><Avatar name="émile" /><Avatar name="Liam Kim" src="/l.png" size="lg" shape="square" />`)
	dataHas(t, got,
		`class="bk-avatar bk-avatar--tint bk-avatar--t2" role="img" aria-label="Maya  van Chen (busy)" id="a"><span aria-hidden="true">MC</span><span class="bk-avatar__status bk-avatar__status--busy"></span>`,
		`aria-label="émile" ><span aria-hidden="true">É</span>`,
		`<span class="bk-avatar bk-avatar--lg bk-avatar--square" ><img class="bk-avatar__img" src="/l.png" alt="Liam Kim"></span>`,
	)
	if a, b := dataRender(t, `<Avatar name="Zoe Park" />`), dataRender(t, `<Avatar name="Zoe Park" />`); a != b {
		t.Error("the same name rendered differently")
	}
	grp := dataRender(t, `---
team: [{name: A B}, {name: C D}, {name: E F}, {name: G H}]
---
<AvatarGroup :items=".Params.team" :max="2" /><AvatarGroup :items=".Params.team" :max="2" :total="40" /><AvatarGroup :max="1"><Avatar name="X Y" /><Avatar name="Z W" /></AvatarGroup>`)
	dataHas(t, grp, `aria-label="2 more">+2</span>`, `aria-label="38 more">+38</span>`, `aria-label="1 more">+1</span>`)
	if n := strings.Count(grp, `bk-avatar--tint`); n != 5 {
		t.Errorf("%d avatars shown, want 2+2+1", n)
	}
}

// Badge and Tag: every tone and variant is a class, and a removable tag's
// control is named after the tag's text.
func TestKitBadgeAndTag(t *testing.T) {
	t.Parallel()
	got := dataRender(t, `<Badge>Draft</Badge><Badge tone="success" variant="solid" dot size="sm" pill>Live</Badge><Tag>go</Tag><Tag href="/t/go/">go</Tag><Tag removeHref="?x" tone="primary">status</Tag><Tag removeAction="untag(3)">db</Tag>`)
	dataHas(t, got,
		`<span class="bk-badge bk-badge--neutral bk-badge--soft" >Draft</span>`,
		`<span class="bk-badge bk-badge--success bk-badge--solid bk-badge--sm bk-badge--pill" ><span class="bk-badge__dot" aria-hidden="true"></span>Live</span>`,
		`<span class="bk-tag bk-tag--neutral" >go</span>`, `<a class="bk-tag bk-tag--neutral" href="/t/go/" >go</a>`,
		`class="bk-tag bk-tag--primary bk-tag--removable"`, `<a class="bk-tag__remove" href="?x"`,
		`brick:click="untag(3)"`, `aria-label="Remove" aria-labelledby="`,
	)
	for _, m := range regexp.MustCompile(`aria-labelledby="(\S+)-r (\S+)-t"`).FindAllStringSubmatch(got, -1) {
		if !strings.Contains(got, `id="`+m[2]+`-t">`) {
			t.Errorf("remove control labelled by missing id %s-t", m[2])
		}
	}
	if err := renderErr(t, newEngine(t, kitOptions(t, `<Badge tone="purple">x</Badge>`)), "content/a.html"); !strings.Contains(err.Error(), "is not one of") {
		t.Errorf("a bad tone: %v", err)
	}
}

// Stat reads the trend from the delta's sign, and invert swaps good and bad.
func TestKitStat(t *testing.T) {
	t.Parallel()
	got := dataRender(t, `<Stats variant="joined" :columns="3"><Stat label="A" value="1" delta="+5%" /><Stat label="B" value="2" delta="-5%" /><Stat label="C" value="3" delta="-5%" invert /><Stat label="D" value="4" delta="0" trend="flat" /></Stats>`)
	dataHas(t, got, `class="bk-stats bk-stats--joined bk-stats--cols" style="--bk-stats-cols: 3"`, `bk-stat__delta--flat">0</span>`)
	moods := regexp.MustCompile(`bk-stat__delta--(\w+)`).FindAllStringSubmatch(got, -1)
	var seq []string
	for _, m := range moods {
		seq = append(seq, m[1])
	}
	if strings.Join(seq, ",") != "good,bad,good,flat" {
		t.Errorf("moods %v, want good,bad,good,flat", seq)
	}
}

// Tree renders itself for each level, and current opens every branch
// around the current item and marks it.
func TestKitTree(t *testing.T) {
	t.Parallel()
	got := dataRender(t, `---
t:
  - label: src
    children:
      - {label: main.go, href: /src/main.go}
      - label: lib
        children: [{label: a.go, href: /src/lib/a.go}]
  - label: docs
    children: [{label: x.md, href: /docs/x}]
  - {label: README, href: /readme}
---
<Tree :items=".Params.t" files current="/src/lib/a.go" label="Files" />`)
	if n := strings.Count(got, `<ul class="bk-tree"`); n != 4 {
		t.Errorf("%d lists, want 4 (the tree and three branches)", n)
	}
	if n := strings.Count(got, `<details class="bk-tree__branch" open>`); n != 2 {
		t.Errorf("%d open branches, want src and lib", n)
	}
	dataHas(t, got, `<details class="bk-tree__branch">`, `href="/src/lib/a.go" aria-current="page"`, `aria-label="Files"`)
	if strings.Count(got, `aria-label="Files"`) != 1 {
		t.Error("the label repeats on nested lists")
	}
}

// Banner is a region landmark named regionLabel ("Announcement" by
// default), so several banners on one page can be told apart.
func TestKitBannerRegionLabel(t *testing.T) {
	t.Parallel()
	got := dataRender(t, `<Banner>a</Banner><Banner regionLabel="Maintenance notice" variant="warning">b</Banner>`)
	dataHas(t, got, `role="region" aria-label="Announcement"`, `role="region" aria-label="Maintenance notice"`)
}

// Accordion keeps the markup the showcase and TestKitRendersWithoutJS pin
// for every variant, with the caller's attributes before open and name.
func TestKitAccordion(t *testing.T) {
	t.Parallel()
	got := dataRender(t, `<Accordion title="A" variant="bordered" id="q" open group="g">x</Accordion><Accordion title="B">y</Accordion>`)
	dataHas(t, got,
		`<details class="bk-accordion bk-accordion--bordered" id="q" open name="g">`,
		`<details class="bk-accordion">`, `<summary>B</summary>`, `<div class="bk-accordion__body">y</div>`,
	)
}

// CodeBlock keeps the body raw, numbers lines through CSS, highlights the
// listed lines and hides the copy button until its module runs.
func TestKitCodeBlock(t *testing.T) {
	t.Parallel()
	got := dataRender(t, `<CodeBlock lang="go" title="main.go" lineNumbers highlight="2, 3">
if x < 1 {
    y("{{ raw }}")
}
</CodeBlock>`)
	dataHas(t, got,
		`class="bk-code-block bk-code-block--numbers"`, `<span class="bk-code-block__title">main.go</span><span class="bk-code-block__lang">go</span>`,
		`<button type="button" class="bk-code-block__copy" aria-label="Copy code" hidden>`,
		`<code class="language-go"><span class="bk-code-block__line">if x &lt; 1 {</span>`+"\n"+`<span class="bk-code-block__line bk-code-block__line--hl">    y(&#34;{{ raw }}&#34;)</span>`+"\n"+`<span class="bk-code-block__line bk-code-block__line--hl">}</span></code>`,
	)
	if bare := dataRender(t, `<CodeBlock :copy="false">x</CodeBlock>`); !strings.Contains(bare, `bk-code-block--bare`) || strings.Contains(bare, "bk-code-block__copy") {
		t.Errorf("bare, no copy:\n%s", bare)
	}
}

// The rest render their documented structure without JavaScript.
func TestKitDataDisplayMarkup(t *testing.T) {
	t.Parallel()
	got := dataRender(t, `---
ev: [{title: Placed, time: Sep 2, datetime: "2026-09-02", icon: package, tone: primary}, {title: Shipped, body: On its way}]
dl: [{term: Plan, desc: Pro}]
---
<Timeline :items=".Params.ev" />
<DescriptionList :items=".Params.dl" /><DescriptionList layout="grid" :items=".Params.dl" />
<List variant="bordered"><ListItem title="Maya" description="m@x" avatar="Maya Chen" meta="Admin" /><ListItem title="File" icon="file" href="/f" current /></List>
<ChatBubble side="end" time="09:42" status="Read" tone="success">Yes</ChatBubble>
<Countdown to="2026-12-31T23:59:00Z" label="Launch in" :seconds="false" />
<Diff before="/a.png" after="/b.png" ratio="4/3" :position="30" />
<Skeleton lines="3" width="40%" /><EmptyState title="None" description="Nothing yet."><b>act</b></EmptyState>
<Figure src="/i.png" alt="I" caption="Cap" ratio="16/9" />{{ assets }}`)
	dataHas(t, got,
		`<li class="bk-timeline__item bk-timeline__item--primary">`, `<time class="bk-timeline__time" datetime="2026-09-02">Sep 2</time>`,
		`<li class="bk-timeline__item bk-timeline__item--neutral">`, `<div class="bk-timeline__body">On its way</div>`,
		`<dt>Plan</dt><dd>Pro</dd>`, `<div class="bk-dl__pair"><dt>Plan</dt><dd>Pro</dd></div>`,
		`<ul class="bk-list bk-list--bordered" >`, `<div class="bk-list-item__trail">Admin</div>`,
		`class="bk-list-item bk-list-item--link bk-list-item--current"`, `<a class="bk-list-item__title" href="/f" aria-current="page">File</a>`,
		`class="bk-chat bk-chat--end"`, `<div class="bk-chat__bubble bk-chat__bubble--success">Yes</div>`, `<time>09:42</time> · Read`,
		`<time class="bk-countdown__until" datetime="2026-12-31T23:59:00Z">until Dec 31, 2026 23:59</time>`, `role="timer" hidden`,
		`class="bk-diff bk-diff--4-3" style="--bk-diff-pos: 30%"`, `<input type="range" class="bk-diff__range" min="0" max="100" value="30"`,
		`<span class="bk-skeleton__bone" style="width: 40%;"></span><span class="bk-skeleton__bone" style="width: 40%;"></span><span class="bk-skeleton__bone" style="width: 40%;"></span>`,
		`<p class="bk-empty__title">None</p><p class="bk-empty__desc">Nothing yet.</p><div class="bk-empty__actions"><b>act</b></div>`,
		`<figure class="bk-figure bk-figure--r16-9" >`, `<figcaption class="bk-figure__caption">Cap</figcaption>`,
	)
	if strings.Contains(got, `data-unit="s"`) {
		t.Error(`:seconds="false" still renders the seconds box`)
	}
}

// With JavaScript: DataTable sorts, filters, pages and selects.
func TestKitDataTableInBrowser(t *testing.T) {
	var rows strings.Builder
	for i := 1; i <= 23; i++ {
		rows.WriteString("  - {id: " + strconv.Itoa(i) + ", name: \"P" + strconv.Itoa(i) + "\", n: " + strconv.Itoa((i*7)%23) + ", team: " + []string{"red", "blue", "green"}[i%3] + "}\n")
	}
	runKitJS(t, "---\ncols: [{key: name, label: Name}, {key: n, label: N}, {key: team, label: Team}]\nrows:\n"+rows.String()+"---\n"+
		`<DataTable :columns=".Params.cols" :rows=".Params.rows" selectable :pageSize="10" :pageSizes="list 5 10 25" />{{ assets }}`, "kit_datatable.mjs")
}

// With JavaScript: CodeBlock copies, Countdown ticks, Carousel builds dots
// and Diff moves its divider.
func TestKitDataWidgetsInBrowser(t *testing.T) {
	runKitJS(t, `<CodeBlock lang="go" lineNumbers>a := 1
b := 2</CodeBlock>
<Countdown to="2099-01-01T00:00:00Z" /><Countdown to="2000-01-01T00:00:00Z" done="Over" />
<Carousel><b>1</b><b>2</b><b>3</b></Carousel>
<Diff before="/a.png" after="/b.png" :position="30" />{{ assets }}`, "kit_widgets.mjs")
}
