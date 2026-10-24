// Drives the second navigation batch's client modules in jsdom: TimePicker,
// MultiSelect, Editor and Fab, on the page kit_test.go's runKitJS renders
// (TestKitNav2InBrowser in kit_nav2_test.go).
import assert from "node:assert/strict";
import { load, tick, click, key } from "./kit_harness.mjs";
import { loadESM } from "./kit_esm.mjs";

const { w, doc, mount } = load("TimePicker", "MultiSelect", "Editor", "Fab");
// jsdom has no execCommand; record calls instead.
const commands = [];
doc.execCommand = (c, _, v) => { commands.push(v === undefined ? c : c + ":" + v); return true; };
doc.queryCommandState = () => false;
doc.queryCommandValue = () => "";
// Popover exports place() and the pickers import it, so those modules load
// through kit_esm.mjs (Popover first); Editor and Fab stay plain scripts.
loadESM(w, "Popover", "TimePicker", "MultiSelect");
for (const m of ["Editor", "Fab"]) mount(m);
await tick();
const events = [];
const on = (el) => ["input", "change"].forEach((t) => el.addEventListener(t, () => events.push(t)));

// TimePicker: the button appears, opens the list on the chosen time,
// arrows move, Enter picks and fires input+change, Escape closes.
{
  const el = doc.querySelector(".bk-timepicker");
  const input = el.querySelector("input[type=time]"), btn = el.querySelector(".bk-timepicker__toggle"), list = el.querySelector("[role=listbox]");
  on(input);
  assert.equal(btn.hidden, false, "the list button shows once enhanced");
  click(w, btn);
  assert.equal(list.hidden, false);
  assert.equal(btn.getAttribute("aria-expanded"), "true");
  const active = () => list.querySelector(".bk-timepicker__option--active").dataset.value;
  assert.equal(active(), "09:30", "opens on the current value");
  key(w, list, "ArrowDown");
  assert.equal(active(), "10:00");
  key(w, list, "Home");
  assert.equal(active(), "09:00");
  key(w, list, "Enter");
  assert.equal(input.value, "09:00", "Enter picks");
  assert.deepEqual(events.splice(0), ["input", "change"]);
  assert.equal(list.hidden, true, "and closes");
  click(w, btn);
  key(w, list, "Escape");
  assert.equal(list.hidden, true, "Escape closes");
  assert.equal(input.value, "09:00", "without changing the value");
}

// MultiSelect: the native select hides; chips come from the template; a
// click toggles, Backspace removes the last chip, Select all / Clear act
// on the options shown, and each change fires input+change on the select.
{
  const el = doc.querySelector(".bk-multiselect");
  const select = el.querySelector("select"), input = el.querySelector("[role=combobox]"), list = el.querySelector(".bk-multiselect__list");
  on(select);
  const chips = () => [...el.querySelectorAll(".bk-multiselect__chip-text")].map((c) => c.textContent);
  const chosen = () => [...select.selectedOptions].map((o) => o.value);
  assert.equal(select.hidden, true);
  assert.deepEqual(chips(), ["b"], "the server's value is a chip");
  const down = (t) => t.dispatchEvent(new w.MouseEvent("mousedown", { bubbles: true, cancelable: true }));
  click(w, input);
  assert.equal(list.hidden, false);
  down(list.querySelector('[data-value="cherry"]'));
  assert.deepEqual(chosen(), ["b", "cherry"]);
  assert.deepEqual(chips(), ["b", "cherry"]);
  assert.deepEqual(events.splice(0), ["input", "change"]);
  key(w, input, "Backspace");
  assert.deepEqual(chosen(), ["b"], "Backspace in the empty filter removes the last chip");
  input.value = "ap";
  input.dispatchEvent(new w.Event("input", { bubbles: true }));
  assert.deepEqual([...list.querySelectorAll("[role=option]")].filter((o) => !o.hidden).map((o) => o.dataset.value), ["apple"], "typing filters");
  key(w, input, "Enter");
  assert.deepEqual(chosen(), ["apple", "b"], "Enter toggles the active option");
  input.value = "";
  input.dispatchEvent(new w.Event("input", { bubbles: true }));
  down(list.querySelector('[data-bk-all="on"]'));
  assert.deepEqual(chosen(), ["apple", "b", "cherry"], "Select all");
  down(list.querySelector('[data-bk-all="off"]'));
  assert.deepEqual(chosen(), [], "Clear");
  select.options[1].selected = true;
  click(w, el.querySelector(".bk-multiselect__chips") && input);
  key(w, input, "Escape");
  assert.equal(list.hidden, true, "Escape closes");
}

// Editor: the textarea hides, its HTML is cleaned into the editable box
// (no script, no handler, no javascript: link), tools run commands and the
// box writes cleaned HTML back.
{
  const el = doc.querySelector(".bk-editor");
  const src = el.querySelector("textarea"), box = el.querySelector(".bk-editor__content"), tools = [...el.querySelectorAll("[data-tool]")];
  on(src);
  assert.equal(src.hidden, true);
  assert.equal(box.getAttribute("contenteditable"), "true");
  assert.equal(box.getAttribute("aria-label"), "Write something…", "without a label the box is named by its placeholder");
  assert.equal(box.innerHTML, '<p>Hi <b>there</b> <a>x</a> <a href="https://ok.test/">ok</a></p>', "cleaned on the way in");
  click(w, tools.find((b) => b.dataset.tool === "bold"));
  click(w, tools.find((b) => b.dataset.tool === "numbers"));
  click(w, tools.find((b) => b.dataset.tool === "heading"));
  assert.deepEqual(commands.slice(-3), ["bold", "insertOrderedList", "formatBlock:<h2>"]);
  box.innerHTML = '<h2 style="color:red">Title</h2><p>x<img src=y onerror=bad()><iframe></iframe></p>';
  box.dispatchEvent(new w.Event("input", { bubbles: true }));
  assert.equal(src.value, "<h2>Title</h2><p>x</p>", "cleaned on the way out");
  assert.ok(events.includes("input") && events.includes("change"));
  box.innerHTML = "";
  box.dispatchEvent(new w.Event("input", { bubbles: true }));
  assert.equal(src.value, "", "an empty editor submits nothing");
  // The toolbar is one tab stop; arrows move along it.
  assert.deepEqual(tools.map((b) => b.tabIndex).slice(0, 3), [0, -1, -1]);
  tools[0].focus();
  key(w, tools[0], "ArrowRight");
  assert.equal(doc.activeElement, tools[1]);
  key(w, tools[1], "End");
  assert.equal(doc.activeElement, tools[tools.length - 1]);
}

// Inside a Field the box is named by the Field's label, which labels the
// hidden textarea.
{
  const box = doc.querySelectorAll(".bk-editor__content")[1];
  const ids = (box.getAttribute("aria-labelledby") || "").split(" ").filter(Boolean);
  assert.equal(box.hasAttribute("aria-label"), false);
  assert.deepEqual(ids.map((id) => doc.getElementById(id).textContent.trim()), ["Notes"], "named by the Field label");
}

// Fab: Escape and a click outside close the speed dial.
{
  const dial = doc.querySelector(".bk-fab__dial");
  dial.open = true;
  key(w, dial, "Escape");
  assert.equal(dial.open, false, "Escape closes");
  dial.open = true;
  click(w, doc.body);
  assert.equal(dial.open, false, "a click outside closes");
}
w.close();
console.log("kit_nav2 ok");
