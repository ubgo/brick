// Tests the v8 batch A runtime in jsdom: morph keys
// and controls, view transitions on action morphs, viewport actions,
// hooks and watch, optimistic writes, refs, offline and the submit lock.
import { readFileSync } from "node:fs";
import assert from "node:assert/strict";
import { JSDOM, VirtualConsole } from "jsdom";
import { tick, click, fill } from "./dom.mjs";

const runtimeJS = readFileSync(new URL("../runtime.js", import.meta.url), "utf8");
const ENVELOPE = "application/vnd.brick.action+json";

// setup loads html with the runtime. respond(body) returns {html, status,
// hold}; a held answer waits for release(). opts.before(w) runs before the
// runtime (to stub browser APIs).
async function setup(html, respond = () => ({}), opts = {}) {
  const vc = new VirtualConsole();
  const warnings = [];
  vc.on("warn", (m) => warnings.push(String(m)));
  const dom = new JSDOM(`<!doctype html><body>${html}</body>`, { url: "https://site.test/p/", runScripts: "outside-only", pretendToBeVisual: true, virtualConsole: vc });
  const w = dom.window;
  const sent = [];
  const held = [];
  w.fetch = (url, init = {}) => {
    const body = init.body ? JSON.parse(init.body) : null;
    sent.push({ body, headers: init.headers });
    const r = respond(body) || {};
    const status = r.status || 200;
    const answer = { status, ok: status < 400, statusText: "x", headers: { get: () => ENVELOPE }, text: async () => JSON.stringify({ v: 1, html: r.html || "", effects: [] }) };
    if (r.hold) return new Promise((res) => held.push(() => res(answer)));
    return Promise.resolve(answer);
  };
  if (opts.before) opts.before(w);
  w.eval(runtimeJS);
  const events = [];
  for (const n of ["action-error", "action-cancelled"]) w.brick.on(n, (d) => events.push([n, d]));
  await tick(); // the runtime binds the page on DOMContentLoaded
  return { w, doc: w.document, $: (id) => w.document.getElementById(id), sent, events, warnings, release: () => held.shift()() };
}

// ---- 1 · keys, ignore, replace ------------------------------------------

// Inserting a keyed row at the top keeps every existing row node and the
// focused input's value; without keys the rows would be rewritten.
{
  const list = (ids) => `<ul id="l" data-brick="c1" data-brick-state="H" data-brick-token="T">${ids.map((i) => `<li brick:key="${i}"><input value="v${i}"></li>`).join("")}<button id="add" brick:click="add">+</button></ul>`;
  const { w, doc, $ } = await setup(list([2, 3]), () => ({ html: list([1, 2, 3]) }));
  const [r2, r3] = [...doc.querySelectorAll("li")];
  r3.querySelector("input").value = "typed";
  click(w, $("add"));
  await tick();
  const rows = [...doc.querySelectorAll("li")];
  assert.equal(rows.length, 3);
  assert.equal(rows[1], r2, "row 2 kept");
  assert.equal(rows[2], r3, "row 3 kept");
  assert.equal(rows[0].getAttribute("brick:key"), "1", "new row inserted at the top");
  assert.equal(r3.querySelector("input").value, "typed", "typed value stays in its row");
}

// A duplicate key warns once, naming it.
{
  const html = `<ul data-brick="c1" data-brick-state="H" data-brick-token="T"><li brick:key="a">1</li><li brick:key="a">2</li><button id="go" brick:click="x">x</button></ul>`;
  const { w, $, warnings } = await setup(html, () => ({ html }));
  click(w, $("go"));
  await tick();
  click(w, $("go"));
  await tick();
  assert.equal(warnings.filter((m) => m.includes('duplicate brick:key "a"')).length, 1, "one warning");
}

// brick:ignore keeps the element whole; .self keeps its attributes and
// patches its children; brick:replace yields a new node.
{
  const v = (n) => `<div data-brick="c1" data-brick-state="H${n}" data-brick-token="T"><div id="ig" brick:ignore class="c${n}"><b>${n}</b></div><div id="is" brick:ignore.self class="c${n}"><b>${n}</b></div><div id="rp" brick:replace class="c${n}"><b>${n}</b></div><div id="plain" class="c${n}">${n}</div><button id="go" brick:click="x">x</button></div>`;
  const { w, $ } = await setup(v(1), () => ({ html: v(2) }));
  $("ig").firstChild.textContent = "client";
  const rp = $("rp");
  click(w, $("go"));
  await tick();
  assert.equal($("ig").className, "c1", "ignore keeps attributes");
  assert.equal($("ig").textContent, "client", "ignore keeps children");
  assert.equal($("is").className, "c1", "ignore.self keeps attributes");
  assert.equal($("is").textContent, "2", "ignore.self patches children");
  assert.notEqual($("rp"), rp, "replace swaps the node");
  assert.equal(rp.isConnected, false, "old node removed");
  assert.equal($("rp").className, "c2");
  assert.equal($("plain").className, "c2", "others still morph");
}

// ---- 2 · transitions -------------------------------------------------------

{
  const calls = [];
  const before = (w) => {
    w.document.startViewTransition = (arg) => {
      calls.push(arg);
      const update = typeof arg === "function" ? arg : arg.update;
      const done = Promise.resolve().then(update);
      return { updateCallbackDone: done, finished: done };
    };
    w.matchMedia = () => ({ matches: false });
  };
  const v = (n) => `<section data-brick="c1" data-brick-state="H" data-brick-token="T" brick:transition="slide fade"><i id="n">${n}</i><button id="go" brick:click="next">x</button><input id="f" brick:model.lazy="q" value=""></section>`;
  let n = 0;
  const { w, $ } = await setup(v(0), () => ({ html: v(++n) }), { before });
  click(w, $("go"));
  await tick();
  assert.equal(calls.length, 1, "a click action transitions");
  assert.equal(JSON.stringify(calls[0].types), JSON.stringify(["slide", "fade"]), "with the types");
  assert.equal($("n").textContent, "1", "morph applied");
  fill(w, $("f"), "x", "change");
  await tick();
  assert.equal(calls.length, 1, "a $set does not transition");
  assert.equal($("n").textContent, "2", "but still morphs");
}
{
  // Reduced motion and a missing API: the morph runs plainly.
  const calls = [];
  const v = (n) => `<section data-brick="c1" data-brick-state="H" data-brick-token="T" brick:transition><i id="n">${n}</i><button id="go" brick:click="next">x</button></section>`;
  let n = 0;
  const { w, $ } = await setup(v(0), () => ({ html: v(++n) }), { before: (w) => { w.document.startViewTransition = (f) => { calls.push(f); f(); return { updateCallbackDone: Promise.resolve(), finished: Promise.resolve() }; }; w.matchMedia = () => ({ matches: true }); } });
  click(w, $("go"));
  await tick();
  assert.equal(calls.length, 0, "reduced motion skips it");
  assert.equal($("n").textContent, "1");
  const b = await setup(v(0), () => ({ html: v(5) }));
  click(b.w, b.$("go"));
  await tick();
  assert.equal(b.$("n").textContent, "5", "no API: plain morph");
}

// ---- 3 · brick:intersect -----------------------------------------------------

function fakeIO(w) {
  const ios = [];
  w.IntersectionObserver = class {
    constructor(cb, opts) { this.cb = cb; this.opts = opts; this.els = new Set(); this.observed = 0; ios.push(this); }
    observe(el) { this.els.add(el); this.observed++; }
    unobserve(el) { this.els.delete(el); }
    disconnect() { this.els.clear(); }
    fire(on) { this.cb([...this.els].map((target) => ({ target, isIntersecting: on }))); }
  };
  return ios;
}
{
  let ios;
  const v = (n, more = true) => `<div data-brick="c1" data-brick-state="H" data-brick-token="T"><ul>${Array.from({ length: n }, (_, i) => `<li brick:key="${i}">${i}</li>`).join("")}</ul>${more ? `<div id="s" brick:intersect.margin.200px="more"></div>` : ""}</div>`;
  let n = 2;
  const { $, sent, doc, release } = await setup(v(2), () => ({ html: v((n += 2), n < 6), hold: true }), { before: (w) => { ios = fakeIO(w); } });
  assert.equal(ios.length, 1);
  assert.equal(ios[0].opts.rootMargin, "200px", ".margin.<px>");
  ios[0].fire(true);
  assert.equal(sent.length, 1, "fires on enter");
  assert.equal(sent[0].body.action, "more");
  assert.equal($("s").getAttribute("data-loading"), "", "loud: the trigger shows loading");
  ios[0].fire(true);
  assert.equal(sent.length, 1, "ignored while in flight");
  release();
  await tick();
  assert.equal(doc.querySelectorAll("li").length, 4);
  assert.ok(ios[0].observed >= 2, "re-observed after the morph");
  ios[0].fire(true);
  assert.equal(sent.length, 2, "re-fires while still visible");
  release();
  await tick();
  assert.equal($("s"), null, "the template's {{ if }} stops it");
}
{
  let ios;
  const html = `<div data-brick="c1" data-brick-state="H" data-brick-token="T"><p id="a" brick:intersect.leave="gone"></p><p id="b" brick:intersect.once.half="seen"></p></div>`;
  const { sent } = await setup(html, () => ({ html }), { before: (w) => { ios = fakeIO(w); } });
  const [leave, once] = ios;
  assert.equal(once.opts.threshold, 0.5, ".half");
  leave.fire(false);
  assert.equal(sent.length, 0, ".leave does not fire before it was seen");
  leave.fire(true);
  leave.fire(false);
  assert.equal(sent.length, 1, ".leave fires on leaving");
  once.fire(true);
  await tick();
  once.fire(true);
  await tick();
  assert.equal(sent.filter((s) => s.body.action === "seen").length, 1, ".once");
}

// ---- 4 · hooks and watch -----------------------------------------------------

{
  const html = `<div data-brick="c1" data-brick-state="H" data-brick-token="T"><button id="go" brick:click="save">x</button></div>`;
  const { w, $, sent, events } = await setup(html, () => ({ html }));
  const off = w.brick.hook("request", (r) => { assert.equal(r.action, "save"); assert.equal(r.trigger, $("go")); r.headers["X-Trace"] = "t1"; });
  click(w, $("go"));
  await tick();
  assert.equal(sent[0].headers["X-Trace"], "t1", "header added");
  assert.equal(sent[0].headers["Brick-Action"], "1", "protocol headers kept");
  off();
  w.brick.hook("request", (r) => { r.headers["Brick-Action"] = "2"; });
  click(w, $("go"));
  await tick();
  assert.equal(sent.length, 1, "a protected header change sends nothing");
  assert.match(events.at(-1)[1].message, /may not change the Brick-Action header/);
}
{
  const html = `<div data-brick="c1" data-brick-state="H" data-brick-token="T"><button id="go" brick:click="save">x</button></div>`;
  const { w, $, sent, events } = await setup(html, () => ({ html }));
  w.brick.hook("request", async (r) => { await null; r.cancel(); });
  click(w, $("go"));
  await tick();
  assert.equal(sent.length, 0, "cancelled: no request");
  assert.deepEqual(events.map((e) => e[0]), ["action-cancelled"]);
  assert.equal($("go").hasAttribute("data-loading"), false, "busy state cleared");
}
{
  // morph hook false keeps a node; morphed once per root; response sees
  // the envelope; a late ready hook runs at once.
  const v = (n) => `<div data-brick="c1" data-brick-state="H${n}" data-brick-token="T"><div id="ed" class="cm-editor">${n}</div><i id="n">${n}</i><button id="go" brick:click="x">x</button></div>`;
  const { w, $ } = await setup(v(1), () => ({ html: v(2) }));
  const seen = [];
  w.brick.hook("morph", ({ from }) => !from.classList.contains("cm-editor"));
  w.brick.hook("morphed", ({ root }) => seen.push(["morphed", root.getAttribute("data-brick")]));
  w.brick.hook("response", ({ status, envelope }) => seen.push(["response", status, envelope.v]));
  let readied = 0;
  w.brick.hook("ready", () => readied++);
  assert.equal(readied, 1, "ready registered late runs at once");
  click(w, $("go"));
  await tick();
  assert.equal($("ed").textContent, "1", "hooked node untouched");
  assert.equal($("n").textContent, "2", "siblings morphed");
  assert.deepEqual(seen, [["response", 200, 1], ["morphed", "c1"]]);
  assert.throws(() => w.brick.hook("nope", () => {}), /unknown hook/);
}
{
  // A morph hook refusing the root still updates its handle.
  const v = (n) => `<div id="r" data-brick="c1" data-brick-state="H${n}" data-brick-token="T${n}"><i id="n">${n}</i><button id="go" brick:click="x">x</button></div>`;
  const { w, $ } = await setup(v(1), () => ({ html: v(2) }));
  w.brick.hook("morph", ({ from }) => from.id !== "r");
  click(w, $("go"));
  await tick();
  assert.equal($("n").textContent, "1", "root left as is");
  assert.equal($("r").getAttribute("data-brick-state"), "H2", "handle updated");
  assert.equal($("r").getAttribute("data-brick-token"), "T2", "token updated");
}
{
  // watch: on input, on morph, never when unchanged.
  const v = (q) => `<div id="r" data-brick="c1" data-brick-state="H" data-brick-token="T"><input id="q" brick:model.lazy="q" value="${q}"><button id="go" brick:click="x">x</button></div>`;
  let next = "a";
  const { w, $ } = await setup(v(""), () => ({ html: v(next) }));
  const got = [];
  w.brick.watch($("q"), "q", (v2, old) => got.push([v2, old]));
  fill(w, $("q"), "a", "change");
  await tick();
  assert.deepEqual(got, [["a", ""]], "input change, and the morph echoing it is not a change");
  next = "server";
  click(w, $("go"));
  await tick();
  assert.deepEqual(got.at(-1), ["server", "a"], "a morph that changes it");
  assert.equal(got.length, 2);
}
{
  // The upload XHR carries a request hook's header.
  const html = `<div data-brick="c1" data-brick-state="H" data-brick-token="T" data-brick-uploads='{"doc":{"max_files":1}}'><input id="f" type="file" brick:upload="doc"></div>`;
  const xhrs = [];
  const { w, $ } = await setup(html, () => ({}), { before: (w) => {
    w.XMLHttpRequest = class {
      constructor() { this.headers = {}; this.upload = {}; xhrs.push(this); }
      open() {} setRequestHeader(k, v) { this.headers[k] = v; } getResponseHeader() { return ENVELOPE; }
      send() {}
    };
  } });
  w.brick.hook("request", (r) => { r.headers.Authorization = "Bearer x"; });
  w.brick.upload($("f"), "doc", [new w.File(["x"], "a.txt", { type: "text/plain" })]);
  await tick();
  assert.equal(xhrs[0].headers.Authorization, "Bearer x");
}

// ---- 6 · optimistic writes ---------------------------------------------------

{
  const v = (liked) => `<div data-brick="c1" data-brick-state="H" data-brick-token="T"><button id="b" brick:click.optimistic='$toggle("liked")'><span id="on" brick:show="liked" ${liked ? "" : "hidden"}>Liked</span><span id="off" brick:show="!liked" ${liked ? "hidden" : ""}>Like</span></button></div>`;
  let status = 200;
  const { w, $, sent, release, events } = await setup(v(false), () => ({ html: v(true), status, hold: true }));
  click(w, $("b"));
  assert.equal($("on").hasAttribute("hidden"), false, "flipped before the answer");
  assert.equal($("off").hasAttribute("hidden"), true);
  assert.deepEqual(sent[0].body.args, ["liked"], "the action is still sent, args unchanged");
  release();
  await tick();
  assert.equal($("on").hasAttribute("hidden"), false, "server agreed");
  status = 500;
  click(w, $("b"));
  assert.equal($("on").hasAttribute("hidden"), true, "flipped back optimistically");
  release();
  await tick();
  assert.equal($("on").hasAttribute("hidden"), false, "reverted on a 500");
  assert.equal(events.at(-1)[0], "action-error");
}
{
  // The morph decides on success, even when it disagrees.
  const v = (liked) => `<div data-brick="c1" data-brick-state="H" data-brick-token="T"><button id="b" brick:click.optimistic='$set("liked", true)'><span id="on" brick:show="liked" ${liked ? "" : "hidden"}>Liked</span></button></div>`;
  const { w, $ } = await setup(v(false), () => ({ html: v(false) }));
  click(w, $("b"));
  assert.equal($("on").hasAttribute("hidden"), false, "optimistic");
  await tick();
  assert.equal($("on").hasAttribute("hidden"), true, "server's render wins");
}

// ---- 7 · ref, offline, submit lock ------------------------------------------

{
  const html = `<p brick:ref="top" id="pageref"></p><div id="r" data-brick="c1" data-brick-state="H" data-brick-token="T"><b brick:ref="out" id="o"></b><div data-brick="c2" data-brick-state="H2" data-brick-token="T2"><b brick:ref="out" id="inner"></b></div></div>`;
  const { w, $ } = await setup(html);
  assert.equal(w.brick.ref($("r"), "out"), $("o"), "own root's ref");
  assert.equal(w.brick.ref($("inner"), "out"), $("inner"), "child root's own");
  assert.equal(w.brick.ref(w.document.body, "top"), $("pageref"), "outside every root");
  assert.equal(w.brick.ref($("r"), "nope"), null);
}
{
  let online = false;
  const html = `<p id="a" brick:offline hidden>offline</p><p id="b" brick:offline.remove>online</p>`;
  const { w, $, doc } = await setup(html, () => ({}), { before: (w) => Object.defineProperty(w.navigator, "onLine", { get: () => online, configurable: true }) });
  assert.equal($("a").hasAttribute("hidden"), false, "shown offline");
  assert.equal($("b").hasAttribute("hidden"), true, ".remove hidden offline");
  assert.equal(doc.documentElement.hasAttribute("data-offline"), true);
  online = true;
  w.dispatchEvent(new w.Event("online"));
  assert.equal($("a").hasAttribute("hidden"), true);
  assert.equal($("b").hasAttribute("hidden"), false);
  assert.equal(doc.documentElement.hasAttribute("data-offline"), false);
}
{
  const html = `<div data-brick="c1" data-brick-state="H" data-brick-token="T"><form id="f" brick:submit.lock="save"><input id="t" name="t" value="x"><textarea id="ta" name="ta"></textarea><input id="c" type="checkbox" name="c"><select id="s" name="s"><option>a</option></select><button id="go">go</button></form></div>`;
  const { w, $, release } = await setup(html, () => ({ html, hold: true }));
  $("f").dispatchEvent(new w.Event("submit", { bubbles: true, cancelable: true }));
  assert.equal($("t").readOnly, true, "text locked");
  assert.equal($("ta").readOnly, true);
  assert.equal($("c").getAttribute("aria-disabled"), "true", "choice marked");
  assert.equal($("c").disabled, false, "never disabled");
  click(w, $("c"));
  assert.equal($("c").checked, false, "click guard");
  release();
  await tick();
  assert.equal($("t").readOnly, false, "unlocked");
  assert.equal($("c").hasAttribute("aria-disabled"), false);
}
{
  // Without .lock a form keeps today's behaviour: buttons only.
  const html = `<div data-brick="c1" data-brick-state="H" data-brick-token="T"><form id="f" brick:submit="save"><input id="t" name="t"></form></div>`;
  const { w, $, release } = await setup(html, () => ({ html, hold: true }));
  $("f").dispatchEvent(new w.Event("submit", { bubbles: true, cancelable: true }));
  assert.equal($("t").readOnly, false);
  release();
  await tick();
}

console.log("v8 ok");
