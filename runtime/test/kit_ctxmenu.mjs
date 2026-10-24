// Drives the kit's ContextMenu in jsdom (see kit_overlays2_test.go). jsdom
// has no Popover API, so this runs the fallback (hidden toggled by the
// module): right-click opens at the pointer and focuses the first item,
// the keys are DropdownMenu's, Escape returns focus, Shift+F10 opens it
// from the keyboard, a long press opens it on touch, and a click outside
// or on an item closes it.
import assert from "node:assert/strict";
import { load, tick, click, key } from "./kit_harness.mjs";
import { loadESM, pointer, rect } from "./kit_esm.mjs";

const { w, doc } = load();
loadESM(w, "DropdownMenu", "ContextMenu");
await tick();
const target = doc.querySelector(".bk-context-menu__target");
const menu = doc.querySelector(".bk-context-menu__menu");
const label = () => doc.activeElement.querySelector(".bk-menu-item__label").textContent;
rect(menu, { width: 180, height: 120 });

assert.ok(menu.hidden, "hidden until asked");
assert.equal(target.tabIndex, 0, "the region is focusable");
const e = new w.MouseEvent("contextmenu", { bubbles: true, cancelable: true, clientX: 200, clientY: 150 });
target.dispatchEvent(e);
assert.ok(e.defaultPrevented, "the browser's own menu is replaced");
assert.equal(menu.hidden, false, "right-click opens it");
assert.deepEqual([menu.style.left, menu.style.top], ["200px", "150px"], "at the pointer");
assert.equal(label(), "Rename", "on the first item");
key(w, doc.activeElement, "ArrowDown");
assert.equal(label(), "Duplicate", "DropdownMenu's keys");
key(w, doc.activeElement, "Escape");
assert.ok(menu.hidden, "Escape closes");
assert.equal(doc.activeElement, target, "and focus returns to the region");

// Near the bottom-right corner it stays on screen.
target.dispatchEvent(new w.MouseEvent("contextmenu", { bubbles: true, cancelable: true, clientX: w.innerWidth - 20, clientY: w.innerHeight - 20 }));
assert.deepEqual([menu.style.left, menu.style.top], [`${w.innerWidth - 180 - 8}px`, `${w.innerHeight - 20 - 120}px`], "kept inside the viewport");
click(w, doc.getElementById("outside"));
assert.ok(menu.hidden, "a click outside closes");

// Keyboard: Shift+F10 on the focused region.
target.focus();
rect(target, { left: 40, top: 60, width: 300, height: 100 });
key(w, target, "F10", { shiftKey: true });
assert.equal(menu.hidden, false, "Shift+F10 opens it");
assert.deepEqual([menu.style.left, menu.style.top], ["56px", "76px"], "at the region");
click(w, doc.activeElement);
assert.ok(menu.hidden, "choosing an item closes it");
key(w, target, "ContextMenu");
assert.equal(menu.hidden, false, "so does the Menu key");
key(w, doc.activeElement, "Escape");

// Touch: a long press opens it; moving cancels.
pointer(w, target, "pointerdown", { x: 100, y: 100, pointerType: "touch" });
pointer(w, target, "pointermove", { x: 130, y: 100, pointerType: "touch" });
await tick(600);
assert.ok(menu.hidden, "a moving finger is a scroll, not a press");
pointer(w, target, "pointerdown", { x: 100, y: 100, pointerType: "touch" });
await tick(600);
assert.equal(menu.hidden, false, "a long press opens it");
w.close();
console.log("kit_ctxmenu ok");
