// Drives the kit's Popover and Tooltip in jsdom (see kit_overlays_test.go).
// jsdom has neither the Popover API nor anchor positioning, so this runs the
// fallback: the module toggles hidden and places the panel itself, flipping
// it above the trigger near the bottom edge and keeping it on screen.
import assert from "node:assert/strict";
import { load, tick, click, key } from "./kit_harness.mjs";
import { loadESM } from "./kit_esm.mjs";

const { w, doc, mount } = load();
assert.equal(typeof w.HTMLElement.prototype.showPopover, "undefined", "jsdom lacks the Popover API; this test covers the fallback");
// Popover's module exports place(), so it loads through kit_esm.mjs.
loadESM(w, "Popover");
mount("Tooltip");
await tick();
const trigger = doc.querySelector(".bk-popover__trigger");
const panel = doc.getElementById("pp");
const rect = (r) => () => ({ ...r, right: r.left + r.width, bottom: r.top + r.height, x: r.left, y: r.top });
panel.getBoundingClientRect = rect({ top: 0, left: 0, width: 200, height: 120 });

assert.ok(panel.hidden, "closed once enhanced");
assert.equal(trigger.hasAttribute("popovertarget"), false, "the fallback owns the trigger");
assert.equal(panel.hasAttribute("popover"), false, "and the panel");
trigger.getBoundingClientRect = rect({ top: 100, left: 50, width: 100, height: 30 });
click(w, trigger);
assert.equal(panel.hidden, false, "a click opens");
assert.equal(trigger.getAttribute("aria-expanded"), "true");
assert.equal(panel.style.top, "136px", "below the trigger, 6px away");
assert.equal(panel.style.left, "50px", "aligned to its start");
assert.equal(panel.dataset.side, "bottom");
click(w, doc.getElementById("outside"));
assert.ok(panel.hidden, "a click outside closes");
assert.equal(trigger.getAttribute("aria-expanded"), "false");

// Near the bottom edge it flips above; near the right edge it is clamped.
trigger.getBoundingClientRect = rect({ top: w.innerHeight - 40, left: w.innerWidth - 60, width: 50, height: 30 });
click(w, trigger);
assert.equal(panel.dataset.side, "top", "flips above near the bottom");
assert.equal(panel.style.top, `${w.innerHeight - 40 - 6 - 120}px`);
assert.equal(panel.style.left, `${w.innerWidth - 8 - 200}px`, "kept 8px inside the viewport");
panel.querySelector("button").focus();
key(w, doc, "Escape");
assert.ok(panel.hidden, "Escape closes");
assert.equal(doc.activeElement, trigger, "focus returns to the trigger");

// Tooltip: the description moves onto the focusable child.
const [tipA, tipB] = doc.querySelectorAll(".bk-tooltip");
const btn = tipA.querySelector("button");
assert.equal(btn.getAttribute("aria-describedby"), tipA.querySelector("[role=tooltip]").id, "the button is described by the tip");
assert.equal(tipA.hasAttribute("aria-describedby"), false);
assert.equal(tipB.tabIndex, 0, "a tooltip on plain text makes the wrapper focusable");
assert.equal(tipB.getAttribute("aria-describedby"), tipB.querySelector("[role=tooltip]").id);
key(w, btn, "Escape");
assert.ok(tipA.classList.contains("bk-tooltip--dismissed"), "Escape hides the tip");
tipA.dispatchEvent(new w.MouseEvent("mouseleave"));
assert.equal(tipA.classList.contains("bk-tooltip--dismissed"), false, "until the pointer leaves");
w.close();
console.log("kit_popover ok");
