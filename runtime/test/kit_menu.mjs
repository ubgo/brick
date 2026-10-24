// Drives the kit's DropdownMenu in jsdom (see kit_overlays_test.go): the
// menu button keys, roving focus that skips disabled items and wraps,
// Home/End, typeahead, Escape returning focus, a click outside and a click
// on an item closing it.
import assert from "node:assert/strict";
import { load, tick, click, key } from "./kit_harness.mjs";
import { loadESM } from "./kit_esm.mjs";

const { w, doc } = load();
loadESM(w, "DropdownMenu");
await tick();
const menu = doc.getElementById("m");
const summary = menu.querySelector("summary");
const label = () => doc.activeElement.querySelector(".bk-menu-item__label").textContent.trim();
const items = [...menu.querySelectorAll("[role=menuitem]")];
assert.ok(items.every((i) => i.tabIndex === -1), "items leave the tab order once enhanced");
assert.equal(summary.getAttribute("aria-expanded"), "false");

summary.focus();
key(w, summary, "ArrowDown");
await tick();
assert.ok(menu.open, "ArrowDown opens");
assert.equal(summary.getAttribute("aria-expanded"), "true");
assert.equal(label(), "Profile", "on the first item");
key(w, doc.activeElement, "ArrowDown");
assert.equal(label(), "Settings");
key(w, doc.activeElement, "ArrowDown");
assert.equal(label(), "Sign out", "the disabled item is skipped");
key(w, doc.activeElement, "ArrowUp");
assert.equal(label(), "Settings");
key(w, doc.activeElement, "End");
assert.equal(label(), "Share");
key(w, doc.activeElement, "ArrowDown");
assert.equal(label(), "Profile", "wraps to the first");
key(w, doc.activeElement, "ArrowUp");
assert.equal(label(), "Share", "wraps to the last");
key(w, doc.activeElement, "Home");
assert.equal(label(), "Profile");
key(w, doc.activeElement, "s");
assert.equal(label(), "Settings", "typeahead");
key(w, doc.activeElement, "i");
assert.equal(label(), "Sign out", "typeahead extends the typed prefix");
await tick(600);
key(w, doc.activeElement, "p");
assert.equal(label(), "Profile", "typeahead wraps after the buffer resets");
key(w, doc.activeElement, "Escape");
assert.equal(menu.open, false, "Escape closes");
assert.equal(doc.activeElement, summary, "focus returns to the button");

key(w, summary, "ArrowUp");
await tick();
assert.equal(label(), "Share", "ArrowUp opens on the last item");
click(w, doc.getElementById("outside"));
assert.equal(menu.open, false, "a click outside closes");

menu.open = true;
await tick();
click(w, items[1]);
assert.equal(menu.open, false, "choosing an item closes");
menu.open = true;
await tick();
click(w, items[2]);
assert.ok(menu.open, "a disabled item does nothing");
w.close();
console.log("kit_menu ok");
