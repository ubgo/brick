// Checks runtime.js's v22 runtime features in a real, headless Chrome
// against the kit showcase: a modelable child bound to its parent's field
// (brick:model on a component call, both directions), an action's own
// transition effect (brick.transition), brick:keydown.norepeat on a held
// key, and brick:click.details.
// Usage: go run ./ui -serve :8062 (in examples, with BRICK_SECRET set),
//        then node v22_chrome.mjs http://127.0.0.1:8062
// The demos are examples/ui/demos/V22*.html on /runtime-extras/.
import assert from "node:assert/strict";
import { openChrome, input, sleep, MODIFIERS } from "./cdp.mjs";

const base = (process.argv[2] || "http://127.0.0.1:8062").replace(/\/$/, "");
// REPEATS is how many auto-repeat keydowns a held key sends in the test.
const REPEATS = 3;
// SETTLE_MS outlasts a queued action's round trip, so a count that must
// not change is read after anything that could change it has landed.
const SETTLE_MS = 600;

const cdp = await openChrome({ profilePrefix: "brick-v22-" });
const { send, evaluate, until } = cdp;
const person = input(cdp);
const errors = [];
cdp.on((m, p) => { if (m === "Runtime.exceptionThrown") errors.push(p.exceptionDetails.exception?.description); });
const ok = (name) => console.log("ok  " + name);
const js = JSON.stringify;
const text = (sel) => evaluate(`document.querySelector(${js(sel)}).textContent.trim()`);
const waitText = (sel, want) => until(`document.querySelector(${js(sel)}) && document.querySelector(${js(sel)}).textContent.includes(${js(want)})`, `${sel} to show ${want}`);

// hold presses a key, auto-repeats it REPEATS times, and releases it, as a
// held key does: one keydown, then keydowns with repeat set, then keyup.
async function hold(sel, key, code, keyCode, text) {
  await evaluate(`document.querySelector(${js(sel)}).focus()`);
  const common = { key, code, windowsVirtualKeyCode: keyCode, nativeVirtualKeyCode: keyCode };
  const type = text ? "keyDown" : "rawKeyDown";
  await send("Input.dispatchKeyEvent", { type, ...common, ...(text ? { text } : {}) });
  for (let i = 0; i < REPEATS; i++) await send("Input.dispatchKeyEvent", { type, autoRepeat: true, ...common, ...(text ? { text } : {}) });
  await send("Input.dispatchKeyEvent", { type: "keyUp", ...common });
}

try {
  await send("Runtime.enable");
  await send("Page.enable");
  const load = cdp.once("Page.loadEventFired");
  await send("Page.navigate", { url: base + "/runtime-extras/" });
  await load;
  await until("window.brick && document.querySelector('#v22-value')", "the page");
  await sleep(200);

  // 3 Modelable: the stepper starts from the cart's qty; its own action
  // moves the cart's total (up); the cart's $set comes back down.
  assert.equal(await text("#v22-value"), "2");
  assert.match(await text("#v22-total"), /\$24/);
  await person.click("#v22-inc");
  await waitText("#v22-value", "3");
  await waitText("#v22-total", "$36");
  await person.click("#v22-inc");
  await waitText("#v22-total", "$48");
  assert.equal(await text("#v22-value"), "4");
  await person.click("#v22-reset");
  await waitText("#v22-total", "$12");
  await waitText("#v22-value", "1");
  await person.click("#v22-dec");
  await waitText("#v22-total", "$0");
  assert.equal(await text("#v22-value"), "0");
  ok("3 modelable child: up via the child's action, down via the parent's $set");

  // 8 Server transition control: spy on startViewTransition. shuffle
  // records brick.transition("slide"): one transition with that type.
  // edit records "none": the morph runs without one, though the root opts in.
  await evaluate(`(() => {
    window.__vt = [];
    window.__vtIdle = true;
    const real = document.startViewTransition.bind(document);
    document.startViewTransition = (arg) => {
      window.__vt.push(typeof arg === "function" ? null : arg.types);
      window.__vtIdle = false;
      const t = real(arg);
      t.finished.finally(() => { window.__vtIdle = true; });
      return t;
    };
  })()`);
  await person.click("#v22-shuffle");
  await waitText("#v22-note", "rotated with a slide");
  assert.deepEqual(await evaluate("window.__vt"), [["slide"]]);
  assert.equal(await text("#v22-items li"), "Bravo");
  // A running view transition covers the page with its pseudo-elements,
  // so a click would land on <html>: wait for the slide to finish.
  await until("window.__vtIdle", "the slide transition to finish");
  await person.click("#v22-edit");
  await waitText("#v22-note", "noted without a transition");
  await sleep(SETTLE_MS);
  assert.deepEqual(await evaluate("window.__vt"), [["slide"]]);
  ok("8 an action's transition effect: a typed one, and none on an opted-in root");

  // 12 .norepeat: Enter held in the first field runs once; the down arrow
  // held in the second runs on every repeat (the default).
  await hold("#v22-norepeat", "Enter", "Enter", 13, "\r");
  await waitText("#v22-once", "1");
  await sleep(SETTLE_MS);
  assert.equal(await text("#v22-once"), "1");
  await hold("#v22-repeat", "ArrowDown", "ArrowDown", 40);
  await waitText("#v22-every", String(REPEATS + 1));
  await sleep(SETTLE_MS);
  assert.equal(await text("#v22-every"), String(REPEATS + 1));
  ok("12 .norepeat fires once per held key; the default repeats");

  // 13 click details: a shift-click 10px right and 20px down of the pad's
  // corner reaches the action as its last argument.
  const at = await evaluate(`(() => { const e = document.querySelector("#v22-pad"); e.scrollIntoView({block: "center"}); const r = e.getBoundingClientRect(); return {x: Math.round(r.left) + 10, y: Math.round(r.top) + 20}; })()`);
  const mouse = { x: at.x, y: at.y, button: "left", clickCount: 1, modifiers: MODIFIERS.shift };
  await send("Input.dispatchMouseEvent", { type: "mouseMoved", x: at.x, y: at.y });
  await send("Input.dispatchMouseEvent", { type: "mousePressed", ...mouse });
  await send("Input.dispatchMouseEvent", { type: "mouseReleased", ...mouse });
  await waitText("#v22-details", "x=10");
  assert.equal(await text("#v22-details"), "x=10 y=20 shift=true alt=false ctrl=false meta=false count=1");
  ok("13 brick:click.details passes keys, count and position");

  assert.deepEqual(errors, []);
  console.log("all v22 runtime checks passed");
} finally {
  await cdp.close();
}
