// Checks runtime.js's v21 features in a real, headless Chrome against a
// served page: brick:focus/blur.window and brick:js
// focus/blur.window, brick:loading call / $set / .except targets, brick:show
// playing brick:leave and brick:enter, brick.js, brick.find / brick.all,
// valueless brick:disable-with, and the flash-clear effect.
// Usage: brick serve -addr 127.0.0.1:8041 runtime/test/v21site, then
//        node v21_chrome.mjs http://127.0.0.1:8041
// The fixture site v21site has / (V21, two Other components, Hook and a
// #jw brick:js element) and /toast (the kit Toaster beside V21). The brick
// binary must be built from this tree: it embeds runtime.js and the kit.
import assert from "node:assert/strict";
import { openChrome, sleep } from "./cdp.mjs";

const base = (process.argv[2] || "http://127.0.0.1:8041").replace(/\/$/, "");
// LATENCY_MS slows every action (brick.latency), so loading states can be
// read while a request is held.
const LATENCY_MS = 600;
// HALF_MS is a moment inside LATENCY_MS, when a held job is surely busy.
const HALF_MS = 250;
// FADE_MS is the fixture's .panel transition (300 ms); WAIT_FADE_MS outlasts it.
const WAIT_FADE_MS = 500;

const cdp = await openChrome({ profilePrefix: "brick-v21-" });
const { send, evaluate, until } = cdp;
const errors = [];
cdp.on((m, p) => { if (m === "Runtime.exceptionThrown") errors.push(p.exceptionDetails.exception?.description); });
const ok = (name) => console.log("ok  " + name);
const text = (sel) => evaluate(`document.querySelector(${JSON.stringify(sel)}).textContent`);
const hidden = (sel) => evaluate(`document.querySelector(${JSON.stringify(sel)}).hidden`);
const click = (sel) => evaluate(`document.querySelector(${JSON.stringify(sel)}).click()`);

try {
  await send("Runtime.enable");
  await send("Page.enable");
  const load = cdp.once("Page.loadEventFired");
  await send("Page.navigate", { url: base + "/" });
  await load;
  await until("window.brick && document.querySelector('#v21')", "the page");
  await sleep(200);

  // R6 brick.find / brick.all
  assert.equal(await evaluate(`brick.all("Other").length`), 2);
  assert.equal(await evaluate(`brick.all().length`), 3);
  assert.equal(await evaluate(`brick.find("V21").id`), "v21");
  assert.equal(await evaluate(`brick.find("Nope")`), null);
  await evaluate(`brick.action(brick.find("Other"), "inc")`);
  assert.equal(await evaluate(`brick.all("Other").map((r) => r.textContent.trim()).join(",")`), "n=1,n=0");
  ok("R6 brick.find / brick.all");

  // R5 brick.js from a mount hook and from code; a bad list throws.
  await until(`document.querySelector(".hook").classList.contains("mounted")`, "the mount hook's brick.js");
  assert.equal(await evaluate(`document.querySelector(".hook span").dataset.x`), "1");
  await evaluate(`brick.js(document.querySelector("#v21"), 'hide("#q"); addClass("this", "viajs")')`);
  assert.equal(await hidden("#q"), true);
  assert.equal(await evaluate(`document.querySelector("#v21").classList.contains("viajs")`), true);
  assert.match(await evaluate(`(() => { try { brick.js(document.body, 'explode("#x")'); return "no throw"; } catch (e) { return e.message; } })()`), /unknown command/);
  await evaluate(`brick.js(document.body, 'show("#q")')`);
  ok("R5 brick.js");

  // R1 window focus / blur: brick:focus.window and brick:js.blur.window.
  await evaluate(`window.dispatchEvent(new FocusEvent("blur"))`);
  await until(`document.querySelector("#blurred").textContent === "1"`, "brick:blur.window");
  assert.equal(await evaluate(`document.querySelector("#jw").classList.contains("wb")`), true);
  await evaluate(`window.dispatchEvent(new FocusEvent("focus"))`);
  await until(`document.querySelector("#focused").textContent === "1"`, "brick:focus.window");
  assert.equal(await evaluate(`document.querySelector("#jw").classList.contains("wb")`), false);
  // An element's own focus does not run the window attributes.
  await evaluate(`document.querySelector("#q").focus(); document.querySelector("#q").blur()`);
  await sleep(300);
  assert.equal(await text("#focused"), "1");
  assert.equal(await text("#blurred"), "1");
  ok("R1 .window on brick:focus / brick:blur (and brick:js)");

  // M6 + R3: a slow remove(2) lights row 2's spinner and #rm2 only; .except
  // is on for it; save's valueless disable-with keeps its label.
  await evaluate(`brick.latency(${LATENCY_MS})`);
  await click("#row-2 .rm");
  await sleep(HALF_MS);
  assert.equal(await hidden("#row-2 .spin"), false, "row 2 spinner on");
  assert.equal(await hidden("#row-1 .spin"), true, "row 1 spinner off");
  assert.equal(await hidden("#row-3 .spin"), true, "row 3 spinner off");
  assert.equal(await hidden("#rm2"), false, "remove(2) literal target on");
  assert.equal(await hidden("#except"), false, ".except on for remove");
  await until(`!document.querySelector("#row-2")`, "row 2 removed");
  await until(`document.querySelector("#rm2").hidden`, "rm2 off");
  await click("#dl");
  await sleep(HALF_MS);
  assert.equal(await hidden("#except"), true, ".except off for download");
  await until(`!document.querySelector("#v21").hasAttribute("aria-busy")`, "download done");
  await click("#save");
  await sleep(HALF_MS);
  assert.equal(await evaluate(`document.querySelector("#save").disabled`), true, "save disabled while busy");
  assert.equal(await text("#save"), "Save", "label kept");
  await until(`!document.querySelector("#save").disabled`, "save enabled again");
  assert.equal(await text("#save"), "Save");
  ok("M6 valueless brick:disable-with");
  // A typed value: $set("query") lights while its $set is held.
  await evaluate(`(() => { const q = document.querySelector("#q"); q.focus(); q.value = "hi"; q.dispatchEvent(new Event("input", { bubbles: true })); })()`);
  await until(`!document.querySelector("#qload").hidden`, "$set(\"query\") on");
  assert.equal(await hidden("#row-1 .spin"), true);
  await until(`document.querySelector("#qload").hidden`, "$set(\"query\") off");
  ok("R3 loading targets: remove(N), $set(\"query\"), .except");
  await evaluate(`brick.latency(0)`);

  // R4 brick:show plays brick:leave on hide and brick:enter on show.
  await click("#open");
  await sleep(50);
  assert.equal(await evaluate(`document.querySelector("#panel").hasAttribute("data-brick-leaving")`), true, "leaving");
  assert.equal(await hidden("#panel"), false, "still shown while it leaves");
  await sleep(WAIT_FADE_MS);
  assert.equal(await hidden("#panel"), true, "hidden after the transition");
  assert.equal(await evaluate(`document.querySelector("#panel").classList.contains("fadeout")`), false, "leave classes restored");
  await click("#open");
  await sleep(50);
  assert.equal(await hidden("#panel"), false, "shown at once");
  assert.equal(await evaluate(`document.querySelector("#panel").hasAttribute("data-brick-entering")`), true, "entering");
  const mid = Number(await evaluate(`getComputedStyle(document.querySelector("#panel")).opacity`));
  assert.ok(mid < 1, "opacity mid-transition: " + mid);
  await sleep(WAIT_FADE_MS);
  assert.equal(await evaluate(`document.querySelector("#panel").hasAttribute("data-brick-entering")`), false, "entered");
  assert.equal(await evaluate(`getComputedStyle(document.querySelector("#panel")).opacity`), "1");
  ok("R4 brick:show plays brick:leave / brick:enter");

  // flash-clear: two flashes in the default display, clear one level, then all.
  await click("#notify");
  await until(`document.querySelectorAll("#brick-flash > div").length === 2`, "two flashes");
  await click("#clearwarn");
  await until(`document.querySelectorAll("#brick-flash > div").length === 1`, "warning cleared");
  assert.equal(await evaluate(`document.querySelector("#brick-flash > div").dataset.level`), "info");
  await click("#clearall");
  await until(`document.querySelectorAll("#brick-flash > div").length === 0`, "all cleared");
  ok("flash-clear (default display)");

  // flash-clear with the kit Toaster: it shows the flashes as toasts and
  // dismisses them, one variant or all.
  const load2 = cdp.once("Page.loadEventFired");
  await send("Page.navigate", { url: base + "/toast" });
  await load2;
  await until("window.brick && document.querySelector('.bk-toaster')", "the toaster page");
  await sleep(200);
  const live = `[...document.querySelectorAll(".bk-toaster__list .bk-toast:not(.bk-toast--leaving)")]`;
  await click("#notify");
  await until(`${live}.length === 2`, "two toasts");
  await click("#clearwarn");
  await until(`${live}.length === 1`, "warning toast dismissed");
  assert.equal(await evaluate(`${live}[0].classList.contains("bk-toast--info")`), true);
  await click("#clearall");
  await until(`${live}.length === 0`, "all toasts dismissed");
  assert.equal(await evaluate(`document.querySelectorAll("#brick-flash > div").length`), 0, "default display stays off");
  ok("flash-clear (kit Toaster)");

  assert.deepEqual(errors, []);
  console.log("v21 chrome ok");
} finally {
  await cdp.close();
}
