// Drives the kit's CodeBlock, Countdown, Carousel and Diff in jsdom over
// the page kit_data_test.go renders.
import assert from "node:assert/strict";
import { load, click, tick } from "./kit_harness.mjs";

const { w, doc, mount } = load();
let copied = null;
Object.defineProperty(w.navigator, "clipboard", { value: { writeText: async (t) => { copied = t; } } });
const scrolls = [];
w.HTMLElement.prototype.scrollBy = function (o) { scrolls.push(["by", o.left]); };
w.HTMLElement.prototype.scrollTo = function (o) { scrolls.push(["to", o.left]); };
let done = 0;
doc.addEventListener("bk-done", () => { done++; });
["CodeBlock", "Countdown", "Carousel", "Diff"].forEach(mount);

// CodeBlock: the button appears and copies the code without line numbers.
const copy = doc.querySelector(".bk-code-block__copy");
assert.equal(copy.hidden, false);
click(w, copy);
await tick();
assert.equal(copied, "a := 1\nb := 2");
assert.equal(copy.dataset.copied, "true");
assert.equal(doc.querySelector(".bk-code-block__status").textContent, "Copied");

// Countdown: the boxes replace the date; a past target shows done.
const [future, past] = doc.querySelectorAll(".bk-countdown");
assert.equal(future.querySelector(".bk-countdown__boxes").hidden, false);
assert.equal(future.querySelector(".bk-countdown__until").hidden, true);
assert.ok(Number(future.querySelector('[data-unit="d"]').textContent) > 20000, "days until 2099");
assert.match(future.querySelector('[data-unit="s"]').textContent, /^\d\d$/);
assert.equal(past.querySelector(".bk-countdown__boxes").hidden, true);
assert.equal(past.querySelector(".bk-countdown__done").hidden, false);
assert.equal(past.querySelector(".bk-countdown__done").textContent, "Over");
assert.equal(done, 1, "bk-done fired once, for the past target");

// Carousel: a dot per slide, the first current; dots jump, buttons step.
const car = doc.querySelector(".bk-carousel");
const dots = [...car.querySelectorAll(".bk-carousel__dot")];
assert.equal(dots.length, 3);
assert.equal(car.querySelector(".bk-carousel__dots").hidden, false);
assert.equal(dots[0].getAttribute("aria-current"), "true");
assert.equal(dots[1].getAttribute("aria-label"), "Go to slide 2 of 3");
click(w, dots[2]);
click(w, car.querySelector(".bk-next"));
assert.deepEqual(scrolls.map((s) => s[0]), ["to", "by"]);

// Diff: the range input appears and moves the divider.
const diff = doc.querySelector(".bk-diff");
const range = diff.querySelector(".bk-diff__range");
assert.equal(range.hidden, false);
assert.equal(diff.style.getPropertyValue("--bk-diff-pos"), "30%");
range.value = "70";
range.dispatchEvent(new w.Event("input", { bubbles: true }));
assert.equal(diff.style.getPropertyValue("--bk-diff-pos"), "70%");
w.close();
console.log("kit_widgets ok");
