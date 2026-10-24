// Drives the chart tooltips in jsdom: BarChart, LineChart and DonutChart
// share one tooltip. The server's native tooltip (a title attribute, or an
// SVG <title>) becomes the rich one: hovering a mark or arrowing through
// the focused chart shows its category, then one row per series with the
// value first; Escape and leaving hide it. See runKitJS in kit_test.go.
import assert from "node:assert/strict";
import { load, key } from "./kit_harness.mjs";

const { w, doc, mount } = load("BarChart", "LineChart", "DonutChart");
for (const name of ["BarChart", "LineChart", "DonutChart"]) mount(name);

const move = (el) => el.dispatchEvent(new w.MouseEvent("pointermove", { bubbles: true, clientX: 10, clientY: 10 }));
const rows = (tip) => [...tip.querySelectorAll(".bk-chart__tip-row")].map((r) => [r.querySelector("b").textContent, r.querySelector("span").textContent, r.querySelector("i").dataset.tone]);

// BarChart: two series, hover the second category.
const bar = doc.querySelector(".bk-bar-chart");
const cols = [...bar.querySelectorAll(".bk-bar-chart__col")];
const btip = bar.querySelector(".bk-chart__tip");
assert.equal(cols[0].hasAttribute("title"), false, "the native tooltip is replaced");
assert.equal(btip.hidden, true, "hidden until hover");
move(cols[1].querySelector(".bk-bar-chart__bar"));
assert.equal(btip.hidden, false, "hover shows it");
assert.equal(btip.querySelector(".bk-chart__tip-head").textContent, "Feb");
assert.deepEqual(rows(btip), [["$30", "Web", "1"], ["$5", "App", "danger"]], "value first, then series, with its tone");
assert.ok(cols[1].classList.contains("bk-chart--on") && !cols[0].classList.contains("bk-chart--on"), "the hovered column is marked");
bar.querySelector(".bk-chart__plot").dispatchEvent(new w.MouseEvent("pointerleave"));
assert.equal(btip.hidden, true, "leaving hides it");

// Keyboard: the plot is focusable; arrows move, wrap, and Escape hides.
const plot = bar.querySelector(".bk-chart__plot");
assert.equal(plot.tabIndex, 0, "focusable once enhanced");
key(w, plot, "ArrowLeft");
assert.equal(btip.querySelector(".bk-chart__tip-head").textContent, "Mar", "ArrowLeft from nothing starts at the end");
key(w, plot, "ArrowRight");
assert.equal(btip.querySelector(".bk-chart__tip-head").textContent, "Jan", "ArrowRight wraps");
key(w, plot, "Escape");
assert.equal(btip.hidden, true, "Escape hides it");

// LineChart: the crosshair column lists every series.
const line = doc.querySelector(".bk-line-chart");
const lcol = line.querySelectorAll(".bk-line-chart__col")[2];
move(lcol);
const ltip = line.querySelector(".bk-chart__tip");
assert.equal(ltip.querySelector(".bk-chart__tip-head").textContent, "Mar");
assert.deepEqual(rows(ltip).map((r) => r[0]), ["12", "7"]);

// DonutChart: the SVG <title> becomes the tooltip.
const donut = doc.querySelector(".bk-donut-chart");
const seg = donut.querySelectorAll(".bk-donut-chart__seg")[1];
assert.equal(seg.querySelector("title"), null, "the <title> is moved out");
move(seg);
const dtip = donut.querySelector(".bk-chart__tip");
assert.equal(dtip.querySelector(".bk-chart__tip-head").textContent, "Safari");
assert.deepEqual(rows(dtip), [["25 (25%)", "Visits", "2"]]);
w.close();
console.log("kit_charts ok");
