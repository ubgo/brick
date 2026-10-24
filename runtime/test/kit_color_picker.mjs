// Drives the kit's ColorPicker in jsdom (see kit_forms_test.go): a colour
// from the custom picker becomes the checked radio's value.
import assert from "node:assert/strict";
import { load, tick } from "./kit_harness.mjs";

const { w, doc, mount } = load();
mount("ColorPicker");
await tick();
const picker = doc.querySelector(".bk-color__picker");
const custom = picker.closest(".bk-color__swatch").querySelector("input[type=radio]");
let changes = 0;
custom.addEventListener("change", () => changes++);
picker.value = "#ff8800";
picker.dispatchEvent(new w.Event("input", { bubbles: true }));
assert.equal(custom.value, "#ff8800", "the radio takes the colour");
assert.equal(custom.checked, true, "and is checked");
assert.equal(changes, 1, "change fires");
assert.equal(new w.FormData(doc.createElement("form")).get("c"), null);
const form = doc.createElement("form");
form.append(doc.querySelector(".bk-color"));
assert.equal(new w.FormData(form).get("c"), "#ff8800", "the form submits the custom colour");
w.close();
console.log("kit_color_picker ok");
