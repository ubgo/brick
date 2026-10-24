// Tests runtime.js's v22 client features in jsdom: a modelable child's
// change goes up as a $set of the bound parent field (modelable.go), an
// action's transition effect chooses or skips the view transition,
// brick:keydown.norepeat skips auto-repeats, and brick:click.details
// passes the click. headless Chrome covers the same in v22_chrome.mjs.
import { readFileSync } from "node:fs";
import assert from "node:assert/strict";
import { JSDOM, VirtualConsole } from "jsdom";
import { tick, click, key } from "./dom.mjs";

const runtimeJS = readFileSync(new URL("../runtime.js", import.meta.url), "utf8");
const ENVELOPE = "application/vnd.brick.action+json";

// setup loads html with the runtime; answer(body) returns {html, effects}
// for each action request, and sent records {handle, action, args}.
// startViewTransition is a stub that records its types (null for the
// callback form) and runs the update.
async function setup(html, answer = () => ({ html: "", effects: [] })) {
  const dom = new JSDOM(`<!doctype html><body>${html}</body>`, { url: "https://site.test/", runScripts: "outside-only", pretendToBeVisual: true, virtualConsole: new VirtualConsole() });
  const w = dom.window;
  const sent = [];
  const errors = [];
  const transitions = [];
  w.fetch = (url, init = {}) => {
    const body = JSON.parse(init.body);
    sent.push({ handle: body.handle, action: body.action, args: body.args });
    const a = answer(body);
    return Promise.resolve({ status: 200, ok: true, statusText: "OK", headers: { get: () => ENVELOPE }, text: async () => JSON.stringify({ v: 1, html: a.html, effects: a.effects || [] }) });
  };
  w.document.startViewTransition = (arg) => {
    const update = typeof arg === "function" ? arg : arg.update;
    transitions.push(typeof arg === "function" ? null : [...arg.types]); // a node array: the page's is another realm's
    update();
    const done = Promise.resolve();
    return { updateCallbackDone: done, finished: done };
  };
  w.matchMedia = () => ({ matches: false });
  w.console.error = (e) => errors.push(String(e && e.message || e));
  w.console.warn = () => {};
  w.eval(runtimeJS);
  w.brick.on("action-error", (d) => errors.push(d.message));
  await tick();
  return { w, sent, errors, transitions, $: (s) => w.document.querySelector(s) };
}

async function main() {
// 3. Modelable: the child's own answer changes its model value, so the
// runtime sends the parent a $set of the bound field; the bound field
// survives the child's re-render (its markup does not carry it). An
// answer that leaves the value alone sends nothing more.
{
  const child = (v) => `<div id="kid" data-brick="k1" data-brick-name="Stepper" data-brick-state="K" data-brick-token="T" data-brick-model-value="${v}"><button id="inc" brick:click="inc">+</button><b>${v}</b></div>`;
  let value = 2;
  const { $, sent, errors } = await setup(
    `<section data-brick="p1" data-brick-name="Cart" data-brick-state="P" data-brick-token="T">${child(2).replace('data-brick-model-value', 'data-brick-model="qty" data-brick-model-value')}</section>`,
    (body) => {
      if (body.handle === "K" && body.action === "inc") value++;
      if (body.handle === "K") return { html: child(value) };
      return { html: `<section data-brick="p1" data-brick-name="Cart" data-brick-state="P" data-brick-token="T"><brick-keep data-brick="k1"></brick-keep></section>` };
    });
  click($("#inc").ownerDocument.defaultView, $("#inc"));
  await tick(60);
  assert.deepEqual(sent.map((s) => [s.handle, s.action, s.args]), [["K", "inc", []], ["P", "$set", ["qty", 3]]]);
  assert.equal($("#kid").getAttribute("data-brick-model"), "qty", "the bound field is kept across the child's own render");
  sent.length = 0;
  await $("#kid").ownerDocument.defaultView.brick.action($("#kid"), "$refresh");
  await tick(60);
  assert.deepEqual(sent.map((s) => s.action), ["$refresh"], "an unchanged model sends nothing up");
  assert.deepEqual(errors, []);
}

// An unbound modelable child (no data-brick-model) never writes a parent.
{
  const { w, $, sent } = await setup(
    `<section data-brick="p1" data-brick-state="P" data-brick-token="T"><div data-brick="k1" data-brick-state="K" data-brick-token="T" data-brick-model-value="1"><button id="inc" brick:click="inc">+</button></div></section>`,
    () => ({ html: `<div data-brick="k1" data-brick-state="K" data-brick-token="T" data-brick-model-value="2"><button id="inc" brick:click="inc">+</button></div>` }));
  click(w, $("#inc"));
  await tick(60);
  assert.deepEqual(sent.map((s) => s.action), ["inc"]);
}

// 8. Transition effects: a typed one wraps the morph with its types even
// on a root without brick:transition; "none" skips the root's opt-in; a
// quiet job (a poll) with no choice is not wrapped, with one it is.
{
  const box = (n, attrs = "") => `<div id="r" data-brick="c1" data-brick-state="H" data-brick-token="T"${attrs}><button id="b" brick:click="go">${n}</button></div>`;
  let effects = [];
  let n = 0;
  const { w, $, transitions } = await setup(box(0), () => ({ html: box(++n), effects }));
  effects = [{ kind: "transition", name: "slide back" }];
  click(w, $("#b"));
  await tick(60);
  assert.deepEqual(transitions, [["slide", "back"]]);
  effects = [{ kind: "transition", name: "" }];
  click(w, $("#b"));
  await tick(60);
  assert.deepEqual(transitions, [["slide", "back"], null], "an untyped choice takes the callback form");
  effects = [];
  click(w, $("#b"));
  await tick(60);
  assert.equal(transitions.length, 2, "no choice and no opt-in: no transition");
}
{
  const box = (n) => `<div id="r" data-brick="c1" data-brick-state="H" data-brick-token="T" brick:transition="fade"><button id="b" brick:click="go">${n}</button></div>`;
  let effects = [];
  let n = 0;
  const { w, $, transitions } = await setup(box(0), () => ({ html: box(++n), effects }));
  click(w, $("#b"));
  await tick(60);
  assert.deepEqual(transitions, [["fade"]], "the root's opt-in without a choice");
  effects = [{ kind: "transition", name: "none" }];
  click(w, $("#b"));
  await tick(60);
  assert.equal($("#b").textContent, "2", "the morph still happens");
  assert.deepEqual(transitions, [["fade"]], "none skips the opted-in root");
  effects = [{ kind: "transition", name: "slide" }];
  click(w, $("#b"));
  await tick(60);
  assert.deepEqual(transitions, [["fade"], ["slide"]], "the action's type replaces the root's");
}

// 12. .norepeat: a repeat keydown is matched (its default prevented) but
// does not fire; without .norepeat every repeat fires.
{
  const { w, $, sent } = await setup(`<div data-brick="c1" data-brick-state="H" data-brick-token="T"><input id="a" brick:keydown.norepeat.enter="once"><input id="b" brick:keydown.down="every"></div>`);
  key(w, $("#a"), "Enter");
  const ev = new w.KeyboardEvent("keydown", { key: "Enter", repeat: true, bubbles: true, cancelable: true });
  $("#a").dispatchEvent(ev);
  key(w, $("#a"), "Enter", { repeat: true });
  key(w, $("#b"), "ArrowDown");
  key(w, $("#b"), "ArrowDown", { repeat: true });
  key(w, $("#b"), "ArrowDown", { repeat: true });
  await tick(60);
  assert.equal(ev.defaultPrevented, true, "a skipped repeat still prevents the key's default");
  assert.deepEqual(sent.map((s) => s.action), ["once", "every", "every", "every"]);
}

// 13. brick:click.details passes the click as the last argument; a plain
// brick:click passes nothing.
{
  const { w, $, sent } = await setup(`<div data-brick="c1" data-brick-state="H" data-brick-token="T"><p id="pad" brick:click.details='where("pad")'>pad</p><p id="plain" brick:click="plain">p</p></div>`);
  click(w, $("#pad"), { shiftKey: true, altKey: true, clientX: 30, clientY: 40, detail: 2 });
  click(w, $("#plain"), { shiftKey: true });
  await tick(60);
  assert.deepEqual(sent.map((s) => s.args), [
    ["pad", { alt: true, ctrl: false, meta: false, shift: true, button: 0, count: 2, x: 30, y: 40, clientX: 30, clientY: 40 }],
    [],
  ]);
}

console.log("v22 ok");
}

main().catch((err) => { console.error(err); process.exit(1); });
