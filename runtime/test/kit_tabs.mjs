// Drives the kit's Tabs in jsdom: the page rendered by brick and the real
// runtime and Tabs modules, written to the directory given as argv[2] by
// kit_test.go. (Not a *_test.mjs: it needs that directory.)
import assert from "node:assert/strict";
import { load } from "./kit_harness.mjs";

const { w, doc, mount } = load();
mount("Tabs");
const scrolls = [];
w.HTMLElement.prototype.scrollBy = function (o) { scrolls.push(o.left); };
mount("Carousel");

const tabs = [...doc.querySelectorAll("[role=tab]")];
const panels = [...doc.querySelectorAll("[role=tabpanel]")];
const shown = () => panels.map((p) => !p.hidden);
assert.equal(tabs.length, 3);
assert.deepEqual(shown(), [true, false, false], "first panel only");
assert.equal(tabs[0].getAttribute("aria-selected"), "true");
tabs[2].dispatchEvent(new w.MouseEvent("click", { bubbles: true }));
assert.deepEqual(shown(), [false, false, true], "click selects");
tabs[2].focus();
tabs[2].dispatchEvent(new w.KeyboardEvent("keydown", { key: "ArrowRight", bubbles: true }));
assert.deepEqual(shown(), [true, false, false], "ArrowRight wraps to the first");
assert.equal(doc.activeElement, tabs[0], "focus follows");
tabs[0].dispatchEvent(new w.KeyboardEvent("keydown", { key: "End", bubbles: true }));
assert.deepEqual(shown(), [false, false, true], "End");
assert.equal(tabs[1].tabIndex, -1, "roving tabindex");
// Carousel: the buttons appear once mounted and move one slide.
const prev = doc.querySelector(".bk-prev"), next = doc.querySelector(".bk-next");
assert.equal(prev.hidden || next.hidden, false, "buttons shown with JavaScript");
next.dispatchEvent(new w.MouseEvent("click", { bubbles: true }));
prev.dispatchEvent(new w.MouseEvent("click", { bubbles: true }));
assert.equal(scrolls.length, 2, "each button scrolls");
assert.ok(scrolls[0] >= 0 && scrolls[1] <= 0, "next forward, previous back");
w.close();
console.log("kit_tabs ok");
