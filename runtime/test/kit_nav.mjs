// Drives Tabs beyond kit_tabs.mjs: the server's selected tab is where the
// script starts, the keyboard skips a disabled tab, and link-mode tabs are
// left alone (they are real links). See runKitJS in kit_test.go.
import assert from "node:assert/strict";
import { load, click, key } from "./kit_harness.mjs";

const { w, doc, mount } = load("Tabs");
// The third set's bar is laid out wider than its box (jsdom has no layout).
const wide = doc.querySelectorAll(".bk-tabs")[2].querySelector(".bk-tabs__list");
let scroll = 0;
Object.defineProperty(wide, "scrollWidth", { value: 600 });
Object.defineProperty(wide, "clientWidth", { value: 200 });
Object.defineProperty(wide, "scrollLeft", { get: () => scroll, set: (v) => { scroll = Math.max(0, Math.min(400, v)); } });
mount("Tabs");
const [panelSet, linkSet] = doc.querySelectorAll(".bk-tabs");
const tabs = [...panelSet.querySelectorAll("[role=tab]")];
const panels = [...panelSet.querySelectorAll("[role=tabpanel]")];
const shown = () => panels.map((p) => !p.hidden);
assert.deepEqual(shown(), [false, true, false, false], "starts on the server's selected tab");
assert.ok(panelSet.classList.contains("bk-tabs--js"));
tabs[1].focus();
key(w, tabs[1], "ArrowRight");
assert.deepEqual(shown(), [false, false, false, true], "ArrowRight skips the disabled tab");
assert.equal(doc.activeElement, tabs[3]);
click(w, tabs[2]);
assert.deepEqual(shown(), [false, false, false, true], "a disabled tab ignores clicks");
key(w, tabs[3], "Home");
assert.deepEqual(shown(), [true, false, false, false], "Home");
assert.equal(linkSet.classList.contains("bk-tabs--js"), false, "link tabs need no script");
assert.equal(linkSet.querySelector("[aria-current=page]").textContent, "B");
// An overflowing bar fades the side that still hides tabs.
assert.equal(wide.dataset.fade, "end", "at the start, the end fades");
scroll = 200; wide.dispatchEvent(new w.Event("scroll"));
assert.equal(wide.dataset.fade, "both");
scroll = 400; wide.dispatchEvent(new w.Event("scroll"));
assert.equal(wide.dataset.fade, "start");
assert.equal(panelSet.querySelector(".bk-tabs__list").hasAttribute("data-fade"), false, "a bar that fits has no fade");
w.close();
console.log("kit_nav ok");
