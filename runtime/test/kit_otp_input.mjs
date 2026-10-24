// Drives the kit's OTPInput in jsdom (see kit_forms_test.go): typing
// advances, Backspace goes back, paste fills every box, and the carrier
// input holds the joined code.
import assert from "node:assert/strict";
import { load, tick, key, fill } from "./kit_harness.mjs";

const { w, doc, mount } = load();
mount("OTPInput");
await tick();
const [first, second] = doc.querySelectorAll(".bk-otp");
const value = first.querySelector(".bk-otp__value");
const boxes = [...first.querySelectorAll(".bk-otp__box")];
const type = (box, v) => fill(w, box, v);
let inputs = 0, changes = 0;
value.addEventListener("input", () => inputs++);
value.addEventListener("change", () => changes++);

assert.equal(value.hidden, true, "the carrier hides once enhanced");
assert.equal(first.querySelector(".bk-otp__boxes").hidden, false, "the boxes show");
assert.equal(boxes.length, 6);
boxes[0].focus();
type(boxes[0], "4");
assert.equal(doc.activeElement, boxes[1], "typing advances");
type(boxes[1], "x");
assert.equal(boxes[1].value, "", "a letter is refused in numeric mode");
assert.equal(doc.activeElement, boxes[1], "a refused key does not advance");
type(boxes[1], "2");
assert.equal(value.value, "42", "the carrier holds the joined code");
assert.ok(inputs >= 2, "input fires on the carrier");
key(w, doc.activeElement, "Backspace");
assert.equal(doc.activeElement, boxes[1], "Backspace on an empty box goes back");
assert.equal(boxes[1].value, "", "and clears the previous box");
assert.equal(value.value, "4");
// Paste from the first box fills all six and fires change once complete.
const paste = new w.Event("paste", { bubbles: true, cancelable: true });
paste.clipboardData = { getData: () => "98-76 54" };
boxes[0].dispatchEvent(paste);
assert.equal(value.value, "987654", "paste fills every box, dropping separators");
assert.deepEqual(boxes.map((b) => b.value), ["9", "8", "7", "6", "5", "4"]);
assert.equal(doc.activeElement, boxes[5], "focus ends on the last box");
assert.equal(changes, 1, "change fires when the code is complete");
// An autofill that drops the whole code into one box spreads it out.
type(boxes[0], "123456");
assert.equal(value.value, "123456");
key(w, boxes[3], "ArrowLeft");
assert.equal(doc.activeElement, boxes[2], "ArrowLeft moves back");
// Alphanumeric mode, and the initial value fills the boxes.
const alpha = [...second.querySelectorAll(".bk-otp__box")];
assert.deepEqual(alpha.map((b) => b.value), ["a", "b", "", ""], "the rendered value fills the boxes");
type(alpha[2], "Z");
assert.equal(second.querySelector(".bk-otp__value").value, "abZ", "letters pass in alphanumeric mode");
w.close();
console.log("kit_otp_input ok");
