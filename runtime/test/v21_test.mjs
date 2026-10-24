// Tests runtime.js's v21 client features in jsdom:
// brick:focus/blur.window and brick:js focus/blur.window (R1), brick:loading
// call, $set and .except targets (R3), brick:show playing brick:leave and
// brick:enter, held through a morph (R4), brick.js (R5), brick.find /
// brick.all (R6), valueless brick:disable-with (M6) and the flash-clear
// effect. headless Chrome covers the same in v21_chrome.mjs.
import { readFileSync } from "node:fs";
import assert from "node:assert/strict";
import { JSDOM, VirtualConsole } from "jsdom";
import { tick, click, fill } from "./dom.mjs";

const runtimeJS = readFileSync(new URL("../runtime.js", import.meta.url), "utf8");
const ENVELOPE = "application/vnd.brick.action+json";
// MOTION_MS is the fake duration a "fade" class (or, for brick:enter, the
// element's own "slide" class) gives an element: short, so a test waits
// for a real timer instead of faking the clock.
const MOTION_MS = 40;

// setup loads html with the runtime. Each action request waits until the
// test calls release() (hold: true) or answers at once; answer(body)
// returns {html, effects}. sent records {action, args}.
async function setup(html, { answer = () => ({ html: "", effects: [] }), hold = false } = {}) {
  const dom = new JSDOM(`<!doctype html><body>${html}</body>`, { url: "https://site.test/", runScripts: "outside-only", pretendToBeVisual: true, virtualConsole: new VirtualConsole() });
  const w = dom.window;
  const sent = [];
  const errors = [];
  const pending = [];
  w.fetch = (url, init = {}) => {
    const body = JSON.parse(init.body);
    sent.push({ action: body.action, args: body.args });
    const respond = () => {
      const a = answer(body);
      return { status: 200, ok: true, statusText: "OK", headers: { get: () => ENVELOPE }, text: async () => JSON.stringify({ v: 1, html: a.html, effects: a.effects || [] }) };
    };
    if (!hold) return Promise.resolve(respond());
    return new Promise((r) => pending.push(() => r(respond())));
  };
  const real = w.getComputedStyle.bind(w);
  w.getComputedStyle = (el) => {
    const cs = real(el);
    const moving = el.classList && (el.classList.contains("fade") || el.classList.contains("slide"));
    return { display: cs.display, transitionDuration: moving ? MOTION_MS + "ms" : "0s", transitionDelay: "0s", animationDuration: "0s", animationDelay: "0s" };
  };
  w.matchMedia = () => ({ matches: false });
  w.console.error = (e) => errors.push(String(e && e.message || e));
  w.console.warn = () => {};
  w.eval(runtimeJS);
  w.brick.on("action-error", (d) => errors.push(d.message));
  await tick();
  const release = async () => { while (pending.length) pending.shift()(); await tick(); };
  return { w, d: w.document, sent, errors, release, $: (s) => w.document.querySelector(s) };
}

const root = (inner, name = "Box", id = "c1") => `<div data-brick="${id}" data-brick-name="${name}" data-brick-state="H" data-brick-token="T">${inner}</div>`;

async function main() {
// R1. brick:focus.window / brick:blur.window run on the window's focus
// and blur, with no value; an element's own focus does not run them, and
// brick:focus without .window still runs for the element.
{
  const { w, $, sent } = await setup(root(`<input id="i" value="v" brick:focus.window="back" brick:blur.window="away" brick:focus="own">`));
  w.dispatchEvent(new w.FocusEvent("blur"));
  await tick();
  w.dispatchEvent(new w.FocusEvent("focus"));
  await tick();
  assert.deepEqual(sent.map((s) => [s.action, s.args]), [["away", []], ["back", []]]);
  $("#i").focus();
  await tick();
  $("#i").blur();
  await tick();
  assert.deepEqual(sent.map((s) => s.action), ["away", "back", "own"], "the element's own focus runs only brick:focus");
}

// R1. brick:js.focus.window / brick:js.blur.window run commands on the
// window's focus and blur; the element's own focus does not.
{
  const { w, $ } = await setup(`<input id="j" brick:js.blur.window='addClass("this", "away")' brick:js.focus.window='removeClass("this", "away")'>`);
  w.dispatchEvent(new w.FocusEvent("blur"));
  assert.ok($("#j").classList.contains("away"));
  w.dispatchEvent(new w.FocusEvent("focus"));
  assert.ok(!$("#j").classList.contains("away"));
  $("#j").focus();
  $("#j").blur();
  assert.ok(!$("#j").classList.contains("away"), "the element's blur is not the window's");
}

// R3. A call target lights only for a job whose arguments start with its
// own; a bare name for any call; .except for every job but its targets.
{
  const { w, $, release } = await setup(root(`
    <button id="r1" brick:click="remove(1)">1</button><button id="r2" brick:click='remove(2, "a b")'>2</button><button id="dl" brick:click="download">d</button>
    <i id="s1" hidden brick:loading="remove(1)"></i><i id="s2" hidden brick:loading='remove(2, "a b")'></i><i id="sany" hidden brick:loading="remove"></i>
    <i id="pre" hidden brick:loading="remove(2)"></i><i id="ex" hidden brick:loading.except="download remove(1)"></i><i id="two" hidden brick:loading="download remove(1)"></i>`), { hold: true });
  const shown = () => ["s1", "s2", "sany", "pre", "ex", "two"].filter((id) => !$("#" + id).hidden);
  click(w, $("#r2"));
  await tick();
  assert.deepEqual(shown(), ["s2", "sany", "pre", "ex"], "remove(2, \"a b\"): its own target, the bare name, a prefix, .except");
  await release();
  assert.deepEqual(shown(), []);
  click(w, $("#r1"));
  await tick();
  assert.deepEqual(shown(), ["s1", "sany", "two"], "remove(1) is excepted");
  await release();
  click(w, $("#dl"));
  await tick();
  assert.deepEqual(shown(), ["two"], "download is excepted");
  await release();
}

// R3. $set("field") lights for a live $set of that field or a path under
// it, and for a deferred value riding along another action.
{
  const { w, $, release } = await setup(root(`
    <input id="q" brick:model="query"><input id="n" brick:model="user.name"><input id="d" brick:model.defer="draft">
    <button id="go" brick:click="save">go</button>
    <i id="lq" hidden brick:loading='$set("query")'></i><i id="lu" hidden brick:loading='$set("user")'></i><i id="ld" hidden brick:loading='$set("draft")'></i>`), { hold: true });
  const shown = () => ["lq", "lu", "ld"].filter((id) => !$("#" + id).hidden);
  fill(w, $("#n"), "Ann");
  await tick(400);
  assert.deepEqual(shown(), ["lu"], "user.name is under user");
  await release();
  fill(w, $("#q"), "x");
  await tick(400);
  assert.deepEqual(shown(), ["lq"]);
  await release();
  fill(w, $("#d"), "held");
  await tick();
  assert.deepEqual(shown(), [], "a deferred value is not sent yet");
  click(w, $("#go"));
  await tick();
  assert.deepEqual(shown(), ["ld"], "the deferred draft rides along save");
  await release();
}

// M6. A valueless brick:disable-with disables and keeps the label; one
// with text swaps it and puts it back.
{
  const { w, $, release } = await setup(root(`<button id="a" brick:click="save" brick:disable-with>Save</button><button id="b" brick:click="save" brick:disable-with="Saving…">Save</button>`), { hold: true });
  click(w, $("#a"));
  click(w, $("#b"));
  await tick();
  assert.ok($("#a").disabled && $("#b").disabled);
  assert.equal($("#a").textContent, "Save", "the label stays");
  assert.equal($("#b").textContent, "Saving…");
  await release();
  await release();
  assert.ok(!$("#a").disabled && !$("#b").disabled);
  assert.equal($("#a").textContent, "Save");
  assert.equal($("#b").textContent, "Save");
}

// R4. brick:show hides through brick:leave and shows through brick:enter;
// a morph during either keeps the motion; the first render plays nothing.
{
  const panel = (open) => `<input id="o" type="checkbox" brick:model="open"${open ? " checked" : ""}><div id="p" class="slide" brick:show="open" brick:leave="fade" brick:enter="from"${open ? "" : " hidden"}>p</div>`;
  let open = true;
  const { w, $ } = await setup(root(panel(true)), { answer: () => ({ html: root(panel(open)) }) });
  assert.ok(!$("#p").hasAttribute("data-brick-entering"), "nothing plays on the first render");
  open = false;
  $("#o").checked = false;
  $("#o").dispatchEvent(new w.Event("change", { bubbles: true }));
  assert.ok($("#p").hasAttribute("data-brick-leaving") && $("#p").classList.contains("fade"), "leaving");
  assert.ok(!$("#p").hidden, "shown while it leaves");
  await tick(); // the $set's answer morphs in, rendered hidden
  assert.ok($("#p").hasAttribute("data-brick-leaving") && $("#p").classList.contains("fade") && !$("#p").hidden, "the morph does not cut the exit short");
  assert.equal(w.document.querySelectorAll("#p").length, 1, "the morph patches the hiding element, never adds a copy");
  await tick(MOTION_MS * 3);
  assert.ok($("#p").hidden && !$("#p").classList.contains("fade") && !$("#p").hasAttribute("data-brick-leaving"), "hidden after the exit");
  open = true;
  $("#o").checked = true;
  $("#o").dispatchEvent(new w.Event("change", { bubbles: true }));
  assert.ok(!$("#p").hidden, "shown at once");
  assert.ok($("#p").hasAttribute("data-brick-entering") && $("#p").classList.contains("from"), "starts from brick:enter");
  await tick(); // a frame passes and the answer morphs in
  assert.ok(!$("#p").classList.contains("from"), "the start classes come off");
  assert.ok($("#p").hasAttribute("data-brick-entering"), "the morph keeps the marker");
  await tick(MOTION_MS * 3);
  assert.ok(!$("#p").hasAttribute("data-brick-entering"), "entered");
}

// R4. An element hiding (brick:leave) that a morph then drops is removed,
// not left hidden.
{
  const { w, d, $ } = await setup(root(`<button id="h" brick:js.click='hide("#x")'>h</button><div id="x" class="slide" brick:leave="fade">x</div>`), { answer: () => ({ html: root(`<button id="h" brick:js.click='hide("#x")'>h</button>`) }) });
  click(w, $("#h"));
  assert.ok($("#x").hasAttribute("data-brick-leaving"));
  await w.brick.action($("#h"), "drop");
  await tick(MOTION_MS * 3);
  assert.equal(d.querySelector("#x"), null, "dropped after its exit");
}

// R4. The JS show command plays brick:enter too; a hide meanwhile ends it.
{
  const { w, $ } = await setup(`<button id="s" brick:js.click='show("#m")'>s</button><button id="h" brick:js.click='hide("#m")'>h</button><div id="m" class="slide" brick:enter="from" hidden>m</div>`);
  click(w, $("#s"));
  assert.ok(!$("#m").hidden && $("#m").hasAttribute("data-brick-entering"));
  click(w, $("#h"));
  assert.ok($("#m").hidden && !$("#m").hasAttribute("data-brick-entering") && !$("#m").classList.contains("from"), "a hide ends the entrance");
}

// R5. brick.js runs a command list with el as this and scoping @ref; a
// bad list throws and runs nothing.
{
  const { w, $ } = await setup(root(`<div id="a"><b brick:ref="x">x</b></div><p id="p" data-on-close='addClass("#p", "closed")'></p>`));
  w.brick.js($("#a"), 'addClass("this", "on"); setAttr("@x", "title", "t")');
  assert.ok($("#a").classList.contains("on"));
  assert.equal($("[brick\\:ref=x]").title, "t");
  w.brick.js($("#p"), $("#p").getAttribute("data-on-close"));
  assert.ok($("#p").classList.contains("closed"), "a stored attribute");
  assert.throws(() => w.brick.js($("#a"), 'addClass("this", "z"); explode("#a")'), /unknown command/);
  assert.ok(!$("#a").classList.contains("z"), "a bad list runs nothing");
  assert.throws(() => w.brick.js(null, 'show("#a")'), /not an element/);
}

// R6. brick.all / brick.find look stateful roots up by component name.
{
  const { w } = await setup(root("", "Counter", "a") + root("", "Counter", "b") + root("", "Cart", "c") + `<div data-brick-type="Counter"></div>`);
  assert.deepEqual([...w.brick.all("Counter")].map((r) => r.getAttribute("data-brick")), ["a", "b"], "a mounted, stateless element is not a component root");
  assert.equal(w.brick.all().length, 3);
  assert.equal(w.brick.find("Cart").getAttribute("data-brick"), "c");
  assert.equal(w.brick.find("Nope"), null);
  assert.equal(w.brick.find('"] *, [x="'), null, "the type is never a selector");
}

// flash-clear: the default display drops every flash or one level; the
// bus hears {level}; a clear in a leaving answer drops carried flashes.
{
  let effects = [];
  const { w, d, $ } = await setup(root(`<button id="b" brick:click="go">b</button>`), { answer: () => ({ html: root(`<button id="b" brick:click="go">b</button>`), effects }) });
  const heard = [];
  // Listening to flash-clear only (not flash) keeps the default display.
  w.brick.on("flash-clear", (x) => heard.push(x.level));
  effects = [{ kind: "flash", message: "a", level: "info" }, { kind: "flash", message: "b", level: "warning" }];
  click(w, $("#b"));
  await tick();
  assert.equal(d.querySelectorAll("#brick-flash > div").length, 2);
  effects = [{ kind: "flash-clear", level: "warning" }];
  click(w, $("#b"));
  await tick();
  assert.deepEqual([...d.querySelectorAll("#brick-flash > div")].map((x) => x.dataset.level), ["info"]);
  effects = [{ kind: "flash-clear" }];
  click(w, $("#b"));
  await tick();
  assert.equal(d.querySelectorAll("#brick-flash > div").length, 0);
  assert.deepEqual(heard, ["warning", ""]);
  assert.throws(() => w.brick.dispatch($("#b"), "flash-clear"), /reserved/);
  // A leaving answer: the flash before the clear is not carried.
  w.brick._assign = () => {};
  effects = [{ kind: "flash", message: "gone", level: "info" }, { kind: "flash-clear" }, { kind: "flash", message: "kept", level: "info" }, { kind: "redirect", url: "/next" }];
  click(w, $("#b"));
  await tick();
  const saved = JSON.parse(w.sessionStorage.getItem("brick:flash"));
  assert.deepEqual(saved.flashes.map((f) => f.message), ["kept"]);
}

console.log("v21 ok");
}

main().catch((err) => { console.error(err); process.exit(1); });
