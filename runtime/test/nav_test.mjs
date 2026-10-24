// Tests nav.js (with runtime.js) in jsdom. Run by runtime_test.go when
// runtime/test/node_modules exists (task js:deps).
import { readFileSync } from "node:fs";
import assert from "node:assert/strict";
import { JSDOM, VirtualConsole } from "jsdom";
import { tick, click, popped } from "./dom.mjs";

const runtimeJS = readFileSync(new URL("../runtime.js", import.meta.url), "utf8");
const navJS = readFileSync(new URL("../nav.js", import.meta.url), "utf8");

const pageA = `<!doctype html><html><head><title>A</title><meta name="description" content="about a"></head><body>
<header id="top"><nav><a id="to-b" href="/b/">B</a><a id="off" href="/b/" brick:nav="off">B</a><a id="ext" href="https://other.test/">x</a><a id="hash" href="#s">s</a></nav></header>
<main><h1>Page A</h1><input id="q" value="start"><div data-brick-type="W" data-brick="w1">w</div></main>
<aside id="player" brick:persist>playing</aside>
<form id="search" action="/search/" method="get"><input name="q" value="go"></form>
<form id="post" action="/save/" method="post"><input name="x" value="1"></form>
<brick-assets><script type="module" src="/_brick/c/runtime.1.js"></script></brick-assets>
</body></html>`;

const pageB = `<!doctype html><html><head><title>B</title><meta name="description" content="about b"></head><body>
<header id="top"><nav><a id="to-b" href="/b/">B</a><a id="off" href="/b/" brick:nav="off">B</a><a id="ext" href="https://other.test/">x</a><a id="hash" href="#s">s</a></nav></header>
<main><h1>Page B</h1><input id="q" value="start"><div data-brick-type="V" data-brick="v1">v</div></main>
<aside id="player" brick:persist>new content that must not replace the player</aside>
<brick-assets><script type="module" src="/_brick/c/runtime.1.js"></script><script type="module" src="/_brick/c/V.2.js"></script></brick-assets>
</body></html>`;

function setup(routes) {
  const dom = new JSDOM(pageA, { url: "https://site.test/a/", runScripts: "outside-only", pretendToBeVisual: true, virtualConsole: new VirtualConsole() });
  const w = dom.window;
  const calls = [];
  w.fetch = async (url, init = {}) => {
    calls.push({ url: String(url), method: init.method || "GET", body: init.body });
    const r = routes[new URL(String(url), "https://site.test/").pathname] || { status: 404, type: "text/plain", body: "no" };
    return {
      ok: r.status === undefined || r.status < 400,
      url: r.finalURL ? `https://site.test${r.finalURL}` : String(url),
      headers: { get: (h) => (h.toLowerCase() === "content-type" ? r.type || "text/html; charset=utf-8" : null) },
      text: async () => r.body,
    };
  };
  w.scrollTo = () => {};
  Object.defineProperty(w.HTMLElement.prototype, "scrollIntoView", { value() {}, configurable: true });
  w.eval(runtimeJS);
  w.eval(navJS);
  const loads = [];
  w.brick._load = (url) => loads.push(url);
  return { dom, w, doc: w.document, calls, loads };
}


// 1. A link navigates in place, keeping unchanged and persistent elements.
{
  const { w, doc, calls } = setup({ "/b/": { body: pageB } });
  let unmounted = 0, mounted = [];
  w.brick.mount("W", () => () => unmounted++);
  w.brick.mount("V", (el) => { mounted.push(el.textContent); });
  const header = doc.getElementById("top");
  const player = doc.getElementById("player");
  const input = doc.getElementById("q");
  input.value = "typed";
  const events = [];
  w.brick.on("navigated", (d) => events.push(d.url));
  const ev = click(w, doc.getElementById("to-b"));
  assert.ok(ev.defaultPrevented, "internal link intercepted");
  await tick();
  assert.equal(calls[0].url, "https://site.test/b/");
  assert.equal(doc.title, "B");
  assert.equal(doc.querySelector('meta[name="description"]').getAttribute("content"), "about b");
  assert.equal(w.location.pathname, "/b/");
  assert.equal(doc.querySelector("h1").textContent, "Page B");
  assert.equal(doc.getElementById("top"), header, "unchanged header kept (same element)");
  assert.equal(doc.getElementById("player"), player, "persistent element kept");
  assert.equal(player.textContent, "playing", "persistent element not updated");
  assert.equal(doc.getElementById("q"), input, "matched input kept");
  assert.equal(input.value, "typed", "typed value kept when the page value is unchanged");
  assert.equal(unmounted, 1, "removed component unmounted");
  assert.deepEqual(mounted, ["v"], "new component mounted");
  const scripts = [...doc.querySelectorAll("brick-assets script")].map((s) => s.getAttribute("src"));
  assert.deepEqual(scripts, ["/_brick/c/runtime.1.js", "/_brick/c/V.2.js"], "new module merged once");
  assert.equal(doc.activeElement, doc.querySelector("main"), "focus moved to main");
  assert.equal(doc.getElementById("brick-live").textContent, "B", "title announced");
  assert.deepEqual(events, ["https://site.test/b/"]);
}

// 2. What is not intercepted.
{
  const { w, doc, calls } = setup({ "/b/": { body: pageB } });
  for (const [id, opts] of [["off", {}], ["ext", {}], ["hash", {}], ["to-b", { metaKey: true }], ["to-b", { ctrlKey: true }]]) {
    const ev = click(w, doc.getElementById(id), opts);
    assert.ok(!ev.defaultPrevented, `${id} ${JSON.stringify(opts)} not intercepted`);
  }
  await tick();
  assert.equal(calls.length, 0, JSON.stringify(calls));
}

// 3. A non-HTML or failed response falls back to a full load.
{
  const { w, doc, loads } = setup({ "/b/": { body: "{}", type: "application/json" } });
  click(w, doc.getElementById("to-b"));
  await tick();
  assert.deepEqual(loads, ["https://site.test/b/"], "full load on non-HTML");
  assert.equal(doc.title, "A", "page untouched");
}

// 4. Forms: GET becomes a query navigation; POST follows the redirect.
{
  const { w, doc, calls } = setup({ "/search/": { body: pageB }, "/save/": { body: pageB, finalURL: "/saved/" } });
  const get = new w.SubmitEvent("submit", { bubbles: true, cancelable: true });
  doc.getElementById("search").dispatchEvent(get);
  await tick();
  assert.ok(get.defaultPrevented);
  assert.equal(calls[0].url, "https://site.test/search/?q=go");
  const post = new w.SubmitEvent("submit", { bubbles: true, cancelable: true });
  // The GET navigation replaced the forms; re-add one to post.
  doc.body.insertAdjacentHTML("beforeend", '<form id="post2" action="/save/" method="post"><input name="x" value="1"></form>');
  doc.getElementById("post2").dispatchEvent(post);
  await tick();
  assert.equal(calls[1].method, "POST");
  assert.equal(calls[1].body.get("x"), "1");
  assert.equal(w.location.pathname, "/saved/", "the redirect's final URL is shown");
  // A method="dialog" form only closes its dialog: never a navigation.
  doc.body.insertAdjacentHTML("beforeend", '<dialog open><form id="dlg" method="dialog"><button value="ok">OK</button></form></dialog>');
  const dlg = new w.SubmitEvent("submit", { bubbles: true, cancelable: true });
  doc.getElementById("dlg").dispatchEvent(dlg);
  await tick();
  assert.ok(!dlg.defaultPrevented, "a dialog form is left to the browser");
  assert.equal(calls.length, 2, "no request for a dialog form");
}

// 5. Back uses the cache: no second fetch.
{
  const { w, doc, calls } = setup({ "/b/": { body: pageB }, "/a/": { body: pageA } });
  click(w, doc.getElementById("to-b"));
  await tick();
  await popped(w, () => w.history.back());
  await tick();
  assert.equal(doc.title, "A");
  click(w, doc.getElementById("to-b"));
  await tick();
  assert.equal(calls.filter((c) => c.url.endsWith("/b/")).length, 1, "second visit served from the snapshot cache");
}

// 6. The morph keeps keyed list items by id, whatever their order.
{
  const { w, doc } = setup({});
  const list = doc.createElement("ul");
  list.innerHTML = '<li id="a">A</li><li id="b">B</li><li id="c">C</li>';
  doc.body.appendChild(list);
  const [a, b, c] = [...list.children];
  const next = doc.createElement("ul");
  next.innerHTML = '<li id="c">C!</li><li id="a">A</li><li id="d">D</li>';
  w.brick._morph(list, next);
  assert.deepEqual([...list.children].map((li) => li.id), ["c", "a", "d"]);
  assert.equal(list.children[0], c, "c kept and moved");
  assert.equal(list.children[1], a, "a kept");
  assert.equal(c.textContent, "C!", "c updated");
  assert.ok(!b.isConnected, "b removed");
}

// 7. A persistent element moves to where the new page puts it, even into
// a different parent, without being recreated or updated.
{
  const { w, doc } = setup({});
  doc.body.innerHTML = '<aside><div id="p" brick:persist>old</div></aside><main></main>';
  const p = doc.getElementById("p");
  const next = doc.createElement("body");
  next.innerHTML = '<aside></aside><main><div id="p" brick:persist>new</div></main>';
  w.brick._morph(doc.body, next);
  assert.equal(doc.querySelector("main #p"), p, "moved, same element");
  assert.equal(p.textContent, "old", "not updated");
  assert.equal(doc.querySelectorAll("#p").length, 1, "no duplicate");
}

// 8. …and into a parent that is new on this page.
{
  const { w, doc } = setup({});
  doc.body.innerHTML = '<aside><div id="p" brick:persist>old</div></aside>';
  const p = doc.getElementById("p");
  const next = doc.createElement("body");
  next.innerHTML = '<section class="new"><div id="p" brick:persist>new</div></section>';
  w.brick._morph(doc.body, next);
  assert.equal(doc.querySelector("section.new #p"), p, "moved into new markup");
  assert.equal(doc.querySelectorAll("#p").length, 1, "no duplicate");
  assert.equal(doc.querySelector("aside"), null, "old parent removed");
}

// 9. A site mounted under a base path navigates in place only within it: a
// link to another site on the same origin is a normal page load.
{
  const page = `<!doctype html><html><head><meta name="brick-base" content="/ui"></head><body><main><a id="in" href="/ui/b/">in</a><a id="out" href="/engine/">out</a><a id="root" href="/ui">root</a></main></body></html>`;
  const dom = new JSDOM(page, { url: "https://site.test/ui/a/", runScripts: "outside-only", pretendToBeVisual: true, virtualConsole: new VirtualConsole() });
  const w = dom.window;
  const calls = [];
  w.fetch = async (url) => { calls.push(String(url)); return { ok: true, url: String(url), headers: { get: () => "text/html" }, text: async () => page }; };
  w.scrollTo = () => {};
  w.eval(runtimeJS);
  w.eval(navJS);
  w.brick._load = () => {};
  assert.equal(click(w, w.document.getElementById("out")).defaultPrevented, false, "another site: a normal load");
  assert.equal(click(w, w.document.getElementById("in")).defaultPrevented, true, "inside the base: in place");
  assert.equal(click(w, w.document.getElementById("root")).defaultPrevented, true, "the base itself is inside");
  await tick();
  assert.deepEqual(calls.map((u) => new URL(u, "https://site.test/").pathname), ["/ui/b/", "/ui"], "fetched in place");
  w.close();
}

// 10. A page built for another runtime (a deploy changed runtime.<hash>.js)
// is a full load, so this tab never runs new pages on its old runtime.
{
  const deployed = pageB.replace("runtime.1.js", "runtime.9.js");
  const { w, doc, loads } = setup({ "/b/": { body: deployed } });
  click(w, doc.getElementById("to-b"));
  await tick();
  assert.deepEqual(loads, ["https://site.test/b/"], "full load");
  assert.equal(doc.querySelector("h1").textContent, "Page A", "not patched in");
}

console.log("nav ok");
