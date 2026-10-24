// Drives the kit's SearchInput in jsdom (see kit_forms_test.go): the clear
// button, Escape, and the ⌘K shortcut.
import assert from "node:assert/strict";
import { load, tick, key, click } from "./kit_harness.mjs";

const { w, doc, mount } = load();
mount("SearchInput");
await tick();
const input = doc.querySelector("input[name=q]");
const clear = doc.querySelector(".bk-search__clear");
let inputs = 0;
input.addEventListener("input", () => inputs++);
assert.equal(clear.hidden, false, "clear shows while there is text");
click(w, clear);
assert.equal(input.value, "", "clear empties");
assert.equal(inputs, 1, "and fires input");
assert.equal(clear.hidden, true, "and hides itself");
assert.equal(doc.activeElement, input, "focus stays in the field");
input.blur();
key(w, doc.body, "k", { metaKey: true });
assert.equal(doc.activeElement, input, "⌘K focuses the field");
input.value = "noah";
input.dispatchEvent(new w.Event("input", { bubbles: true }));
key(w, input, "Escape");
assert.equal(input.value, "", "Escape clears");
w.close();
console.log("kit_search_input ok");
