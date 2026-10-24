// Exercises runtime.js against a minimal fake DOM. Run by runtime_test.go.
import { readFileSync } from "node:fs";
import vm from "node:vm";
import assert from "node:assert/strict";

class El {
  constructor(attrs = {}) { this.attrs = { ...attrs }; this.children = []; this.parent = null; this.nodeType = 1; }
  getAttribute(n) { return n in this.attrs ? this.attrs[n] : null; }
  get isConnected() { let n = this; while (n.parent) n = n.parent; return n === doc.documentElement; }
  matches(sel) { return sel === "[data-brick-type]" && "data-brick-type" in this.attrs; }
  querySelectorAll(sel) { const out = []; const walk = (n) => n.children.forEach((c) => { if (c.matches(sel)) out.push(c); walk(c); }); walk(this); return out; }
}
const doc = { documentElement: new El() };
doc.querySelectorAll = (s) => doc.documentElement.querySelectorAll(s);
doc.querySelector = (s) => doc.querySelectorAll(s)[0] || null; // the brick-config meta; none here
doc.addEventListener = () => {}; // action events; exercised in test/actions_test.mjs
const observers = [];
class MutationObserver { constructor(cb) { this.cb = cb; observers.push(this); } observe() {} }
const append = (parent, child) => { child.parent = parent; parent.children.push(child); observers.forEach((o) => o.cb([{ addedNodes: [child], removedNodes: [] }])); };
const remove = (child) => { const p = child.parent; p.children = p.children.filter((c) => c !== child); child.parent = null; observers.forEach((o) => o.cb([{ addedNodes: [], removedNodes: [child] }])); };
const ios = [];
class IntersectionObserver { constructor(cb) { this.cb = cb; ios.push(this); } observe(el) { this.el = el; } disconnect() { this.off = true; } }
const idle = [];

const win = { document: doc, MutationObserver, IntersectionObserver, requestIdleCallback: (f) => idle.push(f), EventTarget, CustomEvent };
win.window = win;
vm.createContext(win);

// An element on the page before the runtime loads.
const early = new El({ "data-brick-type": "Acc", "data-brick-props": '{"open":true}' });
doc.documentElement.children.push(early); early.parent = doc.documentElement;

vm.runInContext(readFileSync(new URL("./runtime.js", import.meta.url), "utf8"), win);
const brick = win.brick;
assert.ok(brick, "window.brick defined");

const calls = [];
let cleaned = 0;
brick.mount("Acc", (el, props) => { calls.push(props); return () => cleaned++; });
assert.equal(JSON.stringify(calls), '[{"open":true}]', "existing element mounted with its props");

const late = new El({ "data-brick-type": "Acc" });
append(doc.documentElement, late);
assert.equal(calls.length, 2, "inserted element mounted");
append(doc.documentElement, new El({ "data-brick-type": "Other" }));
assert.equal(calls.length, 2, "another type is not mounted");

remove(late);
assert.equal(cleaned, 1, "cleanup ran on removal");
append(doc.documentElement, late);
assert.equal(calls.length, 3, "re-inserted element mounts again");

// Deferred mounting.
let lazy = 0;
brick.mount("Lazy", () => { lazy++; });
const vis = new El({ "data-brick-type": "Lazy", "data-brick-mount": "visible" });
append(doc.documentElement, vis);
assert.equal(lazy, 0, "visible waits");
ios.at(-1).cb([{ isIntersecting: true }]);
assert.equal(lazy, 1, "visible mounts on intersection");
const idl = new El({ "data-brick-type": "Lazy", "data-brick-mount": "idle" });
append(doc.documentElement, idl);
assert.equal(lazy, 1, "idle waits");
idle.shift()();
assert.equal(lazy, 2, "idle mounts when idle");

// Nested elements mount with their container.
const wrap = new El();
const inner = new El({ "data-brick-type": "Acc" });
wrap.children.push(inner); inner.parent = wrap;
append(doc.documentElement, wrap);
assert.equal(calls.length, 4, "nested element mounted");

// Events.
const got = [];
const off = brick.on("open-modal", (d) => got.push(d));
brick.emit("open-modal", { id: 1 });
off();
brick.emit("open-modal", { id: 2 });
assert.equal(JSON.stringify(got), '[{"id":1}]', "emit/on and unsubscribe");

// Loading twice keeps the first runtime.
const first = win.brick;
vm.runInContext(readFileSync(new URL("./runtime.js", import.meta.url), "utf8"), win);
assert.equal(win.brick, first, "idempotent");

console.log("runtime ok");
