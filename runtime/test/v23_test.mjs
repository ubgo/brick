// Tests brick:method links (v23) in jsdom: a plain click builds a hidden
// form to the href and submits it natively (_method for a verb other than
// POST), a modified click is left to the browser, brick:confirm asks
// first, brick:disable-with marks the link busy, brick.methodFormWith adds
// the host's fields, and nav.js neither navigates nor prefetches such a
// link. headless Chrome covers the same against a real router in
// v23_chrome.mjs.
import { readFileSync } from "node:fs";
import assert from "node:assert/strict";
import { JSDOM, VirtualConsole } from "jsdom";
import { tick, click } from "./dom.mjs";

const runtimeJS = readFileSync(new URL("../runtime.js", import.meta.url), "utf8");
const navJS = readFileSync(new URL("../nav.js", import.meta.url), "utf8");

// setup loads html with the runtime (and nav.js when nav is set).
// submitted records each natively submitted form as {action, method,
// target, fields, inBody}; fetched records every fetch, which a method
// link must never cause.
async function setup(html, { nav = false } = {}) {
  const dom = new JSDOM(`<!doctype html><body>${html}</body>`, { url: "https://site.test/posts", runScripts: "outside-only", pretendToBeVisual: true, virtualConsole: new VirtualConsole() });
  const w = dom.window;
  const submitted = [];
  const fetched = [];
  const errors = [];
  w.HTMLFormElement.prototype.submit = function () {
    submitted.push({ action: this.action, method: this.method, target: this.target, hidden: this.hidden, inBody: this.parentNode === w.document.body, fields: [...new w.FormData(this)] });
  };
  w.fetch = (url) => { fetched.push(String(url)); return new Promise(() => {}); };
  w.console.error = (e) => errors.push(String(e && e.message || e));
  w.console.warn = () => {};
  w.eval(runtimeJS);
  if (nav) w.eval(navJS);
  await tick();
  return { w, $: (s) => w.document.querySelector(s), submitted, fetched, errors };
}

async function main() {
// 1. A delete link posts a hidden form to its href with _method=delete,
// and the click's default (a GET) is prevented.
{
  const { w, $, submitted } = await setup(`<a id="d" href="/posts/7" brick:method="delete">Delete</a>`);
  const ev = click(w, $("#d"));
  await tick();
  assert.equal(ev.defaultPrevented, true);
  assert.deepEqual(submitted, [{ action: "https://site.test/posts/7", method: "post", target: "", hidden: true, inBody: true, fields: [["_method", "delete"]] }]);
  await tick();
  assert.equal(w.document.querySelectorAll("form").length, 0, "the hidden form is removed after the submit");
}

// 2. Every verb: POST sends no _method field; the others send theirs,
// lower-cased whatever the case written.
{
  const { w, $, submitted } = await setup(`<a id="p" href="/a" brick:method="post">p</a><a id="u" href="/a" brick:method="PUT">u</a><a id="h" href="/a" brick:method=" Patch ">h</a>`);
  click(w, $("#p"));
  click(w, $("#u"));
  click(w, $("#h"));
  await tick();
  assert.deepEqual(submitted.map((s) => s.fields), [[], [["_method", "put"]], [["_method", "patch"]]]);
}

// 3. A modified or non-primary click is the browser's (a new tab opens the
// GET, as without JS): nothing is submitted and the default stands.
{
  const { w, $, submitted } = await setup(`<a id="d" href="/posts/7" brick:method="delete"><span id="in">Delete</span></a>`);
  for (const opts of [{ metaKey: true }, { ctrlKey: true }, { shiftKey: true }, { altKey: true }, { button: 1 }]) {
    const ev = click(w, $("#d"), opts);
    assert.equal(ev.defaultPrevented, false, JSON.stringify(opts));
  }
  await tick();
  assert.equal(submitted.length, 0);
  // A click on a child of the link is the link's.
  click(w, $("#in"));
  await tick();
  assert.equal(submitted.length, 1);
}

// 4. brick:confirm asks first: a cancel sends nothing, a yes submits; the
// confirmer gets the message and the link.
{
  const { w, $, submitted } = await setup(`<a id="d" href="/posts/7" brick:method="delete" brick:confirm="Delete it?">Delete</a>`);
  const asked = [];
  let yes = false;
  w.brick.confirmWith((m, el) => { asked.push([m, el.id]); return Promise.resolve(yes); });
  const ev = click(w, $("#d"));
  await tick();
  assert.equal(ev.defaultPrevented, true, "a cancelled confirm still keeps the GET from happening");
  assert.equal(submitted.length, 0);
  yes = true;
  click(w, $("#d"));
  await tick();
  assert.deepEqual(asked, [["Delete it?", "d"], ["Delete it?", "d"]]);
  assert.equal(submitted.length, 1);
}

// 5. brick.methodFormWith lets the host add its CSRF token; the function
// it returns puts the previous hook back.
{
  const { w, $, submitted } = await setup(`<meta name="csrf" content="tok-1"><a id="d" href="/posts/7" brick:method="delete">Delete</a>`);
  const off = w.brick.methodFormWith((form, link) => {
    const f = w.document.createElement("input");
    f.type = "hidden"; f.name = "csrf"; f.value = w.document.querySelector('meta[name="csrf"]').content + ":" + link.id;
    form.appendChild(f);
  });
  click(w, $("#d"));
  await tick();
  off();
  click(w, $("#d"));
  await tick();
  assert.deepEqual(submitted.map((s) => s.fields), [[["_method", "delete"], ["csrf", "tok-1:d"]], [["_method", "delete"]]]);
  assert.throws(() => w.brick.methodFormWith("nope"), /not a function/);
}

// 6. brick:disable-with shows the busy label, marks the link
// aria-disabled and ignores a second click while the post is under way.
{
  const { w, $, submitted } = await setup(`<a id="d" href="/posts/7" brick:method="delete" brick:disable-with="Deleting…">Delete</a>`);
  click(w, $("#d"));
  click(w, $("#d"));
  await tick();
  assert.equal(submitted.length, 1);
  assert.equal($("#d").textContent, "Deleting…");
  assert.equal($("#d").getAttribute("aria-disabled"), "true");
  // Back to the page from the back/forward cache: the link is idle again.
  const ev = new w.Event("pageshow");
  ev.persisted = true;
  w.dispatchEvent(ev);
  assert.equal($("#d").textContent, "Delete");
  assert.equal($("#d").hasAttribute("aria-disabled"), false);
}

// 7. A target is copied, so the answer opens where the link would have.
{
  const { w, $, submitted } = await setup(`<a id="d" href="/export" brick:method="post" target="_blank" brick:disable-with="…">Export</a>`);
  click(w, $("#d"));
  await tick();
  assert.equal(submitted[0].target, "_blank");
  assert.equal($("#d").textContent, "Export", "the page stays, so the link is idle again at once");
}

// 8. A verb filled at render time: empty is an ordinary link; an unknown
// one is refused with an error and sends nothing, not even the GET.
{
  const { w, $, submitted, errors } = await setup(`<a id="e" href="/a" brick:method="">e</a><a id="g" href="/a" brick:method="get">g</a>`);
  assert.equal(click(w, $("#e")).defaultPrevented, false);
  assert.equal(click(w, $("#g")).defaultPrevented, true);
  await tick();
  assert.equal(submitted.length, 0);
  assert.equal(errors.length, 1);
  assert.match(errors[0], /brick:method="get" is not one of post, put, patch, delete/);
}

// 9. With nav.js: a method link is never fetched as a GET navigation, and
// brick:prefetch skips it (focus, press and visible), while a plain link with
// brick:prefetch still prefetches.
{
  const { w, $, submitted, fetched } = await setup(`<a id="d" href="/posts/7" brick:method="delete" brick:prefetch>Delete</a><a id="v" href="/posts/8" brick:method="delete" brick:prefetch="visible">v</a><a id="plain" href="/posts/9" brick:prefetch>plain</a>`, { nav: true });
  assert.ok(w.brick.navigate, "nav.js loaded");
  $("#d").dispatchEvent(new w.Event("focusin", { bubbles: true }));
  $("#d").dispatchEvent(new w.Event("pointerdown", { bubbles: true }));
  await tick();
  click(w, $("#d"));
  await tick();
  assert.equal(submitted.length, 1);
  assert.deepEqual(fetched, []);
  $("#plain").dispatchEvent(new w.Event("focusin", { bubbles: true }));
  await tick();
  assert.deepEqual(fetched, ["https://site.test/posts/9"]);
}

console.log("v23 ok");
}

main().catch((err) => { console.error(err); process.exit(1); });
