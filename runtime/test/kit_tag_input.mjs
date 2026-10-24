// Drives the kit's TagInput in jsdom (see kit_forms_test.go): Enter and
// comma add chips, × and Backspace remove them, duplicates and the max are
// respected, and the carrier holds the comma-joined value.
import assert from "node:assert/strict";
import { load, tick, key, click } from "./kit_harness.mjs";

const { w, doc, mount } = load();
mount("TagInput");
await tick();
const el = doc.querySelector(".bk-tags");
const value = el.querySelector(".bk-tags__value");
const entry = el.querySelector(".bk-tags__entry");
const chips = () => [...el.querySelectorAll(".bk-tags__chip .bk-tags__text")].map((c) => c.textContent);
let changes = 0;
value.addEventListener("change", () => changes++);

assert.equal(value.hidden, true, "the carrier hides once enhanced");
assert.equal(entry.hidden, false, "the entry field shows");
assert.deepEqual(chips(), ["go", "design"], "the rendered value becomes chips");
assert.equal(changes, 0, "mounting fires no change");
entry.value = "html";
key(w, entry, "Enter");
assert.deepEqual(chips(), ["go", "design", "html"], "Enter adds");
assert.equal(value.value, "go,design,html", "the carrier is comma-joined");
assert.equal(changes, 1);
entry.value = "css, ";
entry.dispatchEvent(new w.Event("input", { bubbles: true }));
assert.deepEqual(chips(), ["go", "design", "html", "css"], "a typed comma adds");
assert.equal(entry.value, "", "the entry clears");
entry.value = "sql";
key(w, entry, "Enter");
assert.equal(chips().length, 4, "max stops adding");
assert.ok(el.classList.contains("bk-tags--full"));
click(w, el.querySelectorAll(".bk-tags__remove")[1]);
assert.deepEqual(chips(), ["go", "html", "css"], "× removes that chip");
assert.equal(el.querySelectorAll(".bk-tags__remove")[0].getAttribute("aria-label"), "Remove go", "each remove button names its tag");
entry.value = "GO";
key(w, entry, "Enter");
assert.deepEqual(chips(), ["go", "html", "css"], "duplicates are ignored, whatever the case");
key(w, entry, "Backspace");
assert.deepEqual(chips(), ["go", "html"], "Backspace on an empty entry removes the last chip");
assert.equal(value.value, "go,html");
w.close();
console.log("kit_tag_input ok");
