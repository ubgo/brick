package site

import (
	"bytes"
	"fmt"
	"html"
	"net/url"
	"strings"
	"time"

	"github.com/ubgo/brick"
)

// toolbarTop is how many component names the Dev toolbar lists: enough to
// spot the one rendered in a loop, short enough to stay a glance.
const toolbarTop = 5

// toolbarID is the toolbar element's id. Every toolbar style is scoped
// under it, so the toolbar never restyles the page, and the page's own
// element rules (a reset on details or a) lose to the id's specificity.
const toolbarID = "brick-devbar"

// toolbarKey is the localStorage key that remembers whether the toolbar
// was left open, a per-browser convenience only.
const toolbarKey = "brick-devbar-open"

// pageTiming is what the handler measured around one page response: when
// it started, and whether the Result came from the site cache.
type pageTiming struct {
	start time.Time
	hit   bool
	// output is the route's output format ("xml"); "" for an HTML page.
	output string
	// prolog is an output page's lifted XML prolog.
	prolog string
	// errorPage marks the 404 or error page itself, whose own island
	// answering must not render the error page again.
	errorPage bool
	// form is the instance a no-JavaScript form post left, for a page
	// answered directly (422); nil reads the resume cookie.
	form brick.RenderOption
}

// toolbarStyle is the toolbar's whole stylesheet. It sits in the corner
// opposite the runtime's toasts (bottom right), collapsed to one small,
// muted pill until opened (only the time on a phone-width
// screen), and follows the browser's light or dark scheme.
const toolbarStyle = `<style>#brick-devbar{--bg:#fff;--fg:#1f2328;--mute:#656d76;--line:#d0d7de;--acc:#0969da;position:fixed;left:8px;bottom:8px;z-index:2147483646;max-width:calc(100vw - 16px);font:12px/1.4 ui-sans-serif,system-ui,sans-serif;color:var(--fg);text-align:left}@media (prefers-color-scheme:dark){#brick-devbar{--bg:#161b22;--fg:#e6edf3;--mute:#8d96a0;--line:#30363d;--acc:#4493f8}}#brick-devbar>summary{list-style:none;cursor:pointer;display:inline-flex;gap:6px;align-items:center;padding:2px 8px;border:1px solid var(--line);border-radius:999px;background:var(--bg);box-shadow:0 1px 3px rgba(0,0,0,.15);font-variant-numeric:tabular-nums;user-select:none}#brick-devbar>summary::-webkit-details-marker{display:none}#brick-devbar:not([open])>summary{color:var(--mute)}#brick-devbar>summary:hover{color:var(--fg)}@media (max-width:480px){#brick-devbar .bd-more{display:none}}#brick-devbar>summary b{color:var(--acc)}#brick-devbar .bd-panel{margin-bottom:4px;width:240px;max-width:100%;padding:8px 10px;border:1px solid var(--line);border-radius:6px;background:var(--bg);box-shadow:0 4px 12px rgba(0,0,0,.2)}#brick-devbar[open]{display:flex;flex-direction:column-reverse;align-items:flex-start}#brick-devbar table{border-collapse:collapse;width:100%;font-variant-numeric:tabular-nums}#brick-devbar td{padding:1px 0;border:0;vertical-align:top}#brick-devbar td+td{text-align:right}#brick-devbar .bd-k{color:var(--mute)}#brick-devbar .bd-h{margin:6px 0 2px;color:var(--mute);font-size:11px;text-transform:uppercase;letter-spacing:.04em}#brick-devbar a{color:var(--acc);text-decoration:none}#brick-devbar a:hover{text-decoration:underline}#brick-devbar .bd-links{display:flex;gap:10px;margin-top:6px;padding-top:6px;border-top:1px solid var(--line)}</style>`

// toolbarLazyAttr marks the table the toolbar's script fills with the lazy
// islands' counts as each answer lands.
const toolbarLazyAttr = "data-brick-devbar-lazy"

// toolbarScript reopens the toolbar if it was left open, and remembers the
// choice. Storage may be unavailable (a private window), which only means
// the toolbar starts closed.
const toolbarScript = `<script>(()=>{const k=` + "`" + toolbarKey + "`" + `,d=document.getElementById(` + "`" + toolbarID + "`" + `);if(!d)return;try{d.open=localStorage.getItem(k)==="1"}catch(e){}d.addEventListener("toggle",()=>{try{localStorage.setItem(k,d.open?"1":"0")}catch(e){}});` + toolbarLazyScript + `})()</script>`

// toolbarLazyScript fills the Lazy islands table. A lazy island renders on
// its own request after this page was sent; in Dev its answer carries its
// counts in a Server-Timing entry (brick.IslandTimingName), which a page
// reads from the resource timing of a fetch it did not make, so the
// runtime needs no hook. The buffered observer also sees islands that
// landed before this script ran. Names are set as text, never as markup.
// Rows go beside the waiting row, not into the table: the browser has put
// them in a <tbody> the markup never wrote.
// serverTiming exists only in a secure context (localhost is one); on a
// plain-http LAN address the table keeps showing what is still waiting.
const toolbarLazyScript = `const t=d.querySelector("[` + toolbarLazyAttr + `]");` +
	`if(t&&"PerformanceObserver"in window){const w=t.querySelector(".bd-wait td+td");let n=+w.textContent;` +
	`const add=e=>{for(const s of e.serverTiming||[]){if(s.name!=="` + brick.IslandTimingName + `")continue;let j;try{j=JSON.parse(atob(s.description))}catch(x){continue}` +
	`const r=document.createElement("tr"),a=document.createElement("a"),c=document.createElement("td"),v=document.createElement("td");` +
	`a.href="/` + playPrefix + `"+encodeURIComponent(j.component);a.title="Open in the playground";a.textContent=j.component;c.append(a);` +
	`v.textContent=j.components+" comp · "+j.loaderCalls+" load · "+s.duration.toFixed(2)+"ms";r.append(c,v);r.dataset.island=j.component;w.parentNode.before(r);` +
	`n=Math.max(0,n-1);w.textContent=n;if(!n)w.parentNode.hidden=true}};` +
	`try{new PerformanceObserver(l=>l.getEntries().forEach(add)).observe({type:"resource",buffered:true})}catch(x){}}`

// toolbarHTML is the Dev toolbar for one response: the render's counts
// (brick.RenderStats) and the time this request took to render or look up
// the page and fill its islands. It is built per response and never
// cached, so no clock reading reaches a cached Result or an exported file.
func toolbarHTML(st brick.RenderStats, hit bool, took time.Duration) string {
	var b strings.Builder
	cache := "miss"
	if hit {
		cache = "hit"
	}
	b.WriteString(toolbarStyle)
	fmt.Fprintf(&b, `<details id="%s" data-brick-devbar>`, toolbarID)
	fmt.Fprintf(&b, `<summary title="brick dev toolbar"><b>brick</b> %s<span class="bd-more"> · %d comp · cache %s</span></summary>`, formatTook(took), st.Components, cache)
	b.WriteString(`<div class="bd-panel"><table>`)
	row := func(k, v string) {
		fmt.Fprintf(&b, `<tr><td class="bd-k">%s</td><td>%s</td></tr>`, k, v)
	}
	row("Render", formatTook(took))
	row("Site cache", cache)
	row("Components", fmt.Sprint(st.Components))
	row("Islands", fmt.Sprintf("%d (%d lazy)", st.Islands, st.LazyIslands))
	row("Loader calls", fmt.Sprint(st.LoaderCalls))
	row("Batch calls", fmt.Sprintf("%d (%d items)", st.BatchCalls, st.BatchItems))
	b.WriteString(`</table>`)
	if top := st.Top(toolbarTop); len(top) > 0 {
		b.WriteString(`<div class="bd-h">Top components</div><table>`)
		for _, t := range top {
			name := html.EscapeString(t.Name)
			fmt.Fprintf(&b, `<tr><td><a href="/%s%s" title="Open in the playground">%s</a></td><td>%d</td></tr>`, playPrefix, html.EscapeString(url.PathEscape(t.Name)), name, t.Count)
		}
		b.WriteString(`</table>`)
	}
	if st.LazyIslands > 0 {
		// Filled by toolbarLazyScript as each island's answer lands.
		fmt.Fprintf(&b, `<div class="bd-h">Lazy islands</div><table %s><tr class="bd-wait"><td class="bd-k">waiting</td><td>%d</td></tr></table>`, toolbarLazyAttr, st.LazyIslands)
	}
	fmt.Fprintf(&b, `<div class="bd-links"><a href="/%s">Instances</a>`, instancesEndpoint)
	if len(st.Types) > 0 {
		fmt.Fprintf(&b, `<a href="/%s%s">Playground</a>`, playPrefix, html.EscapeString(url.PathEscape(st.Types[0].Name)))
	}
	b.WriteString(`</div></div></details>`)
	b.WriteString(toolbarScript)
	return b.String()
}

// formatTook writes a duration the way a developer reads one: microseconds
// under a millisecond, milliseconds with two decimals below a second.
func formatTook(d time.Duration) string {
	switch {
	case d < time.Millisecond:
		return fmt.Sprintf("%dµs", d.Microseconds())
	case d < time.Second:
		return fmt.Sprintf("%.2fms", float64(d)/float64(time.Millisecond))
	default:
		return fmt.Sprintf("%.2fs", d.Seconds())
	}
}

// injectBeforeBody puts html before the last </body>, or at the end of a
// page that has none.
func injectBeforeBody(b []byte, html string) []byte {
	i := bytes.LastIndex(bytes.ToLower(b), []byte(bodyClose))
	if i < 0 {
		return append(b, html...)
	}
	out := append([]byte(nil), b[:i]...)
	out = append(out, html...)
	return append(out, b[i:]...)
}
