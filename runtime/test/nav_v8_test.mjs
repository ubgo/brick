// Tests the v8 navigation polish in jsdom: brick:current,
// html[data-navigating], a cancellable navigate, scroll restore on back,
// prefetch on focus and press, and replace.
import { readFileSync } from "node:fs";
import assert from "node:assert/strict";
import { JSDOM, VirtualConsole } from "jsdom";
import { tick, click, popped } from "./dom.mjs";

const runtimeJS = readFileSync(new URL("../runtime.js", import.meta.url), "utf8");
const navJS = readFileSync(new URL("../nav.js", import.meta.url), "utf8");

const page = (title) => `<!doctype html><html><head><title>${title}</title></head><body>
<nav id="side" brick:persist><a id="docs" href="/docs" brick:current>Docs</a><a id="home" href="/" brick:current>Home</a><a id="intro" href="/docs/intro" brick:current.exact>Intro</a><a id="ds" href="/docsearch" brick:current>Search</a><a id="plain" href="/docs" aria-current="page">Plain</a><a id="pf" href="/docs/intro" brick:prefetch>pf</a><a id="rep" href="/docs/intro" brick:nav="replace">rep</a></nav>
<main><h1>${title}</h1></main></body></html>`;

function setup(path, routes, opts = {}) {
  const dom = new JSDOM(page("start"), { url: "https://site.test" + path, runScripts: "outside-only", pretendToBeVisual: true, virtualConsole: new VirtualConsole() });
  const w = dom.window;
  const calls = [];
  w.fetch = async (url) => {
    calls.push(String(url));
    const p = new URL(String(url), "https://site.test/").pathname;
    if (opts.delay) await new Promise((r) => setTimeout(r, opts.delay));
    const body = routes[p];
    return { ok: Boolean(body), url: String(url), headers: { get: () => "text/html" }, text: async () => body || "" };
  };
  const scrolls = [];
  w.scrollTo = (x, y) => { scrolls.push([x, y]); };
  w.eval(runtimeJS);
  w.eval(navJS);
  const loads = [];
  w.brick._load = (url) => loads.push(url);
  return { w, doc: w.document, $: (id) => w.document.getElementById(id), calls, loads, scrolls };
}

const cur = (el) => [el.getAttribute("aria-current"), el.hasAttribute("data-current")];

// brick:current at boot and after a navigation: segment prefix, exact,
// /docsearch not current for /docs, the root only on the root, and a link
// without brick:current left alone.
{
  const { w, $ } = setup("/docs/intro", { "/": page("home") });
  assert.deepEqual(cur($("docs")), ["page", true], "prefix match");
  assert.deepEqual(cur($("intro")), ["page", true], "exact match");
  assert.deepEqual(cur($("ds")), [null, false], "/docsearch is not under /docs");
  assert.deepEqual(cur($("home")), [null, false], "root only on the root");
  click(w, $("home"));
  await tick();
  assert.equal(w.location.pathname, "/");
  assert.deepEqual(cur($("home")), ["page", true], "root current on the root");
  assert.deepEqual(cur($("docs")), [null, false], "cleared after navigating away");
  assert.equal($("plain").getAttribute("aria-current"), "page", "a link without brick:current is left alone");
}

// data-navigating only once the fetch outlasts the delay.
{
  const { w, $, doc } = setup("/", { "/docs": page("docs") }, { delay: 250 });
  click(w, $("docs"));
  await tick(50);
  assert.equal(doc.documentElement.hasAttribute("data-navigating"), false, "not before the delay");
  await tick(150);
  assert.equal(doc.documentElement.hasAttribute("data-navigating"), true, "after the delay");
  await tick(150);
  assert.equal(doc.documentElement.hasAttribute("data-navigating"), false, "cleared when done");
}

// A prevented navigate fetches nothing; on popstate the shown URL is put
// back.
{
  const { w, $, calls } = setup("/", { "/docs": page("docs"), "/": page("home") });
  click(w, $("docs"));
  await tick();
  assert.equal(w.location.pathname, "/docs");
  const off = w.brick.on("navigate", (d) => d.preventDefault());
  const n = calls.length;
  click(w, $("intro"));
  await tick();
  assert.equal(calls.length, n, "prevented: no fetch");
  assert.equal(w.location.pathname, "/docs");
  await popped(w, () => w.history.back());
  await tick(50);
  assert.equal(calls.length, n, "prevented popstate: no fetch");
  assert.equal(w.location.pathname, "/docs", "the shown URL is back in the address bar");
  off();
}

// Back restores the scroll position the reader left.
{
  const { w, $, scrolls } = setup("/", { "/docs": page("docs"), "/": page("home") });
  Object.defineProperty(w, "scrollY", { value: 420, configurable: true, writable: true });
  w.dispatchEvent(new w.Event("scroll"));
  click(w, $("docs"));
  await tick();
  assert.deepEqual(scrolls.at(-1), [0, 0], "a new page starts at the top");
  w.scrollY = 0;
  await popped(w, () => w.history.back());
  await tick(50);
  assert.equal(w.location.pathname, "/");
  assert.deepEqual(scrolls.at(-1), [0, 420], "back restores");
}

// Prefetch on focus and on press, and after hover intent only.
{
  const { w, $, calls } = setup("/", { "/docs/intro": page("intro") });
  $("pf").dispatchEvent(new w.FocusEvent("focusin", { bubbles: true }));
  await tick();
  assert.equal(calls.length, 1, "focus prefetches");
}
{
  const { w, $, calls } = setup("/", { "/docs/intro": page("intro") });
  $("pf").dispatchEvent(new w.MouseEvent("pointerover", { bubbles: true }));
  $("pf").dispatchEvent(new w.MouseEvent("pointerout", { bubbles: true }));
  await tick(100);
  assert.equal(calls.length, 0, "a passing pointer does not prefetch");
  $("pf").dispatchEvent(new w.MouseEvent("pointerover", { bubbles: true }));
  await tick(100);
  assert.equal(calls.length, 1, "a resting pointer does");
}
{
  const { w, $, calls } = setup("/", { "/docs/intro": page("intro") });
  $("pf").dispatchEvent(new w.MouseEvent("pointerdown", { bubbles: true }));
  await tick();
  assert.equal(calls.length, 1, "press prefetches");
}

// replace keeps the history length: brick:nav="replace" and the API.
{
  const { w, $ } = setup("/", { "/docs/intro": page("intro"), "/docs": page("docs") });
  const n = w.history.length;
  click(w, $("rep"));
  await tick();
  assert.equal(w.location.pathname, "/docs/intro");
  assert.equal(w.history.length, n, "link replace");
  await w.brick.navigate("/docs", { replace: true });
  assert.equal(w.location.pathname, "/docs");
  assert.equal(w.history.length, n, "API replace");
  await w.brick.navigate("/docs/intro");
  assert.equal(w.history.length, n + 1, "default pushes");
}

console.log("nav v8 ok");
