// Drives the kit's AccentPicker in jsdom (see kit_harness.mjs).
import assert from "node:assert/strict";
import { load, tick } from "./kit_harness.mjs";

const { w, doc, mount } = load("AccentPicker");
const root = doc.documentElement;
const [a, b] = [doc.getElementById("a"), doc.getElementById("b")];
assert.ok(a.hidden && b.hidden, "hidden before mounting");
root.dataset.accent = "rose";
mount("AccentPicker");
await tick();
assert.ok(!a.hidden && !b.hidden, "shown once mounted");
const checked = (el) => el.querySelector("input:checked")?.value;
assert.equal(checked(a), "rose", "checks the current accent");
const emerald = a.querySelector("input[value=emerald]");
emerald.checked = true;
emerald.dispatchEvent(new w.Event("change", { bubbles: true }));
assert.equal(root.dataset.accent, "emerald", "sets data-accent");
assert.equal(w.localStorage.getItem("bk-accent"), "emerald", "saves the choice");
await tick();
assert.equal(checked(b), "emerald", "the other picker follows");
root.dataset.accent = "violet";
await tick();
assert.equal(checked(a), "violet", "follows data-accent set by anything");
b.remove();
await tick();
w.close();
console.log("kit_accent_picker ok");
