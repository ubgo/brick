// Drives the kit's Textarea in jsdom (see kit_forms_test.go): the counter
// follows the text and turns red at the limit; autosize sets a height.
import assert from "node:assert/strict";
import { load, tick } from "./kit_harness.mjs";

const { w, doc, mount } = load();
mount("Textarea");
await tick();
const ta = doc.querySelector("textarea");
const count = doc.querySelector(".bk-textarea__count");
assert.equal(count.textContent, "3 / 10");
ta.value = "héllo wörld";
ta.dispatchEvent(new w.Event("input", { bubbles: true }));
assert.equal(count.textContent, "11 / 10", "counts characters, not bytes");
assert.ok(count.classList.contains("bk-textarea__count--full"), "red at the limit");
Object.defineProperty(ta, "scrollHeight", { value: 120, configurable: true });
ta.dispatchEvent(new w.Event("input", { bubbles: true }));
assert.equal(ta.style.height, "122px", "autosize fits the content");
w.close();
console.log("kit_textarea ok");
