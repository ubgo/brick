// Drives the kit's NumberInput in jsdom (see kit_forms_test.go): the
// buttons step, clamp to min and max, disable at a bound and fire events.
import assert from "node:assert/strict";
import { load, tick, click } from "./kit_harness.mjs";

const { w, doc, mount } = load();
mount("NumberInput");
await tick();
const [a, b] = doc.querySelectorAll(".bk-number");
const input = a.querySelector("input");
const dec = a.querySelector("[data-step='-1']"), inc = a.querySelector("[data-step='1']");
let inputs = 0, changes = 0;
input.addEventListener("input", () => inputs++);
input.addEventListener("change", () => changes++);

assert.equal(dec.hidden || inc.hidden, false, "the buttons show once enhanced");
click(w, inc);
assert.equal(input.value, "10", "+ steps up");
assert.equal(inputs, 1, "input fires");
assert.equal(changes, 1, "change fires");
assert.equal(inc.disabled, true, "+ disables at max");
click(w, inc);
assert.equal(input.value, "10", "a disabled button does nothing");
input.value = "0";
input.dispatchEvent(new w.Event("change", { bubbles: true }));
assert.equal(input.value, "1", "a typed value below min is clamped");
assert.equal(dec.disabled, true, "− disables at min");
assert.equal(inc.disabled, false);
input.value = "";
input.dispatchEvent(new w.Event("input", { bubbles: true }));
click(w, inc);
assert.equal(input.value, "1", "stepping from empty starts at min");
// Decimal steps keep their precision.
const bi = b.querySelector("input");
click(w, b.querySelector("[data-step='1']"));
assert.equal(bi.value, "2.0", "step 0.5 from 1.5");
click(w, b.querySelector("[data-step='-1']"));
click(w, b.querySelector("[data-step='-1']"));
click(w, b.querySelector("[data-step='-1']"));
click(w, b.querySelector("[data-step='-1']"));
click(w, b.querySelector("[data-step='-1']"));
assert.equal(bi.value, "0.0", "clamped at min 0");
w.close();
console.log("kit_number_input ok");
