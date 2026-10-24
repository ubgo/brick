// Drives the kit's Sortable in jsdom (see kit_overlays2_test.go): Space
// lifts, arrows move, Space drops (announced), Escape puts it back; a drop
// dispatches bk-sort and calls the action with [id, newIndex]; a pointer
// drag reorders past neighbours' midpoints.
import assert from "node:assert/strict";
import { load, tick, key } from "./kit_harness.mjs";
import { loadESM, pointer, rect } from "./kit_esm.mjs";

const { w, doc } = load();
const calls = [];
loadESM(w, "Sortable");
w.brick.action = (el, name, args) => { calls.push([name, ...args]); return Promise.resolve(); };
await tick();
const root = doc.querySelector(".bk-sortable");
const list = root.querySelector(".bk-sortable__list");
const live = root.querySelector(".bk-sortable__live");
const order = () => [...list.children].map((li) => li.dataset.id).join(",");
const handle = (id) => list.querySelector(`[data-id="${id}"] .bk-sortable__handle`);
const events = [];
root.addEventListener("bk-sort", (e) => events.push(`${e.detail.id}:${e.detail.from}->${e.detail.to}`));

assert.ok(root.classList.contains("bk-sortable--ready"), "enhanced");
assert.equal(order(), "1,2,3,4");
handle(1).focus();
key(w, handle(1), " ");
assert.match(live.textContent, /Picked up Write the post, position 1 of 4/);
key(w, handle(1), "ArrowDown");
key(w, doc.activeElement, "ArrowDown");
assert.equal(order(), "2,3,1,4", "arrows move it");
assert.equal(doc.activeElement, handle(1), "focus stays on it");
assert.match(live.textContent, /position 3 of 4/);
key(w, doc.activeElement, " ");
assert.match(live.textContent, /Dropped Write the post at position 3 of 4/);
assert.deepEqual(events, ["1:0->2"], "bk-sort");
assert.equal(JSON.stringify(calls), JSON.stringify([["move", 1, 2]]), "the action gets [id, newIndex], numbers as numbers");

key(w, handle(4), "Enter");
key(w, handle(4), "ArrowUp");
key(w, doc.activeElement, "Escape");
assert.equal(order(), "2,3,1,4", "Escape puts it back");
assert.match(live.textContent, /back at position 4 of 4/);
assert.equal(calls.length, 1, "a cancel calls nothing");

// Pointer: items 40px tall; dragging item 2 below item 3's middle.
[...list.children].forEach((li, i) => rect(li, { top: i * 44, height: 40, width: 300 }));
pointer(w, handle(2), "pointerdown", { y: 20 });
pointer(w, handle(2), "pointermove", { y: 22 });
assert.equal(order(), "2,3,1,4", "a tiny move is a click, not a drag");
pointer(w, handle(2), "pointermove", { y: 70 });
assert.ok(list.querySelector('[data-id="2"]').classList.contains("bk-sortable__item--dragging"));
[...list.children].forEach((li, i) => rect(li, { top: i * 44, height: 40, width: 300 }));
assert.equal(order(), "3,2,1,4", "past the neighbour's middle it moves");
pointer(w, handle(2), "pointerup", { y: 70 });
assert.deepEqual(events.slice(-1), ["2:0->1"]);
assert.equal(JSON.stringify(calls.slice(-1)), JSON.stringify([["move", 2, 1]]));
assert.equal(root.querySelector(".bk-sortable__item--dragging"), null);

// A plain <li data-id> list: the whole item is the grip.
const plain = doc.getElementById("plain");
const li = plain.querySelector("[data-id=b]");
assert.equal(li.tabIndex, 0);
li.focus();
key(w, li, " ");
key(w, li, "ArrowRight");
key(w, doc.activeElement, " ");
assert.equal([...plain.querySelectorAll("[data-id]")].map((x) => x.dataset.id).join(","), "a,c,b", "horizontal uses ArrowRight");

// Grouped lists: "To do" (11, 12), "Done" (13) and an empty "Later" trade items.
const col = (id) => doc.getElementById(id);
const ids = (id) => [...col(id).querySelectorAll(".bk-sortable__list > [data-id]")].map((x) => x.dataset.id).join(",");
const cross = [];
doc.addEventListener("bk-sort", (e) => { if (e.detail.toList) cross.push(e.detail); });
const h = (id) => doc.querySelector(`[data-id="${id}"] .bk-sortable__handle`);
calls.length = 0;
// Keyboard: lift 11, ArrowRight hops to Done at the same position, Space drops.
h(11).focus();
key(w, h(11), " ");
assert.match(col("todo").querySelector(".bk-sortable__live").textContent, /ArrowLeft and ArrowRight to move it to another list/);
key(w, h(11), "ArrowLeft");
assert.equal(ids("todo"), "11,12", "no list before the first");
key(w, doc.activeElement, "ArrowRight");
assert.equal(ids("todo"), "12");
assert.equal(ids("done"), "11,13", "the hop keeps the position");
assert.equal(doc.activeElement, h(11), "focus stays on the lifted item");
assert.match(col("done").querySelector(".bk-sortable__live").textContent, /Eleven, position 1 of 2 in Done/);
key(w, doc.activeElement, "ArrowDown");
assert.equal(ids("done"), "13,11", "then the arrows move it within its new list");
key(w, doc.activeElement, " ");
assert.equal(JSON.stringify(calls), JSON.stringify([["move", 11, "done", 1]]), "a grouped drop sends [id, list, index]");
assert.deepEqual(JSON.parse(JSON.stringify(cross.map(({ id, fromList, toList, from, to }) => ({ id, fromList, toList, from, to })))), [{ id: 11, fromList: "todo", toList: "done", from: 0, to: 1 }]);
// Two hops into the empty list, then Escape: back where it started.
key(w, h(12), "Enter");
key(w, h(12), "ArrowRight");
key(w, doc.activeElement, "ArrowRight");
assert.equal(ids("empty"), "12", "an empty list takes it");
key(w, doc.activeElement, "Escape");
assert.equal(ids("todo"), "12", "Escape returns it to its own list");
assert.equal(ids("empty"), "");
assert.equal(calls.length, 1, "a cancel calls nothing");

// Pointer: columns side by side; drag 12 from To do onto Done, above 13.
rect(col("todo").querySelector(".bk-sortable__list"), { left: 0, top: 0, width: 200, height: 300 });
rect(col("done").querySelector(".bk-sortable__list"), { left: 300, top: 0, width: 200, height: 300 });
rect(col("empty").querySelector(".bk-sortable__list"), { left: 600, top: 0, width: 200, height: 300 });
const place = () => ["todo", "done", "empty"].forEach((c, k) => [...col(c).querySelectorAll(".bk-sortable__list > [data-id]")].forEach((li, i) => rect(li, { left: k * 300, top: i * 44, width: 200, height: 40 })));
place();
pointer(w, h(12), "pointerdown", { x: 20, y: 20 });
pointer(w, h(12), "pointermove", { x: 320, y: 10 });
place();
assert.equal(ids("done"), "12,13,11", "above 13's middle, it lands first");
assert.ok(col("done").classList.contains("bk-sortable--dragging") && col("todo").classList.contains("bk-sortable--dragging"), "the whole group shows the drag");
pointer(w, h(12), "pointermove", { x: 320, y: 70 });
place();
assert.equal(ids("done"), "13,12,11", "and then follows the pointer within Done");
pointer(w, h(12), "pointerup", { x: 320, y: 70 });
assert.equal(JSON.stringify(calls.slice(-1)), JSON.stringify([["move", 12, "done", 1]]));
assert.equal(cross.at(-1).fromList, "todo");
assert.equal(doc.querySelector(".bk-sortable--dragging"), null);
w.close();
console.log("kit_sortable ok");
