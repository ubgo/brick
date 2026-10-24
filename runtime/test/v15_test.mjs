// Tests runtime.js's v15 viewport and poll modifiers in jsdom with a fake
// clock: brick:intersect.dwell[.<n>ms] (fires only after the element
// stayed in view that long; leaving or hiding the tab cancels it) and
// brick:poll.keep-alive (keeps polling while the tab is hidden).
import { readFileSync } from "node:fs";
import assert from "node:assert/strict";
import { JSDOM, VirtualConsole } from "jsdom";
import { tick } from "./dom.mjs";

const runtimeJS = readFileSync(new URL("../runtime.js", import.meta.url), "utf8");
const ENVELOPE = "application/vnd.brick.action+json";
// DWELL_MS is the runtime's default .dwell wait (grammar.go DwellDefaultMS).
const DWELL_MS = 1000;
// POLL_MS is the shortest poll the runtime runs (its POLL_MIN_MS).
const POLL_MS = 1000;

// fakeClock replaces the window's timers before the runtime loads, so a
// test moves time with advance(ms) instead of waiting for it.
function fakeClock(w) {
  let now = 0;
  let seq = 0;
  const timers = new Map(); // id -> {at, fn, every}
  w.setTimeout = (fn, ms = 0) => { timers.set(++seq, { at: now + ms, fn, every: 0 }); return seq; };
  w.setInterval = (fn, ms = 0) => { timers.set(++seq, { at: now + ms, fn, every: ms }); return seq; };
  w.clearTimeout = w.clearInterval = (id) => { timers.delete(id); };
  return {
    advance(ms) {
      const end = now + ms;
      for (;;) {
        let next = null;
        for (const [id, t] of timers) if (t.at <= end && (!next || t.at < next[1].at)) next = [id, t];
        if (!next) break;
        const [id, t] = next;
        now = t.at;
        if (t.every) t.at += Math.max(1, t.every); else timers.delete(id);
        t.fn();
      }
      now = end;
    },
  };
}

// fakeIO records every IntersectionObserver; fire(on) reports its
// elements as entering (true) or leaving (false) the viewport.
function fakeIO(w) {
  const ios = [];
  w.IntersectionObserver = class {
    constructor(cb, opts) { this.cb = cb; this.opts = opts || {}; this.els = new Set(); ios.push(this); }
    observe(el) { this.els.add(el); }
    unobserve(el) { this.els.delete(el); }
    disconnect() { this.els.clear(); }
    fire(on) { this.cb([...this.els].map((target) => ({ target, isIntersecting: on }))); }
  };
  return ios;
}

// setup loads html with the runtime, a fake clock, a fake observer and a
// document whose hidden state the test sets with hide(bool).
async function setup(html) {
  const dom = new JSDOM(`<!doctype html><body>${html}</body>`, { url: "https://site.test/", runScripts: "outside-only", pretendToBeVisual: true, virtualConsole: new VirtualConsole() });
  const w = dom.window;
  const sent = [];
  w.fetch = (url, init = {}) => {
    sent.push(JSON.parse(init.body).action);
    return Promise.resolve({ status: 200, ok: true, statusText: "OK", headers: { get: () => ENVELOPE }, text: async () => JSON.stringify({ v: 1, html, effects: [] }) });
  };
  let hidden = false;
  Object.defineProperty(w.document, "hidden", { configurable: true, get: () => hidden });
  const clock = fakeClock(w);
  const ios = fakeIO(w);
  w.eval(runtimeJS);
  await tick(); // the runtime binds the page on DOMContentLoaded
  const hide = (on) => { hidden = on; w.document.dispatchEvent(new w.Event("visibilitychange")); };
  // later moves the clock, then lets the sends it caused settle.
  const later = async (ms) => { clock.advance(ms); await tick(); };
  return { w, sent, ios, hide, later };
}

const root = (inner) => `<div data-brick="c1" data-brick-state="H" data-brick-token="T">${inner}</div>`;

async function main() {
// 1. .dwell fires only after a continuous stay of DWELL_MS, once per stay;
// a new stay after leaving counts again.
{
  const { sent, ios, later } = await setup(root(`<p brick:intersect.dwell="imp"></p>`));
  const [io] = ios;
  io.fire(true);
  await later(DWELL_MS - 1);
  assert.deepEqual(sent, [], "not before the dwell");
  await later(1);
  assert.deepEqual(sent, ["imp"], "fires when the dwell completes");
  io.fire(true); // a re-observe while it never left
  await later(DWELL_MS * 5);
  assert.deepEqual(sent, ["imp"], "once per stay");
  io.fire(false);
  io.fire(true);
  await later(DWELL_MS);
  assert.deepEqual(sent, ["imp", "imp"], "a new stay counts again");
}

// 2. Leaving before the dwell completes cancels it; the next stay starts
// the full wait again rather than resuming.
{
  const { sent, ios, later } = await setup(root(`<p brick:intersect.dwell.500ms="imp"></p>`));
  const [io] = ios;
  io.fire(true);
  await later(400);
  io.fire(false);
  await later(1000);
  assert.deepEqual(sent, [], "leaving cancels the dwell");
  io.fire(true);
  await later(400);
  assert.deepEqual(sent, [], "a new stay waits the full dwell again");
  await later(100);
  assert.deepEqual(sent, ["imp"]);
  // Leaving and coming straight back: the first stay's timer must not
  // fire into the second one.
  io.fire(false);
  io.fire(true);
  await later(400);
  io.fire(false);
  io.fire(true);
  await later(200);
  assert.deepEqual(sent, ["imp"], "the cancelled stay's timer does not fire");
  await later(300);
  assert.deepEqual(sent, ["imp", "imp"], "the new stay fires after its own full dwell");
}

// 3. It combines with .once, .half and .margin: the observer gets the
// threshold and margin, and .once fires on the first completed stay only.
{
  const { sent, ios, later } = await setup(root(`<p brick:intersect.dwell.300ms.once.half.margin.50px="hero"></p>`));
  const [io] = ios;
  assert.equal(io.opts.threshold, 0.5, ".half");
  assert.equal(io.opts.rootMargin, "50px", ".margin");
  io.fire(true);
  await later(300);
  io.fire(false);
  io.fire(true);
  await later(300);
  assert.deepEqual(sent, ["hero"], ".once");
}

// 4. Hiding the tab ends the stay; showing it again with the element
// still in view starts a new full dwell.
{
  const { sent, ios, hide, later } = await setup(root(`<p brick:intersect.dwell="imp"></p>`));
  const [io] = ios;
  io.fire(true);
  await later(DWELL_MS / 2);
  hide(true);
  await later(DWELL_MS * 2);
  assert.deepEqual(sent, [], "a hidden tab cancels the dwell");
  hide(false);
  await later(DWELL_MS - 1);
  assert.deepEqual(sent, [], "the stay restarts from zero");
  await later(1);
  assert.deepEqual(sent, ["imp"]);
  // A quick switch away and back: the stay before the switch does not
  // count toward the dwell.
  io.fire(false);
  io.fire(true);
  await later(DWELL_MS / 2);
  hide(true);
  hide(false);
  await later(DWELL_MS / 2);
  assert.deepEqual(sent, ["imp"], "the time before the hide is not counted");
  await later(DWELL_MS / 2);
  assert.deepEqual(sent, ["imp", "imp"]);
}

// 5. A poll skips its ticks while the tab is hidden; .keep-alive keeps
// polling then.
{
  const { sent, hide, later } = await setup(root(`<i brick:poll.1s="plain"></i><b brick:poll.1s.keep-alive="kept"></b>`));
  hide(true);
  await later(POLL_MS);
  assert.deepEqual(sent, ["kept"], "only .keep-alive polls in a hidden tab");
  hide(false);
  await later(POLL_MS);
  assert.deepEqual(sent.slice(1).sort(), ["kept", "plain"], "both poll when visible");
}

// 6. .keep-alive.visible: the tab may be hidden, but the element must be
// in view (as last reported) for the poll to run.
{
  const { sent, ios, hide, later } = await setup(root(`<b brick:poll.1s.keep-alive.visible="kept"></b>`));
  const [io] = ios;
  hide(true);
  await later(POLL_MS);
  assert.deepEqual(sent, [], "off screen: .visible still holds");
  io.fire(true);
  await later(POLL_MS);
  assert.deepEqual(sent, ["kept"], "in view, in a hidden tab");
}

console.log("v15 dwell and keep-alive ok");
}

main().then(() => process.exit(0), (err) => { console.error(err); process.exit(1); });
