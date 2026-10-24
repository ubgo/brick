// Drives the kit's DataTable in jsdom over the page kit_data_test.go
// renders: 23 rows (P1…P23, n = i*7 mod 23, team by i mod 3), page size 10.
import assert from "node:assert/strict";
import { load, click, fill } from "./kit_harness.mjs";

const { w, doc, mount } = load();
mount("DataTable");
const el = doc.querySelector(".bk-dt");
const range = () => el.querySelector(".bk-dt__range").textContent;
const shown = () => [...el.querySelectorAll("tbody > tr:not(.bk-dt__none)")].filter((tr) => !tr.hidden);
const names = () => shown().map((tr) => tr.querySelector('td[data-col="0"]').textContent);
const pages = () => [...el.querySelectorAll(".bk-dt__page")].map((b) => (b.getAttribute("aria-current") ? `[${b.textContent}]` : b.textContent)).join(" ");
const input = (node, value, type = "input") => fill(w, node, value, type);

assert.equal(el.querySelector(".bk-dt__toolbar").hidden, false, "toolbar shown");
assert.equal(el.querySelector(".bk-dt__footer").hidden, false, "footer shown");
assert.equal(shown().length, 10, "one page of rows");
assert.equal(range(), "1–10 of 23");
assert.equal(pages(), "« ‹ [1] 2 3 › »");
assert.ok(el.querySelector(".bk-dt__page[aria-label='Previous page']").disabled, "previous disabled on page 1");

// Paginate.
click(w, el.querySelector('.bk-dt__page[data-page="3"]:not([aria-label])'));
assert.equal(range(), "21–23 of 23");
assert.equal(shown().length, 3);
assert.ok(el.querySelector(".bk-dt__page[aria-label='Next page']").disabled, "next disabled on the last page");
click(w, el.querySelector(".bk-dt__page[aria-label='First page']"));
assert.equal(range(), "1–10 of 23");

// Page size.
input(el.querySelector(".bk-dt__size select"), "25", "change");
assert.equal(shown().length, 23);
assert.equal(pages(), "« ‹ [1] › »");
input(el.querySelector(".bk-dt__size select"), "5", "change");
assert.equal(range(), "1–5 of 23");
assert.equal(pages(), "« ‹ [1] 2 3 4 5 › »", "five pages, no gap");

// Sort: text in natural order, then descending; numbers as numbers.
const [nameTh, nTh] = el.querySelectorAll("thead th[data-col]");
click(w, nameTh.querySelector(".bk-dt__sort"));
assert.equal(nameTh.getAttribute("aria-sort"), "ascending");
assert.deepEqual(names(), ["P1", "P2", "P3", "P4", "P5"]);
click(w, nameTh.querySelector(".bk-dt__sort"));
assert.equal(nameTh.getAttribute("aria-sort"), "descending");
assert.deepEqual(names(), ["P23", "P22", "P21", "P20", "P19"]);
click(w, nTh.querySelector(".bk-dt__sort"));
assert.equal(nameTh.hasAttribute("aria-sort"), false, "one sorted column");
const ns = shown().map((tr) => Number(tr.querySelector('td[data-col="1"]').textContent));
assert.deepEqual(ns, [0, 1, 2, 3, 4], "numeric ascending");

// Filter across columns, resetting to page 1.
input(el.querySelector(".bk-dt__search input"), "BLUE");
assert.equal(range(), "1–5 of 8", "8 blue rows");
input(el.querySelector(".bk-dt__size select"), "10", "change");
assert.equal(shown().length, 8);
assert.ok(shown().every((tr) => tr.textContent.includes("blue")));

// Select all on the page, then one off: indeterminate, count and event.
let keys = null;
el.addEventListener("bk-select", (e) => { keys = e.detail.keys; });
const all = el.querySelector(".bk-dt__all");
assert.equal(all.hidden, false);
click(w, all);
assert.equal(keys.length, 8);
assert.equal(el.querySelector(".bk-dt__selected").textContent, "8 selected");
assert.equal(shown().filter((tr) => tr.classList.contains("bk-dt__row--selected")).length, 8);
click(w, shown()[0].querySelector(".bk-dt__row-check"));
assert.equal(keys.length, 7);
assert.equal(all.indeterminate, true);
assert.equal(all.checked, false);

// Nothing matches: the empty row shows.
input(el.querySelector(".bk-dt__search input"), "zzz");
assert.equal(shown().length, 0);
assert.equal(el.querySelector(".bk-dt__none").hidden, false);
assert.equal(range(), "0 of 23");
w.close();
console.log("kit_datatable ok");
