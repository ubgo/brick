// Drives the kit's Resizable in jsdom (see kit_overlays2_test.go): the
// handle's keys move the split within both panels' bounds, a drag moves it
// by the pointer's share of the group, and persist restores the sizes.
import assert from "node:assert/strict";
import { load, tick, key } from "./kit_harness.mjs";
import { loadESM, pointer, rect } from "./kit_esm.mjs";

function start(saved) {
  const h = load();
  if (saved) h.w.localStorage.setItem("bk-resizable:split", saved);
  loadESM(h.w, "Resizable");
  return h;
}
const { w, doc } = start();
await tick();
const group = doc.getElementById("split");
const [a, b] = group.querySelectorAll(".bk-resizable__panel");
const handle = group.querySelector(".bk-resizable__handle");
const now = () => Number(handle.getAttribute("aria-valuenow"));

assert.equal(handle.getAttribute("role"), "separator");
assert.equal(handle.tabIndex, 0, "focusable once enhanced");
assert.equal(now(), 30);
key(w, handle, "ArrowRight");
assert.equal(now(), 35, "ArrowRight grows the first panel by step");
assert.equal(a.style.flexGrow, "35");
assert.equal(b.style.flexGrow, "65");
key(w, handle, "ArrowLeft");
key(w, handle, "ArrowLeft");
assert.equal(now(), 25);
key(w, handle, "End");
assert.equal(now(), 60, "End stops at the panel's max");
key(w, handle, "Home");
assert.equal(now(), 20, "Home stops at its min");
assert.equal(w.localStorage.getItem("bk-resizable:split"), "[20,80]", "persisted");

// A drag of 100px in a 1000px group is 10%.
rect(a, { width: 200 });
rect(b, { width: 800 });
pointer(w, handle, "pointerdown", { x: 200 });
pointer(w, handle, "pointermove", { x: 300 });
assert.equal(now(), 30, "the drag follows the pointer");
pointer(w, handle, "pointermove", { x: 5000 });
assert.equal(now(), 60, "and is bounded");
pointer(w, handle, "pointerup", { x: 5000 });
assert.equal(w.localStorage.getItem("bk-resizable:split"), "[60,40]");
w.close();

// The next visit restores the saved split.
const again = start("[42,58]");
await tick();
assert.equal(Number(again.doc.querySelector("#split .bk-resizable__handle").getAttribute("aria-valuenow")), 42, "restored from localStorage");
const v = again.doc.getElementById("vert").querySelector(".bk-resizable__handle");
assert.equal(v.getAttribute("aria-orientation"), "horizontal");
key(again.w, v, "ArrowDown");
assert.equal(Number(v.getAttribute("aria-valuenow")), 55, "a vertical group uses ArrowUp/Down");
again.w.close();
console.log("kit_resizable ok");
