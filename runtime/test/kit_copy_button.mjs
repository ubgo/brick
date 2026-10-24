// Drives the kit's CopyButton in jsdom (see kit_harness.mjs).
import assert from "node:assert/strict";
import { load, tick, click } from "./kit_harness.mjs";

const { w, doc, mount } = load("CopyButton");
const copied = [];
Object.defineProperty(w.navigator, "clipboard", { value: { writeText: async (s) => { copied.push(s); } }, configurable: true });
const [a, b] = [doc.getElementById("a"), doc.getElementById("b")];
assert.ok(a.hidden && b.hidden, "hidden without JavaScript");
mount("CopyButton");
await tick();
assert.ok(!a.hidden && !b.hidden, "shown once mounted");
click(w, a);
await tick();
assert.deepEqual(copied, ["go get brick"], "copies the text prop");
assert.ok("copied" in a.dataset, "shows the check");
assert.equal(a.querySelector(".bk-copy-button__live").textContent, "Copied", "announces");
click(w, b);
await tick();
assert.equal(copied[1], "npm i brick", "copies the target's trimmed text");
assert.equal(b.querySelector(".bk-copy-button__text").textContent, "Done", "shows the copied label");
await tick(2100);
assert.ok(!("copied" in a.dataset) && !("copied" in b.dataset), "resets after two seconds");
assert.equal(b.querySelector(".bk-copy-button__text").textContent, "Copy", "restores the label");
w.close();
console.log("kit_copy_button ok");
