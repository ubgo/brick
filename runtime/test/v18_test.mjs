// Tests runtime.js's v18 client features in jsdom: brick:js commands
//, brick:leave exit transitions
// (B: a morph removal, a second morph while one plays, reduced motion,
// rows delete and limit trim, a hide and a show that cancels it) and
// brick:confirm.prompt (C: the answer is the last argument, a cancel sends
// nothing, brick.promptWith restores).
import { readFileSync } from "node:fs";
import assert from "node:assert/strict";
import { JSDOM, VirtualConsole } from "jsdom";
import { tick, click, key } from "./dom.mjs";

const runtimeJS = readFileSync(new URL("../runtime.js", import.meta.url), "utf8");
const ENVELOPE = "application/vnd.brick.action+json";
// MOTION_MS is the fake duration a "fade" class gives an element: short,
// so a test waits for a real timer instead of faking the clock.
const MOTION_MS = 40;

// setup loads html with the runtime. answer(body) is the server: it
// returns {html, effects} for each action request, which sent records as
// {action, args}. getComputedStyle reports MOTION_MS of transition for an
// element with the class "fade", and nothing for any other.
async function setup(html, { answer = () => ({ html: "", effects: [] }), reduced = false } = {}) {
  const dom = new JSDOM(`<!doctype html><body>${html}</body>`, { url: "https://site.test/", runScripts: "outside-only", pretendToBeVisual: true, virtualConsole: new VirtualConsole() });
  const w = dom.window;
  const sent = [];
  const errors = [];
  w.fetch = (url, init = {}) => {
    const body = JSON.parse(init.body);
    sent.push({ action: body.action, args: body.args });
    const a = answer(body);
    return Promise.resolve({ status: 200, ok: true, statusText: "OK", headers: { get: () => ENVELOPE }, text: async () => JSON.stringify({ v: 1, html: a.html, effects: a.effects || [] }) });
  };
  const real = w.getComputedStyle.bind(w);
  w.getComputedStyle = (el) => {
    const cs = real(el);
    const fade = el.classList && el.classList.contains("fade");
    return { display: cs.display, transitionDuration: fade ? MOTION_MS + "ms" : "0s", transitionDelay: "0s", animationDuration: "0s", animationDelay: "0s" };
  };
  w.matchMedia = (q) => ({ matches: reduced && q.includes("reduce") });
  w.console.error = (e) => errors.push(String(e && e.message || e));
  w.eval(runtimeJS);
  w.brick.on("action-error", (d) => errors.push(d.message));
  await tick();
  return { w, d: w.document, sent, errors, $: (s) => w.document.querySelector(s) };
}

const root = (inner) => `<div data-brick="c1" data-brick-state="H" data-brick-token="T">${inner}</div>`;

async function main() {
// A1. show, hide and toggle use the hidden attribute; the target is a
// selector (every match), "this", "@ref" or "closest <selector>".
{
  const { d, $ } = await setup(`
    <button id="t" brick:js.click='toggle(".m")'>t</button>
    <p class="m">1</p><p class="m" hidden>2</p>
    <div class="card"><button id="x" brick:js.click='hide("closest .card")'>x</button></div>
    <button id="self" brick:js.click='hide("this")'>s</button>
    <section brick:ref="panel" hidden></section><button id="r" brick:js.click='show("@panel")'>r</button>`);
  click(d.defaultView, $("#t"));
  const ms = [...d.querySelectorAll(".m")];
  assert.deepEqual(ms.map((m) => m.hasAttribute("hidden")), [true, false], "toggle flips each match");
  click(d.defaultView, $("#x"));
  assert.ok($(".card").hasAttribute("hidden"), "closest");
  click(d.defaultView, $("#self"));
  assert.ok($("#self").hasAttribute("hidden"), "this");
  click(d.defaultView, $("#r"));
  assert.ok(!$("[brick\\:ref=panel]").hasAttribute("hidden"), "@ref");
}

// A2. Class and attribute commands, focus and dispatch, in one list run
// in order; a semicolon inside a JSON string is not a separator.
{
  const { w, d, $ } = await setup(`
    <button id="b" aria-expanded="false" brick:js.click='addClass("#p", "open big"); removeClass("#p", "dim"); toggleClass("#p", "on"); setAttr("#p", "title", "a; b"); removeAttr("#p", "data-x"); toggleAttr("this", "aria-expanded", "true", "false"); toggleAttr("#p", "inert"); focus("#q"); dispatch("#p", "menu:open", {"id": 7})'>b</button>
    <div id="p" class="dim" data-x="1"></div><input id="q">`);
  let got = null;
  d.addEventListener("menu:open", (e) => { got = e.detail; });
  click(w, $("#b"));
  const p = $("#p");
  assert.deepEqual([...p.classList].sort(), ["big", "on", "open"]);
  assert.equal(p.getAttribute("title"), "a; b");
  assert.ok(!p.hasAttribute("data-x"));
  assert.ok(p.hasAttribute("inert"), "toggleAttr with a name only toggles presence");
  assert.equal($("#b").getAttribute("aria-expanded"), "true");
  assert.equal(d.activeElement, $("#q"));
  assert.deepEqual(JSON.parse(JSON.stringify(got)), { id: 7 }, "dispatch bubbles a CustomEvent with the detail");
  click(w, $("#b"));
  assert.equal($("#b").getAttribute("aria-expanded"), "false", "on ⇄ off");
  assert.ok(!p.classList.contains("on") && !p.hasAttribute("inert"));
}

// A3. .outside on a wrapper closes the menu on a click elsewhere, not on
// a click inside; .window hears a key anywhere; .once runs once; a key
// combo needs exactly its modifiers.
{
  const { w, d, $ } = await setup(`
    <div id="wrap" brick:js.click.outside='hide("#menu")'><button id="open" brick:js.click='show("#menu")'>o</button><ul id="menu" hidden><li id="li">a</li></ul></div>
    <p id="else">x</p>
    <div brick:js.keydown.escape.window='hide("#menu")'></div>
    <button id="once" brick:js.click.once='toggleClass("this", "n")'>1</button>
    <input id="k" brick:js.keydown.ctrl.k='addClass("this", "hit")'>`);
  click(w, $("#open"));
  assert.ok(!$("#menu").hasAttribute("hidden"), "a click inside does not close it");
  click(w, $("#li"));
  assert.ok(!$("#menu").hasAttribute("hidden"));
  click(w, $("#else"));
  assert.ok($("#menu").hasAttribute("hidden"), ".outside");
  click(w, $("#open"));
  key(w, $("#else"), "Escape");
  assert.ok($("#menu").hasAttribute("hidden"), ".window");
  click(w, $("#once"));
  click(w, $("#once"));
  assert.ok($("#once").classList.contains("n"), ".once");
  key(w, $("#k"), "k", { ctrlKey: true, shiftKey: true });
  assert.ok(!$("#k").classList.contains("hit"), "an extra modifier does not match");
  key(w, $("#k"), "k", { ctrlKey: true });
  assert.ok($("#k").classList.contains("hit"));
  // Elements added later are registered too.
  const late = d.createElement("div");
  late.setAttribute("brick:js.keydown.x.window", 'addClass("this", "late")');
  d.body.appendChild(late);
  await tick();
  key(w, $("#else"), "x");
  assert.ok(late.classList.contains("late"), "a late .window element");
}

// A4. A bad list runs nothing: an unknown command, a refused attribute
// (the runtime checks again, as {{ }} fills values at render time), a
// script URL, a dispatch of brick's own event. Each is reported.
{
  const { w, $, errors } = await setup(`
    <button id="a" brick:js.click='addClass("#p", "x"); explode("#p")'>a</button>
    <button id="b" brick:js.click='setAttr("#p", "onclick", "alert(1)")'>b</button>
    <button id="c" brick:js.click='setAttr("#l", "href", "javascript:alert(1)")'>c</button>
    <button id="e" brick:js.click='dispatch("#p", "click")'>e</button>
    <p id="p"></p><a id="l" href="/ok">l</a>`);
  for (const id of ["#a", "#b", "#c", "#e"]) click(w, $(id));
  assert.ok(!$("#p").classList.contains("x"), "the list runs whole or not at all");
  assert.ok(!$("#p").hasAttribute("onclick"));
  assert.equal($("#l").getAttribute("href"), "/ok");
  assert.equal(errors.length, 8, "each refusal is logged and emitted: " + errors.join(" | "));
  assert.ok(errors.some((e) => e.includes("unknown command")) && errors.some((e) => e.includes("event handler")) && errors.some((e) => e.includes("script URL")) && errors.some((e) => e.includes("re-trigger")));
}

// A5. Sticky: what a command changed survives an action's morph, which
// would otherwise reset the attributes to the server's markup; the action
// on the same element still runs.
{
  const markup = (n) => root(`<button id="go" brick:click="inc" brick:js.click='toggleClass("#panel", "open"); show("#panel")'>go</button><div id="panel" class="box" hidden>${n}</div>`);
  const { w, $, sent } = await setup(markup(0), { answer: () => ({ html: markup(1) }) });
  click(w, $("#go"));
  await tick();
  assert.deepEqual(sent.map((s) => s.action), ["inc"]);
  assert.equal($("#panel").textContent, "1", "the morph ran");
  assert.ok($("#panel").classList.contains("open") && $("#panel").classList.contains("box"), "class replayed, server class kept");
  assert.ok(!$("#panel").hasAttribute("hidden"), "show replayed");
}

// B1. A morph that drops an element with brick:leave plays its classes
// and removes it once the transition ends; while it plays it is inert,
// marked, never matched by a second morph, and the rest is in order.
{
  const items = (ks) => root(`<button id="go" brick:click="del">go</button><ul>${ks.map((k) => `<li brick:key="${k}" brick:leave="fade gone">${k}</li>`).join("")}</ul>`);
  let next = ["a", "c"];
  const { w, d, $ } = await setup(items(["a", "b", "c"]), { answer: () => ({ html: items(next) }) });
  const b = $("li[brick\\:key=b]");
  click(w, $("#go"));
  await tick(5);
  assert.ok(b.isConnected, "still in the page while it plays");
  assert.ok(b.classList.contains("gone") && b.hasAttribute("data-brick-leaving") && b.inert);
  assert.deepEqual([...d.querySelectorAll("li")].map((l) => l.textContent), ["a", "b", "c"], "it stays where it was");
  next = ["a", "b", "c", "d"]; // b comes back while the old b plays
  click(w, $("#go"));
  await tick(5);
  const bs = [...d.querySelectorAll("li[brick\\:key=b]")];
  assert.equal(bs.length, 2, "the leaving b is not reused");
  assert.ok(bs.some((x) => x !== b && !x.hasAttribute("data-brick-leaving")));
  await tick(MOTION_MS * 3);
  assert.ok(!b.isConnected, "removed after the transition");
  assert.deepEqual([...d.querySelectorAll("li")].map((l) => l.textContent), ["a", "b", "c", "d"]);
}

// B2. No brick:leave, or reduced motion: removed at once.
for (const [name, attr, reduced] of [["no attribute", "", false], ["reduced motion", ` brick:leave="fade"`, true]]) {
  const m = (show) => root(`<button id="go" brick:click="x">go</button>${show ? `<p id="p"${attr}>p</p>` : ""}`);
  const { w, $ } = await setup(m(true), { answer: () => ({ html: m(false) }), reduced });
  const p = $("#p");
  click(w, $("#go"));
  await tick();
  assert.ok(!p.isConnected, name);
}

// B3. Rows: a delete plays the leave; a limit trim does not count rows
// still leaving (it would otherwise trim them forever) and leaves exactly
// limit rows.
{
  const row = (k) => ({ key: k, html: `<li brick:leave="fade">${k}</li>` });
  let effects = [];
  const { w, d, $ } = await setup(root(`<button id="go" brick:click="x">go</button><ul brick:ref="list" brick:ignore>${["1", "2", "3"].map((k) => `<li brick:key="${k}" brick:leave="fade">${k}</li>`).join("")}</ul>`), { answer: () => ({ html: d.querySelector("[data-brick]").outerHTML, effects }) });
  effects = [{ kind: "rows", ref: "list", op: "delete", keys: ["2"] }];
  const two = $("li[brick\\:key='2']");
  click(w, $("#go"));
  await tick(5);
  assert.ok(two.isConnected && two.hasAttribute("data-brick-leaving"), "delete plays the leave");
  effects = [{ kind: "rows", ref: "list", op: "insert", at: "top", limit: 2, rows: [row("4"), row("5")] }];
  click(w, $("#go"));
  await tick(5);
  const staying = [...d.querySelectorAll("ul li")].filter((l) => !l.hasAttribute("data-brick-leaving")).map((l) => l.textContent);
  assert.deepEqual(staying, ["4", "5"], "limit counts staying rows only");
  await tick(MOTION_MS * 3);
  assert.deepEqual([...d.querySelectorAll("ul li")].map((l) => l.textContent), ["4", "5"]);
}

// B4. hide plays the leave, then sets hidden and takes the classes off; a
// show while it plays cancels it.
{
  const { w, $ } = await setup(`<button id="h" brick:js.click='hide("#p")'>h</button><button id="s" brick:js.click='show("#p")'>s</button><p id="p" brick:leave="fade out">p</p>`);
  click(w, $("#h"));
  assert.ok(!$("#p").hasAttribute("hidden") && $("#p").classList.contains("out"), "playing");
  await tick(MOTION_MS * 3);
  assert.ok($("#p").hasAttribute("hidden") && !$("#p").classList.contains("out") && !$("#p").inert, "hidden, classes off");
  click(w, $("#s"));
  click(w, $("#h"));
  await tick(5);
  click(w, $("#s"));
  await tick(MOTION_MS * 3);
  assert.ok(!$("#p").hasAttribute("hidden") && !$("#p").classList.contains("out"), "the show cancelled the hide");
}

// C1. brick:confirm.prompt: the answer is the call's last argument (after
// the call's own); "" is an answer; a cancel (null) or a failed prompter
// sends nothing; promptWith's restore puts the previous prompter back.
{
  const { w, $, sent } = await setup(root(`<button id="r" brick:click='rename(7)' brick:confirm.prompt="New name?">r</button>`), { answer: () => ({ html: root(`<button id="r" brick:click='rename(7)' brick:confirm.prompt="New name?">r</button>`) }) });
  const asked = [];
  let reply = "Bob";
  const restore = w.brick.promptWith((m, el) => { asked.push([m, el.id]); return reply; });
  click(w, $("#r"));
  await tick();
  assert.deepEqual(asked, [["New name?", "r"]]);
  assert.deepEqual(sent, [{ action: "rename", args: [7, "Bob"] }]);
  reply = null;
  click(w, $("#r"));
  await tick();
  assert.equal(sent.length, 1, "a cancel sends nothing");
  reply = "";
  click(w, $("#r"));
  await tick();
  assert.deepEqual(sent[1].args, [7, ""], "an empty answer is still an answer");
  w.brick.promptWith(() => Promise.reject(new Error("closed")));
  click(w, $("#r"));
  await tick();
  assert.equal(sent.length, 2, "a failed prompter sends nothing");
  w.brick.promptWith(() => Promise.resolve("Ann"));
  click(w, $("#r"));
  await tick();
  assert.deepEqual(sent[2].args, [7, "Ann"], "a promise of an answer");
  restore(); // not the current prompter: no effect
  let opened = 0;
  w.prompt = () => { opened++; return null; };
  const back = w.brick.promptWith(() => "x");
  back();
  click(w, $("#r"));
  await tick();
  assert.equal(opened, 0, "restore only undoes its own prompter");
  assert.throws(() => w.brick.promptWith("nope"));
}

// C2. A plain brick:confirm still asks the confirmer, not the prompter.
{
  const { w, $, sent } = await setup(root(`<button id="d" brick:click="wipe" brick:confirm="Sure?">d</button>`), { answer: () => ({ html: root(`<button id="d" brick:click="wipe" brick:confirm="Sure?">d</button>`) }) });
  let prompted = 0;
  w.brick.promptWith(() => { prompted++; return "x"; });
  w.brick.confirmWith(() => true);
  click(w, $("#d"));
  await tick();
  assert.equal(prompted, 0);
  assert.deepEqual(sent, [{ action: "wipe", args: [] }]);
}

console.log("v18 ok");
}

main().catch((err) => { console.error(err); process.exit(1); });
