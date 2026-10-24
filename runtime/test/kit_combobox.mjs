// Drives the kit's Combobox in jsdom (see kit_forms_test.go): the ARIA
// combobox pattern over a native select that keeps the value.
import assert from "node:assert/strict";
import { load, tick, key } from "./kit_harness.mjs";
import { loadESM } from "./kit_esm.mjs";

const { w, doc } = load();
// Popover exports place() and the pickers import it, so those modules load
// through kit_esm.mjs (Popover first), not the harness's plain eval.
loadESM(w, "Popover", "Combobox");
await tick();
const el = doc.querySelector(".bk-combobox");
const select = el.querySelector("select");
const input = el.querySelector("[role=combobox]");
const list = el.querySelector("[role=listbox]");
const shown = () => [...list.querySelectorAll("[role=option]")].filter((o) => !o.hidden).map((o) => o.dataset.label);
const active = () => doc.getElementById(input.getAttribute("aria-activedescendant"))?.dataset.label;
let changes = 0;
select.addEventListener("change", () => changes++);

assert.equal(select.hidden, true, "the select hides once enhanced");
assert.equal(el.querySelector(".bk-combobox__ui").hidden, false);
const label = doc.querySelector("label.bk-label");
assert.equal(input.id, label.getAttribute("for"), "the Field's label now names the text input");
assert.equal(select.id, "", "and the select gave its id away");
assert.equal(list.hidden, true);
input.focus();
key(w, input, "ArrowDown");
assert.equal(list.hidden, false, "ArrowDown opens");
assert.equal(input.getAttribute("aria-expanded"), "true");
assert.equal(active(), "Ava Patel", "the first option is active");
key(w, input, "ArrowDown");
assert.equal(active(), "Maya Chen", "ArrowDown moves");
key(w, input, "ArrowUp");
key(w, input, "ArrowUp");
assert.equal(active(), "Zed", "ArrowUp wraps to the last");
input.value = "no";
input.dispatchEvent(new w.Event("input", { bubbles: true }));
assert.deepEqual(shown(), ["Noah Kim"], "typing filters, case-insensitive");
assert.equal(active(), "Noah Kim", "the first match is active");
key(w, input, "Enter");
assert.equal(select.value, "u3", "Enter writes the value into the select");
assert.equal(changes, 1, "and fires change on it");
assert.equal(input.value, "Noah Kim", "the input shows the chosen label");
assert.equal(list.hidden, true, "and the list closes");
assert.deepEqual(shown().length, 4, "the filter resets");
input.value = "zzz";
input.dispatchEvent(new w.Event("input", { bubbles: true }));
assert.equal(el.querySelector(".bk-combobox__empty").hidden, false, "no match shows the empty message");
key(w, input, "Escape");
assert.equal(list.hidden, true, "Escape closes");
assert.equal(input.value, "Noah Kim", "and restores the chosen label");
assert.equal(select.value, "u3", "without changing the value");
// A click (mousedown) on an option picks it.
key(w, input, "ArrowDown");
const maya = [...list.querySelectorAll("[role=option]")].find((o) => o.dataset.value === "u2");
maya.dispatchEvent(new w.MouseEvent("mousedown", { bubbles: true, cancelable: true }));
assert.equal(select.value, "u2");
assert.equal(maya.getAttribute("aria-selected"), "true");
assert.equal(changes, 2);
w.close();
console.log("kit_combobox ok");
