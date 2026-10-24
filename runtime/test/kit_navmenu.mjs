// Drives the kit's NavigationMenu in jsdom (see kit_overlays2_test.go):
// hover intent opens a panel after a delay and closes it after leaving,
// one panel at a time, ArrowDown opens on the first link, arrows move,
// Escape returns focus, a click outside closes.
import assert from "node:assert/strict";
import { load, tick, click, key } from "./kit_harness.mjs";
import { loadESM, pointer } from "./kit_esm.mjs";

const { w, doc } = load();
loadESM(w, "NavigationMenu");
await tick();
const details = [...doc.querySelectorAll(".bk-navmenu__details")];
const triggers = [...doc.querySelectorAll(".bk-navmenu__trigger")];
const [products, docs] = details;

assert.equal(triggers.length, 3, "two panels and a link");
assert.equal(products.querySelector("summary").getAttribute("aria-expanded"), "false");
pointer(w, products.querySelector("summary"), "pointerover");
await tick(40);
assert.equal(products.open, false, "not at once: hover intent");
await tick(150);
assert.ok(products.open, "opens after the delay");
assert.equal(products.querySelector("summary").getAttribute("aria-expanded"), "true");
pointer(w, docs.querySelector("summary"), "pointerover");
await tick(20);
assert.ok(docs.open && !products.open, "with one open, the next opens at once and the other closes");
pointer(w, docs.querySelector("summary"), "pointerout");
await tick(350);
assert.equal(docs.open, false, "closes after the pointer leaves");

// Keyboard.
triggers[0].focus();
key(w, triggers[0], "ArrowRight");
assert.equal(doc.activeElement, triggers[1], "ArrowRight moves between names");
key(w, triggers[1], "ArrowRight");
assert.equal(doc.activeElement, triggers[2], "including plain links");
key(w, triggers[2], "ArrowRight");
assert.equal(doc.activeElement, triggers[0], "and wraps");
key(w, triggers[0], "ArrowDown");
assert.ok(products.open, "ArrowDown opens");
assert.equal(doc.activeElement.textContent.includes("Kit"), true, "on the first link");
key(w, doc.activeElement, "ArrowDown");
assert.ok(doc.activeElement.textContent.includes("Actions"), "ArrowDown moves");
key(w, doc.activeElement, "ArrowUp");
key(w, doc.activeElement, "ArrowUp");
assert.equal(doc.activeElement, triggers[0], "ArrowUp from the first link returns to the name");
key(w, triggers[0], "ArrowDown");
key(w, doc.activeElement, "Escape");
assert.equal(products.open, false, "Escape closes");
assert.equal(doc.activeElement, triggers[0], "and returns focus");

products.open = true;
await tick();
click(w, doc.getElementById("outside"));
assert.equal(products.open, false, "a click outside closes");
assert.equal(doc.querySelector("a[aria-current=page]").getAttribute("href"), "/pricing/", "current is marked");
w.close();
console.log("kit_navmenu ok");
