// Drives the kit's Menubar in jsdom (see kit_overlays2_test.go): one tab
// stop, ArrowLeft/Right between menu names, and with a menu open moving
// to the neighbour's first item; pointing at another name while one is open
// switches menus; only one is open at a time.
import assert from "node:assert/strict";
import { load, tick, key } from "./kit_harness.mjs";
import { loadESM, pointer } from "./kit_esm.mjs";

const { w, doc } = load();
loadESM(w, "DropdownMenu", "Menubar");
await tick();
const bar = doc.querySelector(".bk-menubar");
const menus = [...bar.querySelectorAll(":scope > .bk-dropdown")];
const names = menus.map((m) => m.querySelector("summary"));
const label = () => doc.activeElement.querySelector(".bk-menu-item__label")?.textContent;

assert.equal(bar.getAttribute("role"), "menubar");
assert.deepEqual(names.map((s) => s.getAttribute("role")), ["menuitem", "menuitem", "menuitem"]);
assert.deepEqual(names.map((s) => s.tabIndex), [0, -1, -1], "one tab stop");
names[0].focus();
key(w, names[0], "ArrowRight");
assert.equal(doc.activeElement, names[1], "ArrowRight moves to the next name");
assert.deepEqual(names.map((s) => s.tabIndex), [-1, 0, -1], "roving tabindex");
key(w, names[1], "ArrowLeft");
key(w, names[0], "ArrowLeft");
assert.equal(doc.activeElement, names[2], "and wraps");
key(w, names[2], "Home");
assert.equal(doc.activeElement, names[0], "Home");

key(w, names[0], "ArrowDown");
await tick();
assert.ok(menus[0].open, "ArrowDown opens the menu");
assert.equal(label(), "New file");
key(w, doc.activeElement, "ArrowRight");
await tick();
assert.equal(menus[0].open, false, "ArrowRight in a menu closes it");
assert.ok(menus[1].open, "and opens the next");
assert.equal(label(), "Undo", "on its first item");
key(w, doc.activeElement, "ArrowLeft");
await tick();
assert.ok(menus[0].open && !menus[1].open, "ArrowLeft goes back");

pointer(w, names[2], "pointerover");
await tick();
assert.ok(menus[2].open && !menus[0].open, "pointing at another name while open switches");
key(w, doc.activeElement, "Escape");
await tick();
assert.equal(menus[2].open, false, "Escape closes");
assert.equal(doc.activeElement, names[2], "and returns to its name");
pointer(w, names[0], "pointerover");
await tick();
assert.equal(menus[0].open, false, "pointing opens nothing while every menu is closed");
w.close();
console.log("kit_menubar ok");
