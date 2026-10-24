// Tests persisted scroll containers (G20) in jsdom: an element with
// brick:scroll="<name>" keeps its position across a navigation, even when
// the next page builds it differently, back/forward restore the position
// that history entry had, and containers without the attribute or with
// another name are left alone.
import { readFileSync } from "node:fs";
import assert from "node:assert/strict";
import { JSDOM, VirtualConsole } from "jsdom";
import { tick, click, popped } from "./dom.mjs";

const runtimeJS = readFileSync(new URL("../runtime.js", import.meta.url), "utf8");
const navJS = readFileSync(new URL("../nav.js", import.meta.url), "utf8");

// side is the kept container; on /b it is a <section> in another place, so
// the morph replaces the node (which in a browser resets its scroll).
// plain has no brick:scroll and is also rebuilt on /b; other has a name no
// other page uses.
const pages = {
  "/a": `<!doctype html><html><head><title>a</title></head><body><div id="side-a" brick:scroll="side"><a id="to-b" href="/b">b</a></div><div id="plain"></div><div id="other" brick:scroll="other"></div><main><h1>a</h1></main></body></html>`,
  "/b": `<!doctype html><html><head><title>b</title></head><body><main><h1>b</h1></main><section id="side-b" brick:scroll="side"><a id="to-c" href="/c">c</a></section><p id="plain"></p></body></html>`,
  "/c": `<!doctype html><html><head><title>c</title></head><body><section id="side-c" brick:scroll="side"></section><main><h1>c</h1></main></body></html>`,
};

function setup(path) {
  const dom = new JSDOM(pages[path], { url: "https://site.test" + path, runScripts: "outside-only", pretendToBeVisual: true, virtualConsole: new VirtualConsole() });
  const w = dom.window;
  // jsdom does no layout, so scrollTop is always 0; a per-element store
  // stands in for it. A new node starts at 0, as in a browser.
  const store = new WeakMap();
  for (const prop of ["scrollTop", "scrollLeft"]) {
    Object.defineProperty(w.Element.prototype, prop, {
      configurable: true,
      get() { return (store.get(this) || {})[prop] || 0; },
      set(v) { const o = store.get(this) || {}; o[prop] = v; store.set(this, o); },
    });
  }
  w.fetch = async (url) => {
    const body = pages[new URL(String(url), "https://site.test/").pathname];
    return { ok: Boolean(body), url: String(url), headers: { get: () => "text/html" }, text: async () => body || "" };
  };
  w.scrollTo = () => {};
  w.eval(runtimeJS);
  w.eval(navJS);
  w.brick._load = (url) => { throw new Error("unexpected full load of " + url); };
  const $ = (id) => w.document.getElementById(id);
  // scroll sets a container's position and fires its (non-bubbling)
  // scroll event, as the reader scrolling would.
  const scroll = (el, top) => { el.scrollTop = top; el.dispatchEvent(new w.Event("scroll")); };
  const side = () => w.document.querySelector('[brick\\:scroll="side"]');
  return { w, $, scroll, side };
}

{
  const { w, $, scroll, side } = setup("/a");
  scroll($("side-a"), 300);
  $("plain").scrollTop = 50;
  scroll($("other"), 70);
  click(w, $("to-b"));
  await tick();
  assert.equal(w.location.pathname, "/b");
  assert.equal(side().id, "side-b", "the next page's container is shown");
  assert.equal(side().scrollTop, 300, "a forward navigation carries the position to the same name");
  assert.equal($("plain").scrollTop, 0, "an element without brick:scroll is not kept");

  // On /b the reader scrolls further, then goes on to /c.
  scroll(side(), 500);
  click(w, $("to-c"));
  await tick();
  assert.equal(side().id, "side-c");
  assert.equal(side().scrollTop, 500);
  scroll(side(), 900);

  // Back restores what each entry had, not the latest position.
  await popped(w, () => w.history.back());
  await tick(50);
  assert.equal(w.location.pathname, "/b");
  assert.equal(side().scrollTop, 500, "back to /b restores /b's position");
  await popped(w, () => w.history.back());
  await tick(50);
  assert.equal(w.location.pathname, "/a");
  assert.equal(side().scrollTop, 300, "back to /a restores /a's position");
  assert.equal($("other").scrollTop, 70, "a name only /a has comes back with /a");
  w.history.forward();
  await tick(50);
  assert.equal(w.location.pathname, "/b");
  assert.equal(side().scrollTop, 500, "forward restores too");
}

// The dev server's refresh keeps the position as well.
{
  const { w, $, scroll, side } = setup("/a");
  scroll($("side-a"), 120);
  await w.brick.refresh();
  assert.equal(side().scrollTop, 120, "a live reload keeps the container's position");
}

console.log("nav scroll ok");
