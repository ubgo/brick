// Tests brick.refresh (the dev server's live reload) and the
// prefetch split with speculation rules, in jsdom. Run by runtime_test.go.
import { readFileSync } from "node:fs";
import assert from "node:assert/strict";
import { JSDOM, VirtualConsole } from "jsdom";
import { tick } from "./dom.mjs";

const runtimeJS = readFileSync(new URL("../runtime.js", import.meta.url), "utf8");
const navJS = readFileSync(new URL("../nav.js", import.meta.url), "utf8");

const page = ({ heading = "Old", css = "/_brick/c/site.1.css", style = "h1{color:red}" } = {}) => `<!doctype html><html><head><title>${heading}</title>
<link rel="stylesheet" href="${css}"><style>${style}</style></head><body>
<main><h1>${heading}</h1><input id="q" value="start"><p id="keep">same</p>
<a id="hover" href="/b/" brick:prefetch>b</a><a id="off" href="/c/" brick:prefetch brick:nav="off">c</a></main>
<brick-assets><script type="module" src="/_brick/c/runtime.1.js"></script><script type="module" src="/_brick/c/nav.1.js"></script></brick-assets>
</body></html>`;

function setup(routes) {
  const dom = new JSDOM(page(), { url: "https://site.test/a/", runScripts: "outside-only", pretendToBeVisual: true, virtualConsole: new VirtualConsole() });
  const w = dom.window;
  const calls = [];
  w.fetch = async (url, init = {}) => {
    calls.push({ url: String(url), cache: init.cache });
    const r = routes[new URL(String(url), "https://site.test/").pathname] || { status: 404, type: "text/plain", body: "no" };
    return {
      ok: r.status === undefined || r.status < 400,
      url: String(url),
      headers: { get: (h) => (h.toLowerCase() === "content-type" ? r.type || "text/html; charset=utf-8" : null) },
      text: async () => r.body,
    };
  };
  w.scrollTo = () => {};
  w.eval(runtimeJS);
  w.eval(navJS);
  const reloads = [];
  w.brick._reload = () => reloads.push(w.location.href);
  w.brick._load = (url) => reloads.push("load " + url);
  return { w, doc: w.document, calls, reloads };
}

// 1. A template edit morphs in: typed input, kept nodes and the URL stay,
//    no history entry is added, and the fetch bypasses the HTTP cache.
{
  const routes = { "/a/": { body: page() } };
  const { w, doc, calls, reloads } = setup(routes);
  const input = doc.getElementById("q");
  input.value = "typed";
  const kept = doc.getElementById("keep");
  const before = w.history.length;
  let refreshed = null;
  w.brick.on("refreshed", (d) => { refreshed = d.url; });
  routes["/a/"] = { body: page({ heading: "New" }) };
  await w.brick.refresh();
  await tick();
  assert.equal(doc.querySelector("h1").textContent, "New");
  assert.equal(doc.title, "New");
  assert.equal(input.value, "typed", "typed input survives");
  assert.equal(doc.getElementById("keep"), kept, "unchanged node is the same node");
  assert.equal(w.history.length, before, "no history entry");
  assert.deepEqual(reloads, []);
  assert.equal(refreshed, "https://site.test/a/");
  assert.equal(calls.at(-1).cache, "no-store");
}

// 2. A changed stylesheet (new content hash) gets a full reload.
{
  const routes = { "/a/": { body: page({ heading: "New", css: "/_brick/c/site.2.css" }) } };
  const { doc, reloads } = setup(routes);
  await doc.defaultView.brick.refresh();
  assert.equal(reloads.length, 1, "reloaded");
  assert.equal(doc.querySelector("h1").textContent, "Old", "not patched");
}

// 3. A changed inline head style gets a full reload too (the morph never
//    touches <head>).
{
  const { doc, reloads } = setup({ "/a/": { body: page({ heading: "New", style: "h1{color:blue}" }) } });
  await doc.defaultView.brick.refresh();
  assert.equal(reloads.length, 1);
}

// 4. A failed render (the dev error page answers 500) gets a full reload,
//    which shows the error block.
{
  const { doc, reloads } = setup({ "/a/": { status: 500, body: "boom" } });
  await doc.defaultView.brick.refresh();
  assert.equal(reloads.length, 1);
}

// 5. refresh drops the snapshot cache: a page visited before the edit is
//    fetched again on the next navigation.
{
  const routes = { "/a/": { body: page() }, "/b/": { body: page({ heading: "B" }) } };
  const { w, calls } = setup(routes);
  await w.brick.navigate("/b/");
  await w.brick.navigate("/a/");
  await w.brick.refresh();
  const n = calls.filter((c) => c.url.endsWith("/b/")).length;
  await w.brick.navigate("/b/");
  assert.equal(calls.filter((c) => c.url.endsWith("/b/")).length, n + 1, "refetched after refresh");
}

// 6. Prefetch: a link nav.js intercepts is prefetched by nav.js; one that
//    opted out (a full load, left to speculation rules) is not.
{
  const { w, doc, calls } = setup({ "/b/": { body: page({ heading: "B" }) }, "/c/": { body: page({ heading: "C" }) } });
  const focus = (id) => doc.getElementById(id).dispatchEvent(new w.FocusEvent("focusin", { bubbles: true }));
  focus("hover");
  focus("off");
  await tick();
  assert.ok(calls.some((c) => c.url.endsWith("/b/")), "intercepted link prefetched");
  assert.ok(!calls.some((c) => c.url.endsWith("/c/")), "opted-out link left to the browser");
}

console.log("nav refresh: ok");
