// Drives the kit's CommandPalette in jsdom (see kit_overlays_test.go):
// ⌘K / Ctrl+K opens it, typing filters by label, keyword and group,
// arrows move the selection, Enter follows a link or emits a command,
// Escape closes.
import assert from "node:assert/strict";
import { load, tick, click, key, fill } from "./kit_harness.mjs";
import { loadESM } from "./kit_esm.mjs";

const { w, doc } = load();
loadESM(w, "Dialog", "CommandPalette");
await tick();
const dialog = doc.querySelector(".bk-cmdk__dialog");
const input = dialog.querySelector(".bk-cmdk__input");
const shown = () => [...dialog.querySelectorAll("[role=option]")].filter((o) => !o.hidden).map((o) => o.querySelector("span").textContent);
const selected = () => dialog.querySelector("[aria-selected=true] span")?.textContent;
const type = (s) => fill(w, input, s);
const followed = [];
doc.addEventListener("click", (e) => { const a = e.target.closest("a[href]"); if (a) { e.preventDefault(); followed.push(a.getAttribute("href")); } });
const commands = [];
w.brick.on("command", (c) => commands.push(c));

assert.equal(doc.querySelector(".bk-cmdk__hotkey").textContent, "Ctrl K", "the hint names Ctrl off a Mac");
key(w, doc.body, "k", { ctrlKey: true });
assert.ok(dialog.open, "Ctrl+K opens");
assert.equal(doc.activeElement, input, "the search box has focus");
assert.deepEqual(shown(), ["Dashboard", "Contacts", "Tasks", "Building apps", "Toggle theme", "Sign out", "DataTable", "Table layouts"]);
assert.equal(selected(), "Dashboard", "the first item is selected");
assert.equal(input.getAttribute("aria-activedescendant"), dialog.querySelector("[aria-selected=true]").id);

type("con");
assert.deepEqual(shown(), ["Contacts"], "filters by label");
assert.equal(dialog.querySelectorAll(".bk-cmdk__group")[1].hidden, true, "an empty group hides");
type("stats");
assert.deepEqual(shown(), ["Dashboard"], "filters by keywords");
type("actions");
assert.deepEqual(shown(), ["Toggle theme", "Sign out"], "filters by group");
// Ranking: a label that starts with the query beats a label containing it,
// which beats a keyword-only match; groups follow their best option, and
// ties keep the server's order.
type("data tab");
assert.deepEqual(shown(), ["DataTable", "Table layouts", "Building apps"], "the label-prefix match first; keyword-only matches after it");
assert.equal(selected(), "DataTable", "the best match is selected");
type("tab");
assert.deepEqual(shown(), ["Table layouts", "DataTable", "Building apps"], "prefix before substring; the keyword match last");
type("");
assert.deepEqual(shown(), ["Dashboard", "Contacts", "Tasks", "Building apps", "Toggle theme", "Sign out", "DataTable", "Table layouts"], "an empty query restores the order");
type("zzz");
assert.deepEqual(shown(), []);
assert.equal(dialog.querySelector(".bk-cmdk__empty").hidden, false, "no results says so");

type("");
key(w, input, "ArrowDown");
key(w, input, "ArrowDown");
assert.equal(selected(), "Tasks");
key(w, input, "ArrowUp");
key(w, input, "ArrowUp");
key(w, input, "ArrowUp");
assert.equal(selected(), "Table layouts", "ArrowUp wraps");
type("tas");
key(w, input, "Enter");
assert.deepEqual(followed, ["/tasks/"], "Enter follows the selected link");
assert.equal(dialog.open, false, "and closes the palette");

key(w, doc.body, "k", { metaKey: true });
assert.ok(dialog.open, "⌘K opens");
assert.equal(input.value, "", "a fresh search each time");
type("theme");
key(w, input, "Enter");
assert.equal(JSON.stringify(commands), JSON.stringify([{ value: "theme", label: "Toggle theme" }]), "a command item emits");
assert.equal(dialog.open, false);

click(w, doc.querySelector(".bk-cmdk__trigger"));
assert.ok(dialog.open, "the trigger opens it");
key(w, input, "k", { ctrlKey: true });
assert.equal(dialog.open, false, "the hotkey toggles it closed");
click(w, doc.querySelector(".bk-cmdk__trigger"));
key(w, dialog, "Escape");
assert.equal(dialog.open, false, "Escape closes");
w.close();
console.log("kit_cmdk ok");
