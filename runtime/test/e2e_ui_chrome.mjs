// End-to-end check of the kit showcase (examples/ui) in a real, headless
// Chrome over the DevTools protocol: every page loads clean, the interactive
// kit components answer real clicks and keys, and the demo apps round-trip
// their server actions. Clicks and keys are CDP Input events at the
// element's position, typing is Input.insertText, so the page sees what a
// person's browser sends.
//
// Usage: node e2e_ui_chrome.mjs http://127.0.0.1:18300   (task ui:e2e runs it)
//        E2E_BASE=/ui node e2e_ui_chrome.mjs http://127.0.0.1:18300
// With E2E_BASE the showcase is mounted under that path (site BasePath): every
// page the suite opens and every href it expects is prefixed, and any request
// to the server outside the base (other than / and the favicon) fails the
// page it came from.
// Prints one line per check — "ok", "FAIL" or "SKIP" — then "e2e ui ok", and
// exits non-zero when any check failed. A demo app that is not built yet
// (its page is a 404) is a SKIP, not a failure.
import assert from "node:assert/strict";
import { mkdtempSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { openChrome, input, sleep } from "./cdp.mjs";

const origin = (process.argv[2] || "http://127.0.0.1:18300").replace(/\/$/, "");
// B is the mount path ("" unmounted, "/ui" mounted); base is where page paths start.
const B = (process.env.E2E_BASE || "").replace(/\/$/, "");
const base = origin + B;
// at is a site path (as the suite writes it, "/components/x/") as the
// browser sees it, with the mount path in front.
const at = (path) => B + path;

// VIEWPORT is the window the suite drives. E2E_WIDTH (default 1280) runs it
// at another width, e.g. 800 to drive the docs sidebar as AppShell's drawer.
const VIEWPORT = { width: Number(process.env.E2E_WIDTH) || 1280, height: 900 };
// LOAD_CAP_MS bounds the wait for a page's load event; a page that never
// fires one still gets checked (until() then waits for the runtime).
const LOAD_CAP_MS = 10000;
// SLOW_NAV_MS is when E2E_DEBUG reports the requests a navigation waits on.
const SLOW_NAV_MS = 3000;
// SETTLE_MS lets client modules mount after the runtime appears.
const SETTLE_MS = 150;
// PAGE_SETTLE_MS is the extra wait in the page sweep, so late requests
// (lazy assets, a first poll) are seen before the page is judged clean.
const PAGE_SETTLE_MS = 250;
// DRAWER_MS covers a slide-in animation (AppShell's drawer, Drawer) before
// clicking inside it: a click on a still-moving panel misses.
const DRAWER_MS = 400;
// CHANGE_SETTLE_MS follows a committed edit (Tab out of a field, typing
// into the Editor): the change event and the component's write-back (a
// brick:change action, the Editor's textarea sync) start before the next
// step reads or submits.
const CHANGE_SETTLE_MS = 200;
// ACTION_MS waits for a server action that sits behind client code (a
// Sortable drop, a palette navigation): longer than until()'s default.
const ACTION_MS = 8000;
// POLL_REPLY_MS covers the chat's 5 s brick:poll plus the reply's render.
const POLL_REPLY_MS = 12000;

// The back/forward cache is off, so every visit is a fresh document with
// fresh state, as the checks assume.
const cdp = await openChrome({ profilePrefix: "brick-e2e-ui-", args: ["--disable-features=BackForwardCache"] });
const { send, evaluate, until, once } = cdp;
const { click, key, type } = input(cdp);
const js = (s) => JSON.stringify(s);

// What went wrong on the current page: script errors, console.error, and
// failed requests (4xx/5xx or a network failure). Reset on every visit.
let problems = [];
const inflight = new Map();
let docStatus = 0;

// visit loads url as a fresh document and waits for the runtime.
async function visit(path) {
  problems = [];
  docStatus = 0;
  // The old document's requests go with it; Chrome does not always report
  // a long-lived one (an event stream) as failed when it navigates away.
  inflight.clear();
  // Headless Chrome marks the tab hidden after a key it handles itself (an
  // Escape no page listener prevents, as a brick:js .window command
  // leaves it), and the next documents stay hidden: polls pause and view
  // transitions abort. A visitor is looking at the page each check opens.
  await send("Page.bringToFront");
  const t0 = Date.now();
  const loaded = once("Page.loadEventFired");
  await send("Page.navigate", { url: base + path });
  const slow = setTimeout(() => { if (process.env.E2E_DEBUG) console.log("  slow navigation; in flight:", [...inflight.values()].join(" ")); }, SLOW_NAV_MS);
  await Promise.race([loaded, sleep(LOAD_CAP_MS)]);
  clearTimeout(slow);
  if (process.env.E2E_DEBUG) console.log("  loaded", path, Date.now() - t0, "ms");
  if (docStatus >= 400) return docStatus;
  await until("document.readyState === 'complete' && !!window.brick", "the runtime on " + path);
  await sleep(SETTLE_MS);
  return docStatus;
}

// THEME_TOGGLE is the docs top bar's theme button (the showcase's DocsShell
// is a kit AppShell, so its top bar is AppShell's).
const THEME_TOGGLE = ".bk-appshell__topbar .bk-theme-toggle";

// clickSidebar clicks a docs sidebar link; below AppShell's breakpoint the
// sidebar is a popover drawer, so it opens the drawer first.
async function clickSidebar(sel) {
  const hidden = await evaluate(`(() => { const e = document.querySelector(${js(sel)}); return !e || e.getClientRects().length === 0; })()`);
  if (hidden) {
    await click(".bk-appshell__menu");
    await until(`document.querySelector(${js(sel)}).getClientRects().length > 0`, "the sidebar drawer to open");
    await sleep(DRAWER_MS);
  }
  await click(sel);
}

// ---- checks ----------------------------------------------------------------

let failed = 0;
// E2E_ONLY=<regexp> runs only the checks whose name matches (for debugging).
const only = process.env.E2E_ONLY ? new RegExp(process.env.E2E_ONLY) : null;
async function check(name, fn) {
  if (only && !only.test(name)) return;
  try {
    const r = await fn();
    if (r && r.skip) console.log(`SKIP ${name}: ${r.skip}`);
    else console.log(`ok   ${name}`);
  } catch (e) {
    failed++;
    console.log(`FAIL ${name}: ${String(e.message || e).split("\n").slice(0, 3).join(" | ")}`);
    if (problems.length) console.log("     page problems: " + problems.slice(0, 5).join(" | "));
    if (process.env.E2E_DEBUG) console.log(e.stack);
  }
}
// clean fails when the page logged an error or a request failed.
const clean = (what) => { if (problems.length) throw new Error(`${what}: ${problems.length} problem(s): ${problems.slice(0, 4).join(" | ")}`); };
// app visits a demo app page, or returns a SKIP when it is not built yet.
async function app(path) {
  const status = await visit(path);
  if (status === 404) return { skip: "not built yet (404)" };
  if (status >= 400) throw new Error(`${path}: HTTP ${status}`);
  return null;
}

try {
  cdp.on((method, p) => {
    if (method === "Network.requestWillBeSent") {
      inflight.set(p.requestId, p.request.url);
      // Mounted, nothing may reach the server outside the mount path: an
      // unrewritten /_brick/… asset or a root-absolute link would.
      if (B && p.request.url.startsWith(origin)) {
        const path = new URL(p.request.url).pathname;
        if (path !== "/" && path !== "/favicon.ico" && !path.startsWith(B + "/")) problems.push(`outside ${B}/: ${path} (${p.type})`);
      }
    }
    if (method === "Network.loadingFinished" || method === "Network.loadingFailed") inflight.delete(p.requestId);
    switch (method) {
      case "Runtime.exceptionThrown": problems.push("exception: " + (p.exceptionDetails.exception?.description || p.exceptionDetails.text).split("\n")[0]); break;
      case "Runtime.consoleAPICalled": if (p.type === "error") problems.push("console.error: " + p.args.map((a) => a.value ?? a.description).join(" ")); break;
      case "Network.responseReceived":
        if (p.type === "Document" && p.response.url.startsWith(origin)) docStatus = p.response.status;
        else if (p.response.status >= 400) problems.push(`HTTP ${p.response.status} ${p.response.url.replace(origin, "")}`);
        break;
      case "Network.loadingFailed": if (!p.canceled && p.errorText !== "net::ERR_ABORTED" && p.blockedReason !== "inspector") problems.push(`${p.errorText} (${p.type})`); break;
    }
  });
  await send("Runtime.enable");
  await send("Page.enable");
  await send("Network.enable");
  await send("Emulation.setDeviceMetricsOverride", { ...VIEWPORT, deviceScaleFactor: 1, mobile: false });

  // ---- every page ----------------------------------------------------------

  await visit("/");
  const pages = await evaluate(`[...new Set([...document.querySelectorAll(".docs__side a[href^='/']")].map((a) => new URL(a.href).pathname))]`)
    .then((ps) => ps.map((p) => {
      if (B && !p.startsWith(B + "/")) throw new Error(`sidebar link ${p} is outside the mount path ${B}/`);
      return p.slice(B.length);
    }));
  if (pages.length <= 50) throw new Error(`the sidebar lists only ${pages.length} pages; the home page says: ${(await evaluate("document.body.innerText")).slice(0, 300).replace(/\s+/g, " ")}`);
  for (const path of pages) {
    await check(`page ${path} loads clean`, async () => {
      const status = await visit(path);
      if (status === 404 && path.startsWith("/apps/")) return { skip: "not built yet (404)" };
      if (status >= 400) throw new Error(`HTTP ${status}`);
      await sleep(PAGE_SETTLE_MS);
      clean(path);
    });
  }

  // ---- kit components --------------------------------------------------------

  await check("Dialog opens from its trigger, closes on Escape, returns focus", async () => {
    await visit("/components/dialog/");
    const trigger = ".bk-dialog__trigger .bk-btn";
    await click(trigger);
    await until("document.querySelector('dialog.bk-dialog[open]') !== null", "the dialog to open");
    assert.ok(await evaluate("document.querySelector('dialog.bk-dialog[open]').contains(document.activeElement)"), "focus moved into the dialog");
    await key("Escape");
    await until("document.querySelector('dialog.bk-dialog[open]') === null", "Escape to close it");
    assert.ok(await evaluate(`document.activeElement === document.querySelector(${js(trigger)}) || document.querySelector(${js(trigger)}).contains(document.activeElement)`), "focus returned to the trigger");
    clean("dialog");
  });

  await check("v8: intersect loads more rows, keyed rows survive a reorder, .optimistic flips first", async () => {
    await visit("/interactivity/");
    const rows = () => evaluate("document.querySelectorAll('.rf-row').length");
    assert.equal(await rows(), 8, "first page");
    await evaluate("document.getElementById('rf-scroll').scrollTop = 1e6");
    await until("document.querySelectorAll('.rf-row').length >= 16", "the sentinel to load the next page");
    // Keep scrolling: each load appends below, and the re-armed sentinel
    // loads again whenever it is back in view, until the template drops it.
    await until("(document.getElementById('rf-scroll').scrollTop = 1e6, document.getElementById('rf-end') !== null)", "the feed's end", ACTION_MS);
    assert.equal(await rows(), 40);
    const row16 = "[...document.querySelectorAll('.rf-row')].find((r) => r.getAttribute('brick:key') === '16')";
    await evaluate(`window.__row = ${row16}; true`);
    await sleep(DRAWER_MS); // the last load's view transition overlays the page until it ends
    await click("#rf-flip");
    await until("document.querySelector('.rf-row').getAttribute('brick:key') === '1'", "the reorder");
    assert.ok(await evaluate(`window.__row === ${row16}`), "a keyed row keeps its element across the reorder");
    await sleep(DRAWER_MS);
    await click("#rl-like");
    assert.equal(await evaluate("document.getElementById('rl-on').hidden"), false, "liked at once");
    await until("document.getElementById('rl-count').textContent.trim() === '42 likes'", "the server's count");
    clean("interactivity");
  });

  await check("v8: a filter in the URL — read on load, paged, Back restores in place, typed, Clear resets", async () => {
    const count = "document.getElementById('rq-count').textContent";
    await visit("/interactivity/?fruit=e");
    await until(`${count}.startsWith('24 of 35 · page 1 of 4')`, "the URL's filter on load");
    await evaluate("window.__stay = 1; true");
    await click("#rq-next");
    await until("location.search.includes('page=2')", "paging to push page=2");
    assert.ok(await evaluate("location.search.includes('fruit=e')"), "the filter stays in the URL");
    await until(`${count}.includes('page 2 of 4')`, "page 2");
    await evaluate("history.back(); true");
    await until(`${count}.includes('page 1 of 4')`, "Back to restore page 1");
    assert.equal(await evaluate("location.search"), "?fruit=e", "Back restored the URL");
    assert.equal(await evaluate("window.__stay"), 1, "restored in place, without a page load");
    await type("#rq-q", "  Plum ");
    await until(`${count}.startsWith('1 of 35')`, "the typed filter");
    await until("location.search === '?fruit=plum'", "the typed filter in the URL, trimmed by updated:");
    await click("#rq-clear");
    await until("location.search === ''", "Clear to drop the parameters");
    assert.equal(await evaluate("document.getElementById('rq-q').value"), "", "Clear empties the field");
    clean("interactivity");
  });

  await check("v8: a lazy panel paints after the page; Brickbot streams its reply before the action ends", async () => {
    await visit("/interactivity/");
    assert.ok(await evaluate("document.getElementById('rz-skeleton') !== null && document.getElementById('rz-panel') === null"), "the page holds the loading block, not the panel");
    await evaluate("document.getElementById('rz-skeleton').scrollIntoView(); true");
    await until("document.getElementById('rz-total') !== null && document.getElementById('rz-total').textContent.includes('$48,210')", "the lazy panel", ACTION_MS);
    assert.equal(await evaluate("document.querySelector('brick-lazy')"), null, "the placeholder is gone");
    await evaluate("document.getElementById('rs-q').scrollIntoView(); true");
    await type("#rs-q", "lazy islands");
    await click("#rs-ask");
    // Mid-stream: part of the reply shows while the button is still busy.
    await until("document.getElementById('rs-answer').textContent.includes('You asked') && !document.getElementById('rs-answer').textContent.includes('render lands.')", "the first streamed words", ACTION_MS);
    await until("document.getElementById('rs-answer').textContent.trim().endsWith('then the final render lands.')", "the final render", ACTION_MS);
    assert.ok(await evaluate("document.getElementById('rs-answer').textContent.includes('lazy islands')"), "the reply names the question");
    clean("interactivity");
  });

  await check("v20: a streamed panel arrives in the page's own response, after the page has painted", async () => {
    await visit("/interactivity/");
    // visit waits for the load event, which comes after the whole response,
    // streamed panel included: the timings say whether it streamed.
    const t = await evaluate(`(() => { const n = performance.getEntriesByType("navigation")[0]; const p = document.getElementById("rst-total"); return { ttfb: n.responseStart - n.startTime, end: n.responseEnd - n.startTime, panel: p ? p.textContent.trim() : "" }; })()`);
    assert.equal(t.panel, "1,284", "the streamed panel is in place");
    assert.ok(t.end - t.ttfb > 900, `the panel came later in the same response (first byte ${Math.round(t.ttfb)} ms, end ${Math.round(t.end)} ms)`);
    assert.equal(await evaluate("document.querySelectorAll('brick-fill, brick-lazy[data-brick-stream], #rst-skeleton').length"), 0, "no placeholder, fill or loading block left");
    clean("interactivity");
  });

  await check("v17: a note posted here refreshes the live board in another window over server push", async () => {
    await visit("/interactivity/");
    // The other window: the same page in a frame, a separate document with
    // its own runtime, its own EventSource and its own board instance.
    await evaluate(`(() => { const f = document.createElement("iframe"); f.id = "push-peer"; f.style.cssText = "width:800px;height:400px"; f.src = ${js(at("/interactivity/"))}; document.body.append(f); return true; })()`);
    const peer = "document.getElementById('push-peer').contentWindow";
    await until(`${peer}.brick && ${peer}.document.readyState === 'complete' && ${peer}.document.getElementById('rb-count') !== null`, "the second window's board", ACTION_MS);
    // Both documents hold an open stream on the events endpoint: two
    // requests to it are in flight (an EventSource never finishes).
    const streams = () => [...inflight.values()].filter((u) => u.includes("/_brick/events?l=")).length;
    for (const end = Date.now() + ACTION_MS; streams() < 2 && Date.now() < end; ) await sleep(50);
    assert.equal(streams(), 2, "one event stream per window");
    const count = (doc) => evaluate(`Number(${doc}.getElementById('rb-count').textContent.trim().split(' ')[0])`);
    const before = await count(peer + ".document");
    const note = "pushed " + Date.now();
    await evaluate("document.getElementById('rp-body').scrollIntoView(); true");
    await type("#rp-body", note);
    await click("#rp-post");
    // The other window refreshes with no interaction of its own; so does
    // this one, which listens to the same topic.
    await until(`${peer}.document.getElementById('rb-notes').textContent.includes(${js(note)})`, "the note on the other window's board", ACTION_MS);
    assert.equal(await count(peer + ".document"), before + 1, "the other window's count");
    await until(`document.getElementById('rb-notes').textContent.includes(${js(note)})`, "the note on this window's board", ACTION_MS);
    clean("interactivity");
  });

  await check("v18: client commands open and close a menu with no request; an exit transition plays before hide", async () => {
    await visit("/interactivity/");
    const actions = () => evaluate("performance.getEntriesByType('resource').filter((e) => e.name.includes('/_brick/action')).length");
    const before = await actions();
    await evaluate("document.getElementById('v18-open').scrollIntoView({ block: 'center' }); true");
    await click("#v18-open");
    await until("!document.getElementById('v18-menu').hidden", "toggle to open the menu");
    assert.equal(await evaluate("document.getElementById('v18-open').getAttribute('aria-expanded')"), "true", "toggleAttr");
    await click("#v18-pin");
    assert.ok(await evaluate("document.getElementById('v18-menu').classList.contains('v18-panel--pinned')"), "toggleClass");
    // The menu plays brick:leave (opacity .25s) before it is hidden: the
    // page records, as the leave starts, which CSS transitions the classes
    // started (a CDP round trip could land after a 250 ms one has ended).
    await evaluate(`(() => { const m = document.getElementById("v18-menu"); new MutationObserver((rs, o) => { if (!m.hasAttribute("data-brick-leaving")) return; o.disconnect(); getComputedStyle(m).opacity; window.__anims = m.getAnimations().map((a) => a.transitionProperty); }).observe(m, { attributes: true }); return true; })()`);
    await key("Escape");
    await until("window.__anims !== undefined", "the exit transition to start");
    assert.ok((await evaluate("window.__anims")).includes("opacity"), "the leave classes started an opacity transition");
    await until("document.getElementById('v18-menu').hidden && !document.getElementById('v18-menu').classList.contains('v18-fade')", "hidden, leave classes off");
    assert.equal(await evaluate("document.getElementById('v18-open').getAttribute('aria-expanded')"), "false", ".window Escape");
    await click("#v18-open");
    await until("!document.getElementById('v18-menu').hidden", "reopen");
    await click("#v18-dismiss"); // outside the menu's wrapper
    await until("document.getElementById('v18-menu').hidden", ".outside to close the menu");
    await until("document.getElementById('v18-card').hidden", "the notice dismissed via closest");
    await click("#v18-back");
    await until("!document.getElementById('v18-card').hidden", "show brings it back");
    assert.equal(await actions(), before, "no request was sent");
    clean("interactivity");
  });

  await check("v18: a deleted row fades out before it is removed; rename asks, cancel sends nothing; commands stay across a morph", async () => {
    await visit("/interactivity/");
    const actions = () => evaluate("performance.getEntriesByType('resource').filter((e) => e.name.includes('/_brick/action')).length");
    const names = () => evaluate("[...document.querySelectorAll('#v18t-list li')].map((l) => l.textContent.replace(/RenameDelete$/, '').trim())");
    await evaluate("document.getElementById('v18t-add').scrollIntoView({ block: 'center' }); true");
    await click("#v18t-help");
    await until("!document.querySelector('[brick\\\\:ref=help]').hidden", "the help note");
    await click("#v18t-add");
    await until("document.querySelectorAll('#v18t-list li').length === 4", "a task added");
    assert.equal(await evaluate("document.querySelector('[brick\\\\:ref=help]').hidden"), false, "sticky: the note stays open across the morph");
    await evaluate("brick.promptWith((m) => { window.__asked = m; return 'Renamed task'; }); true");
    await click("#v18t-list li:first-child .v18t-rename");
    await until("document.querySelector('#v18t-list li').textContent.includes('Renamed task')", "the rename");
    assert.ok((await evaluate("window.__asked")).startsWith("New name for"), "the prompt message");
    const n = await actions();
    await evaluate("brick.promptWith(() => null); true");
    await click("#v18t-list li:first-child .v18t-rename");
    await sleep(400);
    assert.equal(await actions(), n, "cancel sends nothing");
    const second = "document.querySelector('#v18t-list li[brick\\\\:key=\"2\"]')";
    await evaluate(`window.__row = ${second}; true`);
    await click("#v18t-list li:nth-child(2) .v18t-del");
    await until("window.__row.hasAttribute('data-brick-leaving')", "the row to start leaving");
    assert.ok(await evaluate("window.__row.isConnected"), "still in the page while it fades");
    await until("!window.__row.isConnected", "removed after the transition");
    assert.deepEqual(await names(), ["Renamed task", "Announce it", "Task 4"]);
    clean("interactivity");
  });

  await check("v9: row streams insert escaped rows at the top, trim to the limit, survive a re-render and reset", async () => {
    await visit("/runtime-extras/");
    const rows = () => evaluate("document.querySelectorAll('#rr-feed .rr-row').length");
    const top = () => evaluate("document.querySelector('#rr-feed .rr-row .rr-text').textContent");
    assert.equal(await rows(), 3, "the seeded rows");
    await type("#rr-text", "<b>hello</b>");
    await click("#rr-post");
    await until("document.querySelectorAll('#rr-feed .rr-row').length === 4", "the posted row");
    assert.equal(await top(), "<b>hello</b>", "the typed text lands at the top, as text");
    assert.equal(await evaluate("document.querySelector('#rr-feed b')"), null, "no element was made from the typed text");
    assert.equal(await evaluate("document.querySelector('#rr-feed .rr-row').getAttribute('brick:key')"), "4", "the row carries its key");
    await click("#rr-burst");
    await until("document.querySelector('#rr-feed .rr-row .rr-text').textContent === 'Burst message 7'", "the burst at the top");
    assert.equal(await rows(), 5, "limit trims the oldest rows");
    assert.equal(await evaluate("[...document.querySelectorAll('#rr-feed .rr-row')].map((r) => r.getAttribute('brick:key')).join()"), "7,6,5,4,3", "newest first");
    assert.ok((await evaluate("document.getElementById('rr-carried').textContent")).includes("3 new row"), "the answering render saw the temporary rows");
    await click("#rr-refresh");
    await until("document.getElementById('rr-carried').textContent.includes('0 new row')", "temporary state back at its declared value after a re-render");
    assert.equal(await rows(), 5, "brick:ignore keeps the streamed rows through the morph");
    await click("#rr-clear");
    await until("document.querySelectorAll('#rr-feed .rr-row').length === 0", "the reset");
    clean("row streams");
  });

  await check("v9: brick.submit refuses an empty form, then hands a valid one to a classic POST", async () => {
    await visit("/runtime-extras/");
    await evaluate("document.getElementById('rsub-form').scrollIntoView(); true");
    await click("#rsub-go");
    await until("document.getElementById('rsub-error') !== null", "the action's own error");
    assert.equal(await evaluate("location.pathname + location.search"), at("/runtime-extras/"), "an invalid form stays on the page");
    await type("#rsub-name", "Ada");
    const left = once("Page.loadEventFired");
    await click("#rsub-go");
    await Promise.race([left, sleep(LOAD_CAP_MS)]);
    await until("location.search === '?signed=Ada' && document.getElementById('rsub-signed') !== null", "the 303 back from /demo/sign-in", ACTION_MS);
    assert.ok((await evaluate("document.getElementById('rsub-signed').textContent")).includes("Ada"), "the page names who signed in");
    const cookies = await send("Network.getCookies", { urls: [origin + "/"] });
    assert.ok(cookies.cookies.some((c) => c.name === "brick_demo_user" && c.value === "Ada" && c.httpOnly), "the classic POST set its cookie");
    clean("submit hand-off");
  });

  await check("v9: an action that fails with a 500 is recovered by brick.reloadComponent", async () => {
    await visit("/runtime-extras/");
    await evaluate("document.getElementById('rx-add').scrollIntoView(); true");
    await click("#rx-add");
    await click("#rx-add");
    await until("document.getElementById('rx-count').textContent === '2'", "two bumps");
    await click("#rx-crash");
    await until("document.getElementById('rx-error').hidden === false", "the action-error notice");
    // The 500 (and the runtime's report of it) are the demo; anything else is not.
    problems = problems.filter((p) => !/^HTTP 500 \/_brick\/action$|the server failed to run crash/.test(p));
    await click("#rx-reload");
    await until("document.getElementById('rx-error').hidden === true && document.getElementById('rx-count').textContent === '0'", "a fresh render of the component");
    await click("#rx-add");
    await until("document.getElementById('rx-count').textContent === '1'", "actions work after the reload");
    clean("reload component");
  });

  await check("v9: load_many answers five cards with one call; declared slots render; tag writes a heading level from state", async () => {
    await visit("/runtime-extras/");
    assert.equal(await evaluate("document.querySelectorAll('#ru-team .ru-card').length"), 5, "five cards");
    assert.equal(await evaluate("[...document.querySelectorAll('#ru-team .ru-card')].map((c) => c.dataset.batch).join()"), "4,4,4,4,4", "every card came from the one call with four distinct inputs");
    assert.equal(await evaluate("document.querySelector('#ru-team .ru-card b').textContent"), "Ada Lovelace", "the loader's data, in input order");
    assert.equal(await evaluate("document.getElementById('rp-title').textContent.trim()"), "Quarterly report", "the title block");
    assert.ok(await evaluate("document.getElementById('rp-actions').textContent.includes('Export') && document.getElementById('rp-body').textContent.includes('Revenue grew')"), "the actions block and the body");
    assert.equal(await evaluate("document.getElementById('rh-heading').tagName"), "H2", "the declared level");
    await evaluate("document.getElementById('rh-4').scrollIntoView(); true");
    await click("#rh-4");
    await until("document.getElementById('rh-heading').tagName === 'H4'", "the heading re-rendered as h4");
    assert.equal(await evaluate("document.getElementById('rh-heading').textContent"), "A heading at level 4");
    clean("loaders, slots and tag");
  });

  await check("v22: a modelable stepper moves its parent's total and takes the parent's reset; an action picks or skips its transition; click details reach the action", async () => {
    await visit("/runtime-extras/");
    const total = "document.getElementById('v22-total').textContent";
    await evaluate("document.getElementById('v22-inc').scrollIntoView({block: 'center'}); true");
    await click("#v22-inc");
    await until(`${total}.includes('$36')`, "the cart's total to follow the stepper");
    await click("#v22-reset");
    await until(`${total}.includes('$12') && document.getElementById('v22-value').textContent === '1'`, "the cart's reset to reach the stepper");
    await evaluate("window.__vt = []; window.__vtIdle = true; const real = document.startViewTransition.bind(document); document.startViewTransition = (a) => { window.__vt.push(typeof a === 'function' ? null : a.types); window.__vtIdle = false; const t = real(a); t.finished.finally(() => { window.__vtIdle = true; }); return t; }; document.getElementById('v22-shuffle').scrollIntoView({block: 'center'}); true");
    await click("#v22-shuffle");
    await until("document.getElementById('v22-note').textContent.includes('slide')", "the rotate action");
    // While a view transition runs, its pseudo-elements cover the page, so
    // a click lands on <html>: wait until the slide has finished.
    await until("window.__vtIdle", "the slide transition to finish");
    await click("#v22-edit");
    await until("document.getElementById('v22-note').textContent.includes('without')", "the note action");
    assert.equal(await evaluate("JSON.stringify(window.__vt)"), '[["slide"]]', "one slide transition, none for the note");
    await evaluate("document.getElementById('v22-pad').scrollIntoView({block: 'center'}); true");
    await click("#v22-pad");
    await until("document.getElementById('v22-details').textContent.includes('count=1')", "the click's details");
    assert.match(await evaluate("document.getElementById('v22-details').textContent"), /shift=false alt=false/);
    clean("v22 runtime");
  });

  await check("Deferred form: typing sends nothing, Save sends the held fields", async () => {
    await visit("/components/deferred-form/");
    const server = "document.getElementById('deferred-server').textContent";
    await type("input[placeholder='Ada Lovelace']", "Ada");
    await until("document.getElementById('deferred-dirty').hidden === false", "the unsaved-changes notice");
    assert.ok(!(await evaluate(`${server}.includes('Ada')`)), "a deferred field is not sent while typing");
    await click("#deferred-save");
    await until(`${server}.includes('Ada')`, "the save to reach the server");
    clean("deferred form");
  });

  await check("Validated form: an error shows after leaving a field, the submit shows every error", async () => {
    await visit("/components/validated-form/");
    const text = "document.querySelector('form[brick\\\\:submit=signup]').innerText";
    assert.ok(!(await evaluate(`${text}.includes('required')`)), "an untouched form shows no error");
    await type("#vs-email", "nope");
    await key("Tab");
    await until(`${text}.includes('Enter a valid email address.')`, "the e-mail error after blur");
    assert.ok(!(await evaluate(`${text}.includes('This field is required.')`)), "untouched fields stay quiet");
    await click("#vs-submit");
    await until(`${text}.split('This field is required.').length === 4 && ${text}.includes('Enter a valid email address.')`, "every error after submit");
    clean("validated form");
  });

  await check("Upload: a picked picture streams through the disk sink and its server URL becomes the preview", async () => {
    await visit("/components/upload/");
    // A real PNG (1x1), written for Chrome's file chooser to read.
    const png = join(mkdtempSync(join(tmpdir(), "brick-e2e-upload-")), "dot.png");
    writeFileSync(png, Buffer.from("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==", "base64"));
    const { root } = await send("DOM.getDocument", {});
    const { nodeId } = await send("DOM.querySelector", { nodeId: root.nodeId, selector: "#au-input" });
    await send("DOM.setFileInputFiles", { nodeId, files: [png] });
    await until("document.querySelector('#au-preview') !== null", "the preview after the upload");
    const src = await evaluate("document.querySelector('#au-preview').getAttribute('src')");
    assert.match(src, new RegExp("^" + B + "/uploads/[0-9a-f]{32}$"), "the preview is the disk sink's URL");
    await until("document.querySelector('#au-preview').complete && document.querySelector('#au-preview').naturalWidth === 1", "the preview image to load");
    assert.equal(await evaluate("document.querySelector('#au-name').textContent"), "dot.png");
    assert.equal(await evaluate("document.querySelector('#au-progress').value"), await evaluate("document.querySelector('#au-progress').max"), "the progress bar is full");
    await click("#au-remove");
    await until("document.querySelector('#au-empty') !== null", "the removed picture");
    clean("upload");
  });

  await check("Upload without JavaScript: the file goes with the form post, lands in the sink and shows after the 303", async () => {
    // Scripts are off for this check only, so the runtime never runs and
    // the browser posts the form itself. Runtime.evaluate is off with them,
    // so the page is read and driven through the DOM domain.
    const png = join(mkdtempSync(join(tmpdir(), "brick-e2e-nojs-")), "dot.png");
    writeFileSync(png, Buffer.from("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==", "base64"));
    const node = async (sel) => {
      const { root } = await send("DOM.getDocument", { depth: -1 });
      return (await send("DOM.querySelector", { nodeId: root.nodeId, selector: sel })).nodeId;
    };
    const text = async (sel) => {
      const id = await node(sel);
      return id ? (await send("DOM.getOuterHTML", { nodeId: id })).outerHTML.replace(/<[^>]+>/g, "").trim() : null;
    };
    const load = async (fn) => {
      const loaded = once("Page.loadEventFired");
      await fn();
      await Promise.race([loaded, sleep(LOAD_CAP_MS)]);
    };
    await send("Emulation.setScriptExecutionDisabled", { value: true });
    try {
      problems = [];
      await load(() => send("Page.navigate", { url: base + "/components/upload/" }));
      await send("DOM.scrollIntoViewIfNeeded", { nodeId: await node("#nju-save") });
      await send("DOM.focus", { nodeId: await node("#nju-name") });
      await send("Input.insertText", { text: "Ada" });
      await send("DOM.setFileInputFiles", { nodeId: await node("#nju-input"), files: [png] });
      const { model } = await send("DOM.getBoxModel", { nodeId: await node("#nju-save") });
      const [x1, y1, , , x3, y3] = model.content;
      const x = (x1 + x3) / 2, y = (y1 + y3) / 2;
      await load(async () => {
        await send("Input.dispatchMouseEvent", { type: "mousePressed", x, y, button: "left", clickCount: 1 });
        await send("Input.dispatchMouseEvent", { type: "mouseReleased", x, y, button: "left", clickCount: 1 });
      });
      assert.equal(docStatus, 200, "the page the 303 leads to");
      assert.match(await text("#nju-file") || "", /^dot\.png · image\/png$/, "the stored file");
      assert.match(await text("#nju-saved") || "", /Saved 1× as Ada/, "the action ran after the upload");
      const img = await send("DOM.getAttributes", { nodeId: await node("#nju-preview") });
      assert.match(img.attributes[img.attributes.indexOf("src") + 1], new RegExp("^" + B + "/uploads/[0-9a-f]{32}$"), "the preview is the disk sink's URL");
      clean("no-JS upload");
    } finally {
      await send("Emulation.setScriptExecutionDisabled", { value: false });
    }
  });

  await check("brick:confirm asks with the kit ConfirmDialog on its page; Escape cancels, confirm runs the action", async () => {
    await visit("/components/confirm-dialog/");
    const asking = "document.querySelector('dialog.bk-confirm[open]')";
    // One delete dialog per contact row (its title and message ids share the prefix).
    const rows = "document.querySelectorAll('dialog.bk-confirm[id^=demo-del-]').length";
    await click("#demo-del-all");
    await until(`${asking} !== null`, "the kit confirm dialog");
    assert.equal(await evaluate(`${asking}.querySelector('h2').textContent`), "Delete every contact?");
    assert.equal(await evaluate(`${asking}.querySelector('button[value=confirm]').textContent`), "Delete all");
    assert.ok(await evaluate(`${asking}.contains(document.activeElement) && document.activeElement.value === 'cancel'`), "Cancel is focused first");
    await key("Escape");
    await until(`${asking} === null`, "Escape to close it");
    await sleep(CHANGE_SETTLE_MS);
    assert.equal(await evaluate(rows), 3, "Escape sent nothing");
    await click("#demo-del-all");
    await until(`${asking} !== null`, "the kit confirm dialog again");
    await click("dialog.bk-confirm[open] button[value=confirm]");
    await until(`${rows} === 0`, "the confirmed delete");
    clean("confirm");
  });

  await check("DropdownMenu: keyboard opens, arrows move, Escape closes", async () => {
    await visit("/components/dropdown-menu/");
    const trig = ".bk-dropdown__trigger";
    await click(trig);
    await until("document.querySelector('.bk-dropdown[open]') !== null", "the menu to open on click");
    await key("Escape");
    await until("document.querySelector('.bk-dropdown[open]') === null", "Escape to close it");
    await evaluate(`document.querySelector(${js(trig)}).focus()`);
    await key("ArrowDown");
    await until("document.querySelector('.bk-dropdown[open]') !== null && document.activeElement.getAttribute('role') === 'menuitem'", "ArrowDown to open on the first item");
    const first = await evaluate("document.activeElement.textContent.trim()");
    await key("ArrowDown");
    const second = await evaluate("document.activeElement.textContent.trim()");
    assert.notEqual(first, second, "ArrowDown moves");
    await key("End");
    assert.match(await evaluate("document.activeElement.textContent"), /Sign out/, "End jumps to the last item");
    await key("Escape");
    await until(`document.querySelector('.bk-dropdown[open]') === null && document.activeElement === document.querySelector(${js(trig)})`, "Escape to return focus");
    clean("dropdown");
  });

  await check("CommandPalette: ⌘K opens, typing filters, Enter navigates", async () => {
    await visit("/components/button/");
    await key("k", 4);
    await until("document.querySelector('dialog.bk-cmdk__dialog[open]') !== null", "⌘K to open the palette");
    await send("Input.insertText", { text: "data tab" });
    const opts = `[...document.querySelectorAll('dialog.bk-cmdk__dialog[open] [role=option]')].filter((o) => !o.hidden && o.offsetParent)`;
    await until(`${opts}.length >= 1 && ${opts}.length < 10`, "the list to filter");
    // Ranking puts the label match first, so Enter alone opens DataTable.
    assert.match(await evaluate(`(document.querySelector('dialog.bk-cmdk__dialog[open] [role=option][aria-selected=true]') || {}).textContent || ''`), /DataTable/, "DataTable is the first, selected option");
    await key("Enter");
    await until(`location.pathname === ${js(at("/components/data-table/"))}`, "Enter to navigate to DataTable", ACTION_MS);
    clean("palette");
  });

  await check("CommandPalette: the \"Toggle dark mode\" command flips the theme", async () => {
    await visit("/components/badge/");
    const theme = `document.documentElement.dataset.theme || (matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light')`;
    const before = await evaluate(theme);
    await key("k", 4);
    await until("document.querySelector('dialog.bk-cmdk__dialog[open]') !== null", "⌘K to open the palette");
    await send("Input.insertText", { text: "dark" });
    await until(`/dark mode/i.test((document.querySelector('dialog.bk-cmdk__dialog[open] [role=option][aria-selected=true]') || {}).textContent || '')`, "the dark-mode command to be selected");
    await key("Enter");
    await until(`document.querySelector('dialog.bk-cmdk__dialog[open]') === null && (${theme}) !== ${js(before)}`, "<html data-theme> to flip");
    await key("k", 4); // back, for the checks that follow
    await send("Input.insertText", { text: "dark" });
    await until(`/dark mode/i.test((document.querySelector('dialog.bk-cmdk__dialog[open] [role=option][aria-selected=true]') || {}).textContent || '')`, "the command again");
    await key("Enter");
    await until(`(${theme}) === ${js(before)}`, "the theme to flip back");
    clean("palette command");
  });

  await check("Tabs: click and arrow keys select, roving tabindex", async () => {
    await visit("/components/tabs/");
    const tabs = ".bk-tabs [role=tablist]";
    await click(`${tabs} [role=tab]:nth-child(2)`);
    assert.equal(await evaluate(`document.querySelector('${tabs} [role=tab]:nth-child(2)').getAttribute('aria-selected')`), "true");
    await key("ArrowRight");
    assert.equal(await evaluate(`document.activeElement.textContent.trim()`), "Docker", "ArrowRight moves focus and selection");
    assert.equal(await evaluate(`document.querySelector('${tabs} [role=tab]:nth-child(3)').getAttribute('aria-selected')`), "true");
    await key("ArrowRight");
    assert.equal(await evaluate(`document.activeElement.textContent.trim()`), "Go", "ArrowRight wraps");
    const shown = await evaluate(`[...document.querySelector('.bk-tabs').querySelectorAll(':scope > .bk-tabs__panels > [role=tabpanel], [role=tabpanel]')].slice(0, 3).map((p) => !p.hidden)`);
    assert.deepEqual(shown, [true, false, false], "only the selected panel shows");
    clean("tabs");
  });

  await check("Combobox: typing filters, arrow + Enter picks, the select follows", async () => {
    await visit("/components/combobox/");
    const input = ".bk-combobox .bk-combobox__input";
    await type(input, "ger");
    await until(`document.querySelector(${js(input)}).getAttribute('aria-expanded') === 'true'`, "the listbox to open");
    const opts = await evaluate(`[...document.querySelectorAll('.bk-combobox [role=option]')].filter((o) => !o.hidden && o.offsetParent).map((o) => o.textContent.trim())`);
    assert.deepEqual(opts, ["Germany"], "filtered to Germany");
    await key("ArrowDown");
    await key("Enter");
    await until(`document.querySelector('.bk-combobox select').value === 'Germany'`, "the native select to take Germany");
    assert.equal(await evaluate(`document.querySelector(${js(input)}).value`), "Germany");
    clean("combobox");
  });

  await check("DataTable: sort, filter and paginate 64 contacts", async () => {
    await visit("/components/data-table/");
    // The page has two tables; scope every selector to the first.
    await evaluate(`document.querySelector('.bk-dt').id = 'e2e-dt'`);
    const dt = "#e2e-dt";
    await until(`!document.querySelector('${dt} .bk-dt__footer').hidden`, "the enhanced table");
    const range = () => evaluate(`document.querySelector('${dt} .bk-dt__range').textContent`);
    assert.equal(await range(), "1–10 of 64");
    await click(`${dt} .bk-dt__page[data-page="2"]:not([aria-label])`);
    assert.equal(await range(), "11–20 of 64");
    await click(`${dt} thead th[data-col="0"] .bk-dt__sort`);
    assert.equal(await evaluate(`document.querySelector('${dt} thead th[data-col="0"]').getAttribute('aria-sort')`), "ascending");
    const names = await evaluate(`[...document.querySelectorAll('${dt} tbody tr:not([hidden]) td[data-col="0"]')].slice(0, 3).map((c) => c.dataset.value)`);
    assert.deepEqual([...names].sort(), names, "sorted by name");
    await type(`${dt} .bk-dt__search input`, "hooli");
    await until(`/of [1-9]$/.test(document.querySelector('${dt} .bk-dt__range').textContent) && !document.querySelector('${dt} .bk-dt__range').textContent.includes('64')`, "the filter to narrow the rows");
    assert.ok(await evaluate(`[...document.querySelectorAll('${dt} tbody tr:not([hidden]):not(.bk-dt__none)')].every((r) => r.textContent.toLowerCase().includes('hooli'))`), "every row matches");
    await click(`${dt} thead .bk-dt__all`);
    assert.match(await evaluate(`document.querySelector('${dt} .bk-dt__selected').textContent`), /^\d+ selected$/);
    clean("datatable");
  });

  await check("Toaster shows a toast rendered by a server action", async () => {
    await visit("/components/toast/");
    const btn = `[data-brick-state] .bk-btn`;
    await click(btn);
    await until(`[...document.querySelectorAll('.bk-toaster .bk-toast, [class*="toaster"] .bk-toast')].some((t) => t.textContent.includes('Changes saved'))`, "the toast in the toaster");
    clean("toast");
  });

  await check("ThemeToggle: dark mode persists across navigation", async () => {
    await visit("/components/badge/");
    const before = await evaluate(`document.documentElement.dataset.theme || (matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light')`);
    await click(THEME_TOGGLE);
    const after = await evaluate("document.documentElement.dataset.theme");
    assert.notEqual(after, before, "the toggle switched the theme");
    await clickSidebar(`.docs__side a[href='${at("/components/avatar/")}']`);
    await until(`location.pathname === ${js(at("/components/avatar/"))}`, "sidebar navigation");
    assert.equal(await evaluate("document.documentElement.dataset.theme"), after, "kept after a brick navigation");
    await visit("/components/table/");
    assert.equal(await evaluate("document.documentElement.dataset.theme"), after, "kept after a full load");
    await click(THEME_TOGGLE); // back, for the checks that follow
    clean("theme");
  });

  // ---- apps ------------------------------------------------------------------

  await check("Store: add to cart updates the count and the drawer", async () => {
    const s = await app("/apps/store/");
    if (s) return s;
    const count = () => evaluate(`(document.querySelector('.store__head .bk-indicator__badge') || {}).textContent || '0'`);
    const n0 = Number(await count());
    await click("#product-3 .bk-btn--primary");
    await until(`Number((document.querySelector('.store__head .bk-indicator__badge') || {}).textContent || 0) === ${n0 + 1}`, "the cart count to go up");
    await until(`[...document.querySelectorAll('.bk-toast')].some((t) => t.textContent.includes('Added to cart'))`, "the add-to-cart toast");
    await click("[data-bk-open='store-cart']");
    await until("document.getElementById('store-cart').open", "the cart drawer");
    await sleep(DRAWER_MS);
    assert.ok(await evaluate(`document.querySelector('#store-cart').textContent.includes('Ceramic mug')`), "the mug is in the drawer");
    await click("#store-cart .store-line__qty .bk-btn[aria-label^='One more']");
    await until(`document.querySelector('#store-cart .store-line__qty span').textContent === '2' || [...document.querySelectorAll('#store-cart .store-line__qty span')].some((s) => Number(s.textContent) >= 2)`, "the quantity to change");
    assert.ok(await evaluate("document.getElementById('store-cart').open"), "the drawer stayed open");
    await key("Escape");
    clean("store");
  });

  await check("Inbox: star and archive a message", async () => {
    const s = await app("/apps/inbox/");
    if (s) return s;
    const count = () => evaluate(`document.querySelector('.inbox__list-title small').textContent`);
    const c0 = await count();
    const starred = () => evaluate(`document.querySelector('.inbox-row:first-child .inbox-row__star').classList.contains('inbox-star--on')`);
    const was = await starred();
    await click(".inbox-row:first-child .inbox-row__star");
    await until(`document.querySelector('.inbox-row:first-child .inbox-row__star').classList.contains('inbox-star--on') === ${!was}`, "the star to toggle");
    await click(".inbox-row:first-child .inbox-row__open");
    await until("document.querySelector('.inbox__subject') !== null", "the message to open");
    await click(".inbox__toolbar .bk-btn[aria-label='Archive']");
    await until(`document.querySelector('.inbox__list-title small').textContent !== ${js(c0)}`, "the inbox count to drop");
    await until(`[...document.querySelectorAll('.bk-toast')].some((t) => t.textContent.includes('archived'))`, "the archived toast");
    clean("inbox");
  });

  await check("Settings: profile validation error, then a success toast", async () => {
    const s = await app("/apps/settings/");
    if (s) return s;
    const form = "form.settings-form";
    await type(`${form} input[name=name]`, "A");
    await click(`${form} button[type=submit]`);
    await until(`document.querySelector('${form} .bk-field__error') !== null`, "the name error");
    assert.match(await evaluate(`document.querySelector('${form} .bk-field__error').textContent`), /at least two letters/);
    await type(`${form} input[name=name]`, "Ada Lovelace");
    await click(`${form} button[type=submit]`);
    await until(`document.querySelector('${form} .bk-field__error') === null && [...document.querySelectorAll('.bk-toast')].some((t) => /saved/i.test(t.textContent))`, "the success toast");
    clean("settings");
  });

  await check("Auth: sign in with the demo account", async () => {
    const s = await app("/apps/auth/");
    if (s) return s;
    const form = "form[brick\\:submit]";
    await type(`${form} input[name=email]`, "demo@brick.dev");
    await type(`${form} input[name=password]`, "wrong-password");
    await click(`${form} button[type=submit]`);
    await until(`/don't match/.test(document.body.textContent)`, "the wrong-password error");
    await type(`${form} input[name=password]`, "brickyard");
    await click(`${form} button[type=submit]`);
    await until(`/Welcome|Signed in|signed in/i.test(document.querySelector('[data-brick-state]').textContent)`, "the signed-in state");
    clean("auth");
  });

  await check("Landing: the pricing toggle switches monthly and yearly prices", async () => {
    const s = await app("/apps/landing/");
    if (s) return s;
    const shown = () => evaluate(`[...document.querySelectorAll('.land-plan__price b')].map((b) => [...b.children].find((c) => c.offsetParent !== null)?.textContent)`);
    const monthly = await shown();
    await click(`.land-toggle label:last-of-type, .land-toggle input[value=Yearly] + *`);
    await until(`document.querySelector('.land-toggle input[value=Yearly]').checked`, "Yearly to be chosen");
    const yearly = await shown();
    assert.notDeepEqual(yearly, monthly, "the prices changed");
    clean("landing");
  });

  // toastSays waits for a toast whose text matches re.
  const toastSays = (re, what) => until(`[...document.querySelectorAll('.bk-toast')].some((t) => ${re}.test(t.textContent))`, what);

  await check("Contacts: search, create with a validation error then success, delete from the row menu; the topbar count follows", async () => {
    const s = await app("/apps/contacts/");
    if (s) return s;
    // Every id in the app is built from its instance id ($.ID), so read it
    // from the root rather than hard-coding one.
    const ap = await evaluate(`document.querySelector('[data-brick-name="ContactsApp"]').dataset.brick`);
    const form = `${ap}-form`;
    const rows = `[...document.querySelectorAll('tbody tr[id^="${ap}-row-"]')]`;
    // ContactCount listens to contact-added / contact-deleted.
    const count = `Number((document.querySelector('[data-brick-name="ContactCount"] b') || {}).textContent)`;
    const before = await evaluate(count);
    assert.ok(before > 0, "the topbar shows a contact count");
    await type(".ct-search input", "haddad");
    await until(`${rows}.length > 0 && ${rows}.every((r) => /haddad/i.test(r.textContent))`, "the search to narrow the rows");
    await type(".ct-search input", "zz");
    await until(`${rows}.length === 0`, "no match");
    await click("[brick\\:click='contacts.clear_filters']");
    await until(`${rows}.length > 5`, "the filters to clear");
    await click("[brick\\:click='contacts.new_contact']");
    await until(`document.getElementById('${form}').open`, "the new-contact dialog");
    await type(`#${form} input[name=name]`, "G");
    await type(`#${form} input[name=email]`, "not-an-email");
    await click(`#${form} button[form=${form}-fields]`);
    await until(`document.querySelectorAll('#${form} .bk-field__error').length >= 2 && document.getElementById('${form}').open`, "the errors in the open dialog");
    await type(`#${form} input[name=name]`, "Grace Hopper");
    await type(`#${form} input[name=email]`, "grace.e2e@example.com");
    await click(`#${form} button[form=${form}-fields]`);
    await until(`!document.getElementById('${form}').open`, "the dialog to close");
    await toastSays("/Added Grace Hopper/", "the added toast (a brick.flash shown by the kit Toaster)");
    await until(`${count} === ${before + 1}`, "the topbar count to go up by one (contact-added)");
    const id = await evaluate(`(${rows}.find((r) => r.textContent.includes('Grace Hopper')) || {id: ''}).id`);
    assert.ok(id, "the new contact is listed");
    await click(`#${id} .bk-dropdown__trigger`);
    await click(`#${id} .bk-menu-item[data-bk-open]`);
    const del = id.replace("-row-", "-del-");
    await until(`document.getElementById(${js(del)}).open`, "the delete confirmation");
    await click(`#${del} .bk-btn--danger`);
    await until(`!document.getElementById(${js(id)})`, "the row to go");
    await toastSays("/Deleted Grace Hopper/", "the delete toast");
    await until(`${count} === ${before}`, "the topbar count to go back down (contact-deleted)");
    assert.equal(await evaluate("!!document.getElementById('brick-flash')"), false, "the runtime's default flash display stays off while the Toaster listens");
    if (process.env.E2E_SHOT) {
      const { data } = await send("Page.captureScreenshot", { format: "png" });
      (await import("node:fs")).writeFileSync(process.env.E2E_SHOT, Buffer.from(data, "base64"));
    }
    clean("contacts");
  });

  await check("Invoices: new invoice, add a line, totals follow", async () => {
    const s = await app("/apps/invoices/");
    if (s) return s;
    await click("[brick\\:click='newInvoice']");
    await until("document.querySelector('input[name=desc_0]') !== null", "the editor");
    const total = (dt) => evaluate(`[...document.querySelectorAll('.inv-sum div')].find((d) => d.querySelector('dt').textContent.startsWith(${js(dt)})).querySelector('dd').textContent`);
    const set = async (name, v) => { await type(`input[name=${name}]`, v); await key("Tab"); await sleep(CHANGE_SETTLE_MS); };
    await set("desc_0", "Design work");
    await set("qty_0", "2");
    await set("price_0", "150");
    await until(`[...document.querySelectorAll('.inv-sum dd')][0].textContent === '$300.00'`, "the subtotal for line 1");
    await click("[brick\\:click='addLine']");
    await until("document.querySelector('input[name=desc_1]') !== null", "a second line");
    await set("desc_1", "Hosting");
    await set("qty_1", "1");
    await set("price_1", "50");
    await until(`[...document.querySelectorAll('.inv-sum dd')][0].textContent === '$350.00'`, "the subtotal for both lines");
    assert.equal(await total("Tax"), "$35.00");
    assert.equal(await total("Total"), "$385.00");
    clean("invoices");
  });

  await check("Tasks: move a card to the next column with the keyboard", async () => {
    const s = await app("/apps/tasks/");
    if (s) return s;
    const count = (list) => evaluate(`document.querySelectorAll('.bk-sortable[data-list=${list}] li[data-id]').length`);
    const [todo, doing] = [await count("todo"), await count("doing")];
    const id = await evaluate(`document.querySelector('.bk-sortable[data-list=todo] li[data-id]').dataset.id`);
    const card = `.bk-sortable li[data-id="${id}"]`;
    const handle = await evaluate(`(() => { const c = document.querySelector(${js(card)}); const h = c.querySelector('.bk-sortable__handle') || c; h.focus(); return document.activeElement === h; })()`);
    assert.ok(handle, "the card (or its handle) takes focus");
    await key(" ");
    await until(`document.querySelector(${js(card)}).classList.contains('bk-sortable__item--lifted')`, "Space to lift the card");
    await key("ArrowRight");
    await key(" ");
    await until(`document.querySelector('.bk-sortable[data-list=doing] li[data-id="${id}"]') !== null && document.querySelectorAll('.bk-sortable[data-list=todo] li[data-id]').length === ${todo - 1}`, "the card in In progress after the server re-render", ACTION_MS);
    assert.equal(await count("doing"), doing + 1);
    // The re-render above came from the server after the move was written,
    // so a fresh visit must show it.
    await visit("/apps/tasks/");
    assert.ok(await evaluate(`document.querySelector('.bk-sortable[data-list=doing] li[data-id="${id}"]') !== null`), "the move is saved");
    clean("tasks");
  });

  await check("Chat: send a message to Brickbot; its reply arrives on the poll", async () => {
    const s = await app("/apps/chat/");
    if (s) return s;
    await click(".chat__item[aria-label*='Brickbot'], .chat__item:has([aria-label^='Brickbot'])");
    await until(`/Brickbot/.test(document.querySelector('.chat__composer textarea').placeholder)`, "the Brickbot conversation");
    const bubbles = `document.querySelectorAll('.bk-chat__bubble').length`;
    const n0 = await evaluate(bubbles);
    const text = "Hello from the e2e suite " + Date.now();
    await click(".chat__composer textarea");
    await send("Input.insertText", { text });
    await key("Enter");
    await until(`[...document.querySelectorAll('.bk-chat__bubble')].some((b) => b.textContent.includes(${js(text)}))`, "the sent message");
    assert.equal(await evaluate("document.querySelector('.chat__composer textarea').value"), "", "the composer cleared");
    await until(`${bubbles} >= ${n0 + 2}`, "Brickbot's reply after a poll", POLL_REPLY_MS);
    clean("chat");
  });

  await check("Calendar: click a day, create an event", async () => {
    const s = await app("/apps/calendar/");
    if (s) return s;
    const day = await evaluate(`(() => { const b = [...document.querySelectorAll('.cal-m__add')].at(-5); b.id ||= 'e2e-day'; return b.getAttribute('aria-label'); })()`);
    await click("#e2e-day");
    await until("document.getElementById('cal-event').open", "the event dialog for " + day);
    const title = "E2E planning " + Date.now() % 10000;
    await type("#cal-event input[name=title]", title);
    await click("#cal-event button[form=cal-event-form]");
    await until(`!document.getElementById('cal-event').open && document.body.textContent.includes(${js(title)})`, "the event on the calendar");
    clean("calendar");
  });

  await check("CMS: new post, publish, visible on the blog", async () => {
    const s = await app("/apps/cms/");
    if (s) return s;
    await click("[brick\\:click='cms.new_post']");
    await until("document.querySelector('#cms-form input[name=title]') !== null", "the editor");
    const title = "Shipping the e2e suite " + Date.now() % 10000;
    await type("#cms-form input[name=title]", title);
    await click("#cms-form [contenteditable=true]");
    await send("Input.insertText", { text: "Every page, every click, in a real browser." });
    await sleep(CHANGE_SETTLE_MS);
    await click("[data-bk-open='cms-publish']");
    await until("document.getElementById('cms-publish').open", "the publish confirmation");
    await click("#cms-publish button[value=publish]");
    await until(`/published|live/i.test(document.querySelector('.cms-note')?.textContent || '') || [...document.querySelectorAll('.bk-toast')].some((t) => /publish/i.test(t.textContent))`, "the published note or toast");
    await visit("/apps/cms/blog/");
    assert.ok(await evaluate(`document.body.textContent.includes(${js(title)})`), "the post is on the blog");
    clean("cms");
  });

  await check("Dashboard: stats and charts render", async () => {
    const s = await app("/apps/dashboard/");
    if (s) return s;
    await until("document.querySelector('.bk-stat') !== null && document.querySelector('svg') !== null", "stats and a chart");
    clean("dashboard");
  });
} catch (e) {
  failed++;
  console.log("FAIL harness: " + (e.stack || e));
} finally {
  cdp.close();
}
if (failed) {
  console.log(`e2e ui: ${failed} failed`);
  process.exit(1);
}
console.log("e2e ui ok");
process.exit(0);
