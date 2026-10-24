// Drives the Task board's drag and drop in jsdom: the page the showcase
// rendered, with the real runtime, the kit Sortable module (each column is
// a Sortable in group "board") and the TaskBoard module, written to the
// directory in argv[2] by TestTasksDragInBrowser. brick.action is replaced
// by a recorder, so the test sees exactly what a drop would send.
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { createRequire } from "node:module";
import assert from "node:assert/strict";

const { JSDOM, VirtualConsole } = createRequire(import.meta.url)(process.env.JSDOM_PATH);
const dir = process.argv[2];
const dom = new JSDOM(readFileSync(join(dir, "page.html"), "utf8"), { url: "https://site.test/apps/tasks/", runScripts: "outside-only", pretendToBeVisual: true, virtualConsole: new VirtualConsole() });
const w = dom.window, doc = w.document;
w.CSS = w.CSS || { escape: (s) => String(s) };
w.eval(readFileSync(join(dir, "runtime.js"), "utf8"));
const calls = [];
w.brick.action = (el, name, args) => { calls.push([name, JSON.parse(JSON.stringify(args)), !!el.closest("[data-brick-state]")]); return Promise.resolve(); };
w.eval(readFileSync(join(dir, "Sortable.js"), "utf8"));
w.eval(readFileSync(join(dir, "TaskBoard.js"), "utf8"));
await new Promise((r) => setTimeout(r, 30));

const col = (s) => doc.querySelector(`.tb-col .bk-sortable[data-list="${s}"]`);
const ids = (s) => [...col(s).querySelectorAll("[data-id]")].map((c) => c.dataset.id).join(",");
const rect = (el, left, top, width, height) => { el.getBoundingClientRect = () => ({ left, top, width, height, right: left + width, bottom: top + height }); };
// Lay the three columns side by side, cards 100px tall.
const layout = () => ["todo", "doing", "done"].forEach((s, k) => {
  rect(col(s).querySelector(".bk-sortable__list"), k * 300, 0, 280, 600);
  [...col(s).querySelectorAll("[data-id]")].forEach((c, i) => rect(c, k * 300, i * 100, 280, 90));
});
layout();
const pointer = (el, type, x, y) => {
  const e = new w.MouseEvent(type, { bubbles: true, cancelable: true, clientX: x, clientY: y, button: 0 });
  Object.defineProperty(e, "pointerId", { value: 1 });
  el.dispatchEvent(e);
};
assert.ok(col("todo").classList.contains("bk-sortable--ready"), "the columns are enhanced");
const card = col("doing").querySelector('[data-id="2"]');
assert.equal(card.tabIndex, 0, "a whole card is the grip");
pointer(card.querySelector(".tb-card__title"), "pointerdown", 310, 40);
pointer(card, "pointermove", 20, 120);
layout();
assert.equal(ids("todo"), "1,2,4,7,10", "the card follows the pointer into To do, above T-4's middle");
pointer(card, "pointerup", 20, 120);
assert.deepEqual(calls, [["move", [2, "todo", 1], true]], "the drop calls move(id, status, position) inside the board");
assert.equal(doc.querySelector(".bk-sortable__item--dragging"), null);

// The card's menu is not a grip: a press on it starts no drag.
pointer(col("todo").querySelector('[data-id="4"] summary'), "pointerdown", 20, 150);
pointer(doc.body, "pointermove", 620, 150);
pointer(doc.body, "pointerup", 620, 150);
assert.equal(calls.length, 1, "pressing the menu drags nothing");

// Keyboard: lift T-7, hop right twice to Done, one up, drop.
const t7 = col("todo").querySelector('[data-id="7"]');
t7.focus();
const key = (el, k) => el.dispatchEvent(new w.KeyboardEvent("keydown", { key: k, bubbles: true, cancelable: true }));
key(t7, " ");
key(t7, "ArrowRight");
key(t7, "ArrowRight");
key(t7, "ArrowUp");
key(t7, " ");
assert.deepEqual(calls[1], ["move", [7, "done", 2], true], "the keyboard sends the same shape");
assert.equal(doc.activeElement, t7, "focus stays on the card");
w.close();
console.log("tasks_drag ok");
