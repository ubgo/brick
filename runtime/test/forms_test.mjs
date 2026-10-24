// Tests runtime.js's v7 batch A behaviour in
// jsdom: deferred bindings and "sets", list and nested bindings, the
// modifiers, targeted loading, brick:disable-with, brick:dirty, reset
// buttons, brick:confirm and polls with an action. Each case is its own
// named test, so a failure names the behaviour that broke.
import { readFileSync } from "node:fs";
import assert from "node:assert/strict";
import { JSDOM, VirtualConsole } from "jsdom";
import { tick, click, key, fill } from "./dom.mjs";

const runtimeJS = readFileSync(new URL("../runtime.js", import.meta.url), "utf8");

const windows = [];

// setup loads page with the runtime. respond(body) answers each action
// POST: it returns {status, body, hold}; a held answer waits until
// release() is called (oldest first). opts.before(w) runs before the
// runtime is evaluated (stubs for confirm or IntersectionObserver).
function setup(page, respond = () => ({}), opts = {}) {
  const dom = new JSDOM(`<!doctype html><body>${page}</body>`, { url: "https://site.test/f/", runScripts: "outside-only", virtualConsole: new VirtualConsole(), pretendToBeVisual: Boolean(opts.visual) });
  const w = dom.window;
  windows.push(w);
  const sent = [];
  const held = [];
  w.fetch = (url, init = {}) => {
    const body = init.body ? JSON.parse(init.body) : null;
    sent.push(body);
    const r = respond(body) || {};
    const answer = { status: r.status || 200, ok: !r.status || r.status < 400, statusText: "x", text: async () => r.body || "" };
    if (r.hold) return new Promise((res) => held.push(() => res(answer)));
    return Promise.resolve(answer);
  };
  if (opts.before) opts.before(w);
  w.eval(runtimeJS);
  const events = [];
  for (const n of ["dirty", "clean", "action-error"]) w.brick.on(n, (d) => events.push([n, d]));
  return { w, doc: w.document, $: (id) => w.document.getElementById(id), sent, events, release: () => held.shift()() };
}
const submit = (w, form) => form.dispatchEvent(new w.SubmitEvent("submit", { bubbles: true, cancelable: true }));
const calls = (sent) => sent.map((b) => [b.action, b.args, b.sets]);

// signup is a deferred form: three deferred fields, a live one, a local
// echo and a submit.
const signup = (h = "H", name = "", extra = "") => `<form id="f" data-brick="s1" data-brick-state="${h}" data-brick-token="T" brick:submit="signup"${extra}>
  <input id="name" brick:model.defer="name" value="${name}">
  <input id="email" brick:model.defer="email" value="">
  <select id="plan" brick:model.defer="plan"><option value="free" selected>free</option><option value="pro">pro</option></select>
  <input id="live" brick:model.debounce.30ms="live" value="">
  <p>Hi <span id="hi" brick:text="name">${name}</span></p>
  <p id="unsaved" brick:dirty hidden>unsaved</p>
  <p id="email-dirty" brick:dirty="email" hidden>email changed</p>
  <span id="saved" brick:dirty.remove>saved</span>
  <button id="go">Sign up</button>
  <button id="reset" type="reset">Reset</button>
</form>`;

const tests = [];
const test = (name, fn) => tests.push([name, fn]);

test("defer: typing sends nothing; the submit carries every value as sets in one request", async () => {
  const { w, $, sent } = setup(signup());
  fill(w, $("name"), "Ada");
  fill(w, $("email"), "ada@x.io");
  $("plan").value = "pro";
  $("plan").dispatchEvent(new w.Event("change", { bubbles: true }));
  assert.equal($("hi").textContent, "Ada", "brick:text still reacts locally");
  await tick(300);
  assert.equal(sent.length, 0, "no request while typing");
  click(w, $("go"));
  await tick();
  assert.deepEqual(calls(sent), [["signup", [], [["name", "Ada"], ["email", "ada@x.io"], ["plan", "pro"]]]]);
});

test("defer: a failed request keeps the values for the next one; a 2xx clears them", async () => {
  let n = 0;
  const { w, $, sent } = setup(signup(), () => (++n === 1 ? { status: 422, body: "bad" } : {}));
  fill(w, $("name"), "Ada");
  click(w, $("go"));
  await tick();
  click(w, $("go"));
  await tick();
  click(w, $("go"));
  await tick();
  assert.deepEqual(sent.map((b) => b.sets), [[["name", "Ada"]], [["name", "Ada"]], undefined]);
});

test("defer: a value typed while a request is in flight goes out with the next one and survives the morph", async () => {
  const { w, $, sent, release } = setup(signup("H0"), () => ({ hold: true, body: signup("H1") }));
  fill(w, $("name"), "Ada");
  click(w, $("go"));
  await tick();
  fill(w, $("email"), "a@x.io");
  $("email").blur();
  release();
  await tick();
  assert.equal($("email").value, "a@x.io", "the unsent deferred field keeps its value through the morph");
  click(w, $("go"));
  await tick();
  release();
  await tick();
  assert.deepEqual(sent.map((b) => [b.handle, b.sets]), [["H0", [["name", "Ada"]]], ["H1", [["email", "a@x.io"]]]]);
});

test("defer: a live $set of the field evicts its deferred value", async () => {
  const { w, $, sent } = setup(signup());
  fill(w, $("name"), "Ada");
  await w.brick.action($("go"), "$set", ["name", "Bob"]);
  await tick(300);
  click(w, $("go"));
  await tick();
  assert.deepEqual(calls(sent), [["$set", ["name", "Bob"], undefined], ["signup", [], undefined]], "the evicted value is not sent again");
});

const groups = `<div id="r" data-brick="g1" data-brick-state="H" data-brick-token="T" data-brick-arrays="tags solo">
  <input id="go" type="checkbox" brick:model="tags" value="go" checked>
  <input id="js" type="checkbox" brick:model="tags" value="js">
  <input id="one" type="checkbox" brick:model="solo" value="only">
  <input id="flag" type="checkbox" brick:model="flag">
  <select id="multi" multiple brick:model="langs"><option value="a" selected>a</option><option value="b">b</option><option value="c">c</option></select>
  <input id="city" brick:model.debounce.10ms="address.city" value="">
  <span id="city-echo" brick:text="address.city"></span>
</div>`;

test("arrays: a checkbox group bound to a list sends the checked values, a one-box group too, a plain checkbox a boolean", async () => {
  const { w, $, sent } = setup(groups);
  const toggle = (id) => { $(id).checked = !$(id).checked; $(id).dispatchEvent(new w.Event("change", { bubbles: true })); };
  toggle("js");
  await tick();
  toggle("go");
  await tick();
  toggle("one");
  await tick();
  toggle("flag");
  await tick();
  assert.deepEqual(sent.map((b) => b.args), [["tags", ["go", "js"]], ["tags", ["js"]], ["solo", ["only"]], ["flag", true]]);
});

test("arrays: a multi-select sends its selected values; a nested path drives brick:text and $set", async () => {
  const { w, $, sent } = setup(groups);
  $("multi").options[2].selected = true;
  $("multi").dispatchEvent(new w.Event("change", { bubbles: true }));
  await tick();
  fill(w, $("city"), "Paris");
  assert.equal($("city-echo").textContent, "Paris");
  await tick(60);
  assert.deepEqual(sent.map((b) => b.args), [["langs", ["a", "c"]], ["address.city", "Paris"]]);
});

const mods = `<div data-brick="m1" data-brick-state="H" data-brick-token="T">
  <input id="q" brick:model.throttle.100ms="q" value="">
  <button id="once" brick:click.once="buy">buy</button>
  <button id="deb" brick:click.debounce.60ms="search">s</button>
  <button id="thr" brick:click.throttle.200ms="ping">p</button>
  <form id="sf"><input id="term" brick:model.defer="term" brick:keydown.enter="find" value=""></form>
  <span brick:keydown.escape.window="close"></span>
</div>`;

test("model .throttle: the first value at once, then at most one per window, with a trailing last value", async () => {
  const { w, $, sent } = setup(mods);
  fill(w, $("q"), "a");
  fill(w, $("q"), "ab");
  fill(w, $("q"), "abc");
  await tick(10);
  assert.deepEqual(sent.map((b) => b.args), [["q", "a"]], "leading send only");
  await tick(200);
  assert.deepEqual(sent.map((b) => b.args), [["q", "a"], ["q", "abc"]], "trailing send with the last value");
});

test("event .once, .debounce and .throttle", async () => {
  const { w, $, sent } = setup(mods);
  click(w, $("once"));
  click(w, $("once"));
  click(w, $("deb"));
  click(w, $("deb"));
  click(w, $("deb"));
  click(w, $("thr"));
  click(w, $("thr"));
  await tick(150);
  assert.deepEqual(sent.map((b) => b.action).sort(), ["buy", "ping", "search"]);
});

test("keydown.enter on a deferred input sends one request with sets and prevents the form's submit; other keys do nothing", async () => {
  const { w, $, sent } = setup(mods);
  fill(w, $("term"), "brick");
  key(w, $("term"), "a");
  await tick();
  assert.equal(sent.length, 0);
  const ev = new w.KeyboardEvent("keydown", { key: "Enter", bubbles: true, cancelable: true });
  $("term").dispatchEvent(ev);
  await tick();
  assert.equal(ev.defaultPrevented, true, "Enter's default (an implicit submit) is prevented");
  assert.deepEqual(calls(sent), [["find", [], [["term", "brick"]]]]);
});

test("keydown.escape.window runs for Escape pressed anywhere", async () => {
  const { w, doc, sent } = setup(mods);
  await tick(); // the root is bound on DOMContentLoaded
  key(w, doc.body, "Escape");
  key(w, doc.body, "x");
  await tick();
  assert.deepEqual(sent.map((b) => b.action), ["close"]);
});

const loading = `<div id="root" data-brick="l1" data-brick-state="H" data-brick-token="T">
  <form id="lf" brick:submit="save"><button id="save" brick:disable-with="Saving…">Save</button></form>
  <button id="del" brick:click="remove">Delete</button>
  <input id="n" brick:model.debounce.10ms="n" value="">
  <span id="any" brick:loading hidden>busy</span>
  <span id="only-save" brick:loading="save" hidden>saving</span>
  <span id="slow" brick:loading.delay.100ms="remove" hidden>still working</span>
  <span id="label" brick:loading.remove="save">Save</span>
  <span id="poll-ind" brick:loading="$refresh" hidden>refreshing</span>
  <div brick:poll="1s"></div>
</div>`;

test("loading: brick:loading=<action> shows for that action only; .remove hides; data-loading on trigger, form and root", async () => {
  const { w, $, release } = setup(loading, () => ({ hold: true }));
  fill(w, $("n"), "x");
  await tick(40);
  assert.equal($("any").hidden, false, "unvalued shows for any action");
  assert.equal($("only-save").hidden, true, "not for a $set");
  release();
  await tick();
  submit(w, $("lf"));
  await tick();
  assert.equal($("only-save").hidden, false, "shown for save");
  assert.equal($("label").hidden, true, ".remove hides during save");
  assert.equal($("lf").hasAttribute("data-loading"), true, "the submitted form carries data-loading");
  assert.equal($("root").getAttribute("data-loading"), "save", "root lists in-flight actions");
  assert.equal($("save").textContent, "Saving…", "disable-with label");
  assert.equal($("save").disabled, true);
  release();
  await tick();
  assert.equal($("only-save").hidden, true);
  assert.equal($("label").hidden, false);
  assert.equal($("lf").hasAttribute("data-loading"), false);
  assert.equal($("root").hasAttribute("data-loading"), false);
  assert.equal($("save").textContent, "Save", "label restored");
  assert.equal($("save").disabled, false);
});

test("loading: a clicked trigger carries data-loading; .delay does not flash for a fast action and shows for a slow one", async () => {
  const { w, $, release } = setup(loading, () => ({ hold: true }));
  click(w, $("del"));
  assert.equal($("del").hasAttribute("data-loading"), true);
  await tick(30);
  release();
  await tick();
  assert.equal($("slow").hidden, true, "a fast action never showed the delayed indicator");
  assert.equal($("del").hasAttribute("data-loading"), false);
  click(w, $("del"));
  await tick(180);
  assert.equal($("slow").hidden, false, "shown once the action ran past the delay");
  release();
  await tick();
  assert.equal($("slow").hidden, true);
});

test("loading: a poll is quiet, except for a brick:loading that names $refresh", async () => {
  const { $ } = setup(loading, () => ({ hold: true }), { visual: true });
  await tick(1100);
  assert.equal($("poll-ind").hidden, false, "brick:loading=\"$refresh\" shows");
  assert.equal($("any").hidden, true, "an unvalued indicator stays hidden");
  assert.equal($("root").hasAttribute("aria-busy"), false, "no aria-busy");
  assert.equal($("root").hasAttribute("data-loading"), false, "no action list");
});

test("dirty: shows after a deferred edit, per field and inverted, with data-dirty and events; clean after the server renders it", async () => {
  const { w, $, events } = setup(signup("H0"), () => ({ body: signup("H1", "Ada") }));
  assert.equal($("unsaved").hidden, true);
  fill(w, $("name"), "Ada");
  assert.equal($("unsaved").hidden, false, "any field dirty");
  assert.equal($("email-dirty").hidden, true, "email is not dirty");
  assert.equal($("saved").hidden, true, ".remove hides while dirty");
  assert.equal($("f").hasAttribute("data-dirty"), true);
  click(w, $("go"));
  await tick();
  assert.equal($("unsaved").hidden, true, "clean once the server has the value");
  assert.equal($("saved").hidden, false);
  assert.equal($("f").hasAttribute("data-dirty"), false);
  assert.deepEqual(events.map((e) => e[0]), ["dirty", "clean"]);
});

test("reset: drops deferred values and re-reads the signals with no request", async () => {
  const { w, $, sent } = setup(signup("H", "Al"));
  fill(w, $("name"), "Ada");
  $("f").reset();
  await tick();
  assert.equal($("name").value, "Al");
  assert.equal($("hi").textContent, "Al", "signal re-read");
  assert.equal($("unsaved").hidden, true, "clean again");
  click(w, $("go"));
  await tick();
  assert.deepEqual(calls(sent), [["signup", [], undefined]], "the dropped value is not sent");
});

test("reset: a field already sent live goes back as one $refresh carrying sets", async () => {
  const { w, $, sent, release } = setup(signup("H", "Al"), () => ({ hold: true }));
  fill(w, $("live"), "typed");
  await tick(60);
  assert.deepEqual(calls(sent), [["$set", ["live", "typed"], undefined]]);
  $("f").reset();
  await tick();
  release();
  await tick();
  assert.deepEqual(calls(sent)[1], ["$refresh", [], [["live", ""]]]);
});

const confirmPage = `<div id="root" data-brick="c1" data-brick-state="H" data-brick-token="T">
  <button id="del" brick:click="remove(7)" brick:confirm="Delete this contact?">Delete</button>
  <span id="spin" brick:loading hidden>…</span>
</div>`;

test("confirm: window.confirm by default; no sends and no loading on cancel", async () => {
  const asked = [];
  let answer = false;
  const { w, $, sent } = setup(confirmPage, () => ({}), { before: (w) => { w.confirm = (m) => { asked.push(m); return answer; }; } });
  click(w, $("del"));
  await tick();
  assert.equal($("spin").hidden, true, "no loading state");
  assert.equal(sent.length, 0);
  answer = true;
  click(w, $("del"));
  await tick();
  assert.deepEqual(asked, ["Delete this contact?", "Delete this contact?"]);
  assert.deepEqual(calls(sent), [["remove", [7], undefined]]);
});

test("confirm: brick.confirmWith replaces the confirmer (async) and its undo restores the default", async () => {
  let natives = 0;
  const { w, $, sent } = setup(confirmPage, () => ({}), { before: (w) => { w.confirm = () => { natives++; return false; }; } });
  const seen = [];
  const undo = w.brick.confirmWith(async (message, el) => { seen.push([message, el.id]); return true; });
  click(w, $("del"));
  await tick();
  assert.deepEqual(seen, [["Delete this contact?", "del"]]);
  assert.equal(sent.length, 1);
  undo();
  click(w, $("del"));
  await tick();
  assert.equal(natives, 1, "back to window.confirm");
  assert.equal(sent.length, 1);
});

const pollPage = `<div id="root" data-brick="p1" data-brick-state="H" data-brick-token="T">
  <section id="stats" brick:poll.1s="tick(1)"></section>
  <section id="seen" brick:poll.1s.visible="seen"></section>
</div>`;

test("poll: brick:poll.<interval>=\"call\" on a non-root element runs the call; .visible waits for the viewport", async () => {
  const observers = [];
  const { sent } = setup(pollPage, () => ({}), {
    visual: true,
    before: (w) => {
      w.IntersectionObserver = class { constructor(fn) { this.fn = fn; observers.push(this); } observe(el) { this.el = el; } disconnect() {} };
    },
  });
  await tick(1100);
  assert.deepEqual(sent.map((b) => [b.action, b.args]), [["tick", [1]]], "the off-screen poll waits");
  observers[0].fn([{ isIntersecting: true }]);
  await tick(1000);
  assert.deepEqual(sent.map((b) => b.action).sort(), ["seen", "tick", "tick"]);
});

let failed = 0;
for (const [name, fn] of tests) {
  try {
    await fn();
  } catch (err) {
    failed++;
    console.error("FAIL " + name + "\n", err);
  }
}
windows.forEach((w) => w.close());
if (failed) {
  process.exitCode = 1;
} else {
  console.log("forms ok");
}
