// Tests nav.js's v22 additions in jsdom: fragment navigation (the server
// answers a navigation whose layout matches with only the page) and the
// patch (a link that changes only parameters components on the page own).
import { readFileSync } from "node:fs";
import assert from "node:assert/strict";
import { JSDOM, VirtualConsole } from "jsdom";
import { tick, click } from "./dom.mjs";

const runtimeJS = readFileSync(new URL("../runtime.js", import.meta.url), "utf8");
const navJS = readFileSync(new URL("../nav.js", import.meta.url), "utf8");

const LAYOUT = "fp1";
const page = (title, body, { marks = true } = {}) => `<!doctype html><html><head><title>${title}</title><meta name="description" content="d ${title}"><meta name="brick-layout" content="${LAYOUT}"></head><body>
<header id="top"><input id="search" value=""><a id="to-b" href="/b/">B</a><a id="to-c" href="/c/">C</a></header>
<main id="main">${marks ? "<!--brick:page-->" : ""}${body}${marks ? "<!--/brick:page-->" : ""}</main>
<brick-assets><script type="module" src="/_brick/c/runtime.1.js"></script></brick-assets>
</body></html>`;

// fragment is what the site layer sends for a matching layout.
const fragment = (title, body, assets = "") => `<!doctype html><html><head><title>${title}</title><meta name="description" content="d ${title}"></head><body><brick-assets><script type="module" src="/_brick/c/runtime.1.js"></script>${assets}</brick-assets>${body}</body></html>`;

function setup(html, routes) {
  const dom = new JSDOM(html, { url: "https://site.test/a/", runScripts: "outside-only", pretendToBeVisual: true, virtualConsole: new VirtualConsole() });
  const w = dom.window;
  const calls = [];
  w.fetch = async (url, init = {}) => {
    calls.push({ url: String(url), nav: (init.headers || {})["Brick-Nav"] || "" });
    const r = routes[new URL(String(url), "https://site.test/").pathname];
    const hdr = { "content-type": "text/html; charset=utf-8", "brick-fragment": r.fragment ? "1" : null, "brick-layout": r.layout || null };
    return { ok: true, url: String(url), headers: { get: (h) => hdr[h.toLowerCase()] ?? null }, text: async () => r.body };
  };
  w.scrollTo = () => {};
  Object.defineProperty(w.HTMLElement.prototype, "scrollIntoView", { value() {}, configurable: true });
  w.eval(runtimeJS);
  w.eval(navJS);
  const loads = [];
  w.brick._load = (url) => loads.push(url);
  return { w, doc: w.document, calls, loads };
}

// 1. A page with its marks sends its layout; a fragment is spliced between
//    the marks, the layout around it is left as it is (same nodes, typed
//    input kept), the title, description and new assets arrive.
{
  const { w, doc, calls, loads } = setup(page("A", "<h1>Page A</h1><p>a</p>"), {
    "/b/": { fragment: true, layout: LAYOUT, body: fragment("B", "<h1>Page B</h1><p id=pb>b</p>", `<script type="module" src="/_brick/c/V.2.js"></script>`) },
  });
  const header = doc.getElementById("top");
  doc.getElementById("search").value = "typed";
  click(w, doc.getElementById("to-b"));
  await tick();
  assert.deepEqual(loads, [], "no full load");
  assert.equal(calls[0].nav, LAYOUT, "Brick-Nav carries the shown layout");
  assert.equal(doc.title, "B");
  assert.equal(doc.querySelector('meta[name="description"]').getAttribute("content"), "d B");
  assert.equal(doc.getElementById("top"), header, "layout node kept");
  assert.equal(doc.getElementById("search").value, "typed", "typed layout input kept");
  assert.equal(doc.querySelector("h1").textContent, "Page B");
  assert.ok(doc.getElementById("pb"), "page body spliced in");
  assert.equal(doc.querySelectorAll("brick-page").length, 0, "no wrapper left behind");
  assert.equal(doc.querySelectorAll("brick-assets").length, 1, "the page keeps one asset block");
  const scripts = [...doc.querySelectorAll("brick-assets script")].map((s) => s.getAttribute("src"));
  assert.deepEqual(scripts, ["/_brick/c/runtime.1.js", "/_brick/c/V.2.js"], "fragment assets merged");
  const main = doc.getElementById("main");
  assert.equal(main.firstChild.nodeType, 8, "start mark still first");
  assert.equal(main.lastChild.nodeType, 8, "end mark still last");
  assert.equal(main.querySelectorAll("h1").length, 1, "old page removed");
}

// 2. A full answer (another layout) is patched in as before and its layout
//    becomes the one sent next time.
{
  const { w, doc, calls, loads } = setup(page("A", "<h1>Page A</h1>"), {
    "/c/": { layout: "fp2", body: page("C", "<h1>Page C</h1>").replace(LAYOUT, "fp2") },
    "/b/": { layout: "fp2", fragment: true, body: fragment("B", "<h1>Page B</h1>") },
  });
  click(w, doc.getElementById("to-c"));
  await tick();
  assert.equal(doc.querySelector("h1").textContent, "Page C");
  click(w, doc.getElementById("to-b"));
  await tick();
  assert.deepEqual(loads, []);
  assert.equal(calls[1].nav, "fp2", "the new layout's fingerprint is sent");
  assert.equal(doc.querySelector("h1").textContent, "Page B");
}

// 3. A page without marks never asks for a fragment.
{
  const { w, doc, calls } = setup(page("A", "<h1>Page A</h1>", { marks: false }), {
    "/b/": { body: page("B", "<h1>Page B</h1>", { marks: false }) },
  });
  click(w, doc.getElementById("to-b"));
  await tick();
  assert.equal(calls[0].nav, "", "no marks, no Brick-Nav");
  assert.equal(doc.querySelector("h1").textContent, "Page B");
}

// 4. A fragment that arrives when the marks are gone is not spliced
//    anywhere: the page is loaded in full.
{
  const { w, doc, loads } = setup(page("A", "<h1>Page A</h1>"), {
    "/b/": { fragment: true, layout: LAYOUT, body: fragment("B", "<h1>Page B</h1>") },
  });
  const fetchB = w.fetch;
  w.fetch = async (u, i) => { const r = await fetchB(u, i); doc.querySelectorAll("main")[0].innerHTML = "<h1>moved</h1>"; return r; };
  click(w, doc.getElementById("to-b"));
  await tick();
  assert.deepEqual(loads, ["https://site.test/b/"], "full load instead of a blind splice");
}

// 5. A patch: a link to this page that changes only parameters a
//    component on it owns fetches no page; the owner gets $params with the
//    new values, and the URL is pushed.
{
  const owned = `<section id="cat" data-brick="c1" data-brick-state="s.h1" data-brick-token="t1" data-brick-query='{"tab":["tab","all"],"page":["page",1]}'>tab</section>`;
  const html = page("A", `<h1>Page A</h1>${owned}<a id="p1" href="/a/?tab=books">books</a><a id="p2" href="/a/?tab=books&amp;x=1">foreign</a>`);
  const { w, doc, calls, loads } = setup(html, { "/a/": { body: page("A", "<h1>full</h1>") } });
  const sent = [];
  const base = w.fetch;
  w.fetch = async (url, init = {}) => {
    if (String(url).includes("/_brick/action")) {
      sent.push(JSON.parse(init.body));
      return { ok: true, status: 200, url: String(url), headers: { get: (h) => (h.toLowerCase() === "content-type" ? "text/html" : null) }, text: async () => owned };
    }
    return base(url, init);
  };
  const header = doc.getElementById("top");
  click(w, doc.getElementById("p1"));
  await tick();
  assert.deepEqual(loads, []);
  assert.equal(w.location.search, "?tab=books", "URL pushed");
  assert.equal(calls.length, 0, "no page fetched");
  assert.equal(sent.length, 1, "one action");
  assert.equal(sent[0].action, "$params");
  assert.deepEqual(sent[0].sets, [["tab", "books"], ["page", 1]], "the owner's fields from the URL");
  assert.equal(doc.getElementById("top"), header);
  click(w, doc.getElementById("p2"));
  await tick();
  assert.equal(calls.length, 1, "a parameter nobody owns is a navigation");
}

console.log("nav v22: ok");
