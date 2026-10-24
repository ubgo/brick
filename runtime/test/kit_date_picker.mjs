// Drives the kit's DatePicker in jsdom (see kit_forms_test.go): the
// popover fills its grid for the value's month, the keyboard moves by day,
// week and month, days outside min/max are disabled, and a pick writes the
// ISO date and fires events.
import assert from "node:assert/strict";
import { load, tick, key, click } from "./kit_harness.mjs";
import { loadESM } from "./kit_esm.mjs";

const { w, doc } = load();
// Popover exports place() and DatePicker imports it, so both load through
// kit_esm.mjs (Popover first), not the harness's plain eval.
loadESM(w, "Popover", "DatePicker");
await tick();
const input = doc.querySelector("input[name=due]");
const btn = doc.querySelector(".bk-datepicker__toggle");
const pop = doc.querySelector(".bk-datepicker__pop");
const title = doc.querySelector(".bk-datepicker__title");
const days = [...doc.querySelectorAll(".bk-datepicker__day")];
const day = (iso) => days.find((d) => d.dataset.date === iso);
let changes = 0;
input.addEventListener("change", () => changes++);

assert.equal(btn.hidden, false, "the calendar button shows once enhanced");
assert.equal(pop.hidden, true);
click(w, btn);
assert.equal(pop.hidden, false, "the button opens the popover");
assert.equal(btn.getAttribute("aria-expanded"), "true");
assert.equal(title.textContent, "February 2026", "the value's month");
assert.equal(days[0].dataset.date, "2026-02-01", "Feb 2026 starts on a Sunday");
assert.equal(day("2026-02-14").getAttribute("aria-selected"), "true", "the value is selected");
assert.equal(doc.activeElement, day("2026-02-14"), "focus goes to it");
assert.equal(day("2026-02-02").disabled, true, "before min is disabled");
assert.equal(day("2026-02-03").disabled, false);
key(w, doc.activeElement, "ArrowRight");
assert.equal(doc.activeElement.dataset.date, "2026-02-15", "ArrowRight: next day");
key(w, doc.activeElement, "ArrowDown");
assert.equal(doc.activeElement.dataset.date, "2026-02-22", "ArrowDown: next week");
key(w, doc.activeElement, "PageDown");
assert.equal(title.textContent, "March 2026", "PageDown: next month");
assert.equal(doc.activeElement.dataset.date, "2026-03-22");
assert.equal(day("2026-03-21").disabled, true, "after max is disabled");
key(w, doc.activeElement, "ArrowUp");
key(w, doc.activeElement, "Enter");
assert.equal(input.value, "2026-03-15", "Enter picks the ISO date");
assert.equal(changes, 1, "and fires change");
assert.equal(pop.hidden, true, "and closes");
assert.equal(doc.activeElement, btn, "focus returns to the button");
click(w, btn);
click(w, doc.querySelector("[data-nav='-1']"));
assert.equal(title.textContent, "February 2026", "the previous-month button");
key(w, doc.activeElement, "Escape");
assert.equal(pop.hidden, true, "Escape closes");
assert.equal(input.value, "2026-03-15", "without changing the value");
w.close();
console.log("kit_date_picker ok");
