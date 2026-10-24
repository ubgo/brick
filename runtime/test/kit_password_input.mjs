// Drives the kit's PasswordInput in jsdom (see kit_forms_test.go).
import assert from "node:assert/strict";
import { load, tick, click } from "./kit_harness.mjs";

const { w, doc, mount } = load();
mount("PasswordInput");
await tick();
const input = doc.querySelector("input[name=pw]");
const btn = doc.querySelector(".bk-password__toggle");
assert.equal(btn.hidden, false, "the toggle shows once enhanced");
assert.equal(input.type, "password");
click(w, btn);
assert.equal(input.type, "text", "shows the password");
assert.equal(btn.getAttribute("aria-pressed"), "true");
assert.equal(doc.activeElement, input, "focus returns to the field");
click(w, btn);
assert.equal(input.type, "password", "hides it again");
assert.equal(btn.getAttribute("aria-pressed"), "false");
assert.equal(input.value, "secret", "the value is untouched");
w.close();
console.log("kit_password_input ok");
