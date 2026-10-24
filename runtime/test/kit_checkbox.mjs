// Drives the kit's Checkbox in jsdom (see kit_forms_test.go): the
// indeterminate state HTML cannot express is set by the module.
import assert from "node:assert/strict";
import { load, tick, click } from "./kit_harness.mjs";

const { w, doc, mount } = load();
const box = doc.querySelector("input[name=all]");
assert.equal(box.indeterminate, false, "no attribute can set it");
mount("Checkbox");
await tick();
assert.equal(box.indeterminate, true, "the module sets indeterminate");
click(w, box);
assert.equal(box.indeterminate, false, "a click resolves it");
assert.equal(box.checked, true);
assert.equal(box.hasAttribute("data-indeterminate"), false);
w.close();
console.log("kit_checkbox ok");
