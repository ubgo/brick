// Drives the kit's Slider in jsdom (see kit_forms_test.go): the readout and
// the track fill follow the value.
import assert from "node:assert/strict";
import { load, tick } from "./kit_harness.mjs";

const { w, doc, mount } = load();
mount("Slider");
await tick();
const el = doc.querySelector(".bk-slider");
const input = el.querySelector("input[type=range]");
const out = el.querySelector("output");
assert.equal(out.textContent, "50%");
assert.equal(el.style.getPropertyValue("--bk-slider-p"), "25%");
input.value = "150";
input.dispatchEvent(new w.Event("input", { bubbles: true }));
assert.equal(out.textContent, "150%", "the readout follows");
assert.equal(el.style.getPropertyValue("--bk-slider-p"), "75%", "the fill follows");
const priced = doc.querySelectorAll(".bk-slider")[1];
const pin = priced.querySelector("input");
pin.value = "42";
pin.dispatchEvent(new w.Event("input", { bubbles: true }));
assert.equal(priced.querySelector("output").textContent, "$42", "prefix goes before the number");
w.close();
console.log("kit_slider ok");
