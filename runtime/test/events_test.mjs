// Tests runtime.js's v14 events in jsdom: brick:input, brick:focus,
// brick:blur and brick:keyup, key combos (.ctrl .meta .shift .alt) on
// keydown and keyup, and brick:click.outside.
import { readFileSync } from "node:fs";
import assert from "node:assert/strict";
import { JSDOM, VirtualConsole } from "jsdom";
import { tick, click, fill } from "./dom.mjs";

const runtimeJS = readFileSync(new URL("../runtime.js", import.meta.url), "utf8");
const ENVELOPE = "application/vnd.brick.action+json";
const windows = [];

const stateful = (inner) => `<div data-brick="c" data-brick-state="H" data-brick-token="T">${inner}</div>`;

// setup loads inner inside one stateful root; every action answers with
// the same markup, so a morph keeps the nodes. calls lists the actions
// sent as "name(args)".
function setup(inner, outside = "") {
  const page = stateful(inner) + outside;
  const dom = new JSDOM(`<!doctype html><body>${page}</body>`, { url: "https://site.test/", runScripts: "outside-only", virtualConsole: new VirtualConsole() });
  const w = dom.window;
  windows.push(w);
  const calls = [];
  w.fetch = (url, init = {}) => {
    const body = JSON.parse(init.body);
    calls.push(body.action + "(" + body.args.map((a) => JSON.stringify(a)).join(",") + ")");
    return Promise.resolve({ status: 200, ok: true, statusText: "OK", headers: { get: (h) => (h.toLowerCase() === "content-type" ? ENVELOPE : null) }, text: async () => JSON.stringify({ v: 1, version: 1, html: stateful(inner), effects: [] }) });
  };
  w.console.warn = () => {};
  w.eval(runtimeJS);
  return { w, doc: w.document, calls };
}

const key = (w, el, type, k, opts = {}) => {
  const ev = new w.KeyboardEvent(type, { key: k, bubbles: true, cancelable: true, ...opts });
  el.dispatchEvent(ev);
  return ev;
};
const focusEv = (w, el, type) => el.dispatchEvent(new w.FocusEvent(type));

async function main() {
// 1. brick:input runs on every edit with the control's value; a checkbox
// sends its checked state; .debounce batches a burst into one call.
{
  const { w, doc, calls } = setup(`<input id="q" brick:input="search"><input id="d" brick:input.debounce.30ms="later"><input id="cb" type="checkbox" brick:input="tick">`);
  await tick(); // the runtime binds the root after it loads
  const q = doc.getElementById("q");
  fill(w, q, "a");
  await tick();
  fill(w, q, "ab");
  await tick();
  assert.deepEqual(calls, ['search("a")', 'search("ab")'], "one call per edit, with the value");
  const d = doc.getElementById("d");
  fill(w, d, "x");
  fill(w, d, "xy");
  fill(w, d, "xyz");
  await tick(80);
  assert.deepEqual(calls.slice(2), ['later("xyz")'], ".debounce sends the last value once");
  const cb = doc.getElementById("cb");
  cb.checked = true;
  cb.dispatchEvent(new w.Event("input", { bubbles: true }));
  await tick();
  assert.equal(calls.at(-1), "tick(true)", "a checkbox sends checked");
  // A composing IME edit waits for compositionend.
  const n = calls.length;
  q.value = "に";
  q.dispatchEvent(new w.InputEvent("input", { bubbles: true, isComposing: true }));
  await tick();
  assert.equal(calls.length, n, "nothing while composing");
  q.dispatchEvent(new w.CompositionEvent("compositionend", { bubbles: true }));
  await tick();
  assert.equal(calls.at(-1), 'search("に")', "sent when the composition ends");
}

// 2. brick:focus and brick:blur run on the element itself (focus with no
// value, blur with the value), never for a descendant's focus.
{
  const { w, doc, calls } = setup(`<input id="n" value="Ada" brick:focus="started" brick:blur="check"><form id="f" brick:blur="never"><input id="inner"></form><div id="d" tabindex="0" brick:blur.once="left"></div>`);
  await tick(); // the runtime binds the root after it loads
  const n = doc.getElementById("n");
  focusEv(w, n, "focus");
  await tick();
  n.value = "Ada L";
  focusEv(w, n, "blur");
  await tick();
  assert.deepEqual(calls, ["started()", 'check("Ada L")']);
  focusEv(w, doc.getElementById("inner"), "blur");
  await tick();
  assert.equal(calls.length, 2, "a field's blur does not run its form's brick:blur");
  const d = doc.getElementById("d");
  focusEv(w, d, "blur");
  focusEv(w, d, "blur");
  await tick();
  assert.deepEqual(calls.slice(2), ["left()"], "a non-control sends no value; .once runs once");
  // The real focus() path fires the same listeners.
  n.focus();
  await tick();
  assert.equal(calls.at(-1), "started()", "element.focus() runs brick:focus");
}

// 3. Key combos match only when exactly their modifier keys are held; a
// plain key no longer fires with a modifier held; letters match by key
// or by physical code.
{
  const { w, doc, calls } = setup(`<textarea id="t" brick:keydown.enter="send" brick:keydown.ctrl.enter="sendAll" brick:keydown.meta.k="palette" brick:keydown.shift.alt.n="newer"></textarea>`);
  await tick(); // the runtime binds the root after it loads
  const t = doc.getElementById("t");
  key(w, t, "keydown", "Enter");
  key(w, t, "keydown", "Enter", { shiftKey: true });
  key(w, t, "keydown", "Enter", { ctrlKey: true });
  key(w, t, "keydown", "Enter", { ctrlKey: true, shiftKey: true });
  key(w, t, "keydown", "k", { metaKey: true });
  key(w, t, "keydown", "k", { ctrlKey: true });
  key(w, t, "keydown", "K", { metaKey: true, shiftKey: true });
  // alt+shift+n on a Mac gives e.key "˜"; the physical key is KeyN.
  key(w, t, "keydown", "˜", { altKey: true, shiftKey: true, code: "KeyN" });
  await tick();
  assert.deepEqual(calls, ["send()", "sendAll()", "palette()", "newer()"], "exact modifier match");
  // A combo that matched prevents the key's default; an unmatched one does not.
  assert.equal(key(w, t, "keydown", "k", { metaKey: true }).defaultPrevented, true);
  assert.equal(key(w, t, "keydown", "Enter", { shiftKey: true }).defaultPrevented, false, "shift+enter types a newline");
}

// 4. brick:keyup runs on release with the same key rules, never prevents
// the default, and has a .window form.
{
  const { w, doc, calls } = setup(`<input id="i" brick:keyup.escape="clear" brick:keyup="any"><span brick:keyup.ctrl.s.window="save"></span>`, `<input id="other">`);
  await tick(); // the runtime binds the root after it loads
  const i = doc.getElementById("i");
  const ev = key(w, i, "keyup", "Escape");
  key(w, i, "keyup", "a");
  key(w, i, "keydown", "Escape");
  await tick();
  assert.deepEqual(calls, ["clear()", "any()"], "keyup only on release; the innermost matching attribute");
  assert.equal(ev.defaultPrevented, false, "keyup prevents nothing");
  key(w, doc.getElementById("other"), "keyup", "s", { ctrlKey: true });
  key(w, doc.getElementById("other"), "keyup", "s");
  await tick();
  assert.deepEqual(calls.slice(2), ["save()"], ".window combo from anywhere on the page");
}

// 5. brick:click.outside runs for a click outside the element, never for
// one inside it, never while it is hidden, and never prevents the
// click's default; a plain brick:click on the same element still runs
// for a click inside.
{
  const { w, doc, calls } = setup(`<div id="menu" brick:click.outside="close" brick:click="inside"><button id="item">x</button></div><div id="pop" hidden brick:click.outside="never"></div><a id="link" href="#x">elsewhere</a>`, `<p id="page">page</p>`);
  await tick(); // the runtime binds the root after it loads
  click(w, doc.getElementById("item"));
  // Long enough for a queued second action to be sent after the first.
  await tick(100);
  assert.deepEqual(calls, ["inside()"], "a click inside runs brick:click, not .outside");
  const ev = click(w, doc.getElementById("link"));
  await tick();
  assert.deepEqual(calls.slice(1), ["close()"], "a click elsewhere in the component");
  assert.equal(ev.defaultPrevented, false, "the link keeps its default");
  click(w, doc.getElementById("page"));
  await tick();
  assert.deepEqual(calls.slice(2), ["close()"], "a click outside the component; the hidden one never runs");
  doc.getElementById("menu").style.display = "none";
  click(w, doc.getElementById("page"));
  await tick();
  assert.equal(calls.length, 3, "display:none counts as hidden");
}

// 6. .outside with .once, and registration after a morph re-binds.
{
  const { w, doc, calls } = setup(`<div id="m" brick:click.outside.once="close"></div><button id="b" brick:click="other">b</button>`);
  await tick(); // the runtime binds the root after it loads
  click(w, doc.getElementById("b"));
  await tick();
  click(w, doc.getElementById("b"));
  await tick();
  // Both run on the one root's queue, which orders them by its own rules.
  assert.deepEqual([...calls].sort(), ["close()", "other()", "other()"], ".once: the second outside click does nothing");
}
}

try {
  await main();
  console.log("events ok");
} catch (err) {
  console.error(err);
  process.exitCode = 1;
} finally {
  windows.forEach((w) => w.close());
}
