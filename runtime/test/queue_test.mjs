// Tests runtime.js's v5 behaviour in jsdom: the
// per-root action queue, recovery in place after 410, the loading counter,
// and the binding and morph fixes that came with it.
import { readFileSync } from "node:fs";
import assert from "node:assert/strict";
import { JSDOM, VirtualConsole } from "jsdom";
import { tick, click, fill } from "./dom.mjs";

const runtimeJS = readFileSync(new URL("../runtime.js", import.meta.url), "utf8");

// root renders a stateful form with handle h and a name field.
const root = (h, name = "", extra = "") => `<form class="c" data-brick="b1" data-brick-state="${h}" data-brick-token="T" brick:submit="save"${extra}><input id="name" brick:model="name" value="${name}"><span id="spin" brick:loading hidden>…</span><button id="inc" type="button" brick:click="inc">+</button><button id="go">go</button></form>`;

const windows = [];

// setup loads page with the runtime. respond(req) answers each fetch:
// req is {method, url, headers, body}; it returns {status, body, hold}.
// A held answer waits until release() is called (oldest first).
function setup(page, respond) {
  const dom = new JSDOM(`<!doctype html><body>${page}</body>`, { url: "https://site.test/join/", runScripts: "outside-only", virtualConsole: new VirtualConsole() });
  const w = dom.window;
  windows.push(w);
  const sent = [];
  const held = [];
  w.fetch = (url, init = {}) => {
    const req = { method: init.method || "GET", url, headers: init.headers || {}, body: init.body ? JSON.parse(init.body) : null };
    sent.push(req);
    const r = respond(req) || {};
    const answer = { status: r.status || 200, ok: !r.status || r.status < 400, statusText: "x", text: async () => r.body || "" };
    if (r.hold) return new Promise((res) => held.push(() => res(answer)));
    return Promise.resolve(answer);
  };
  w.eval(runtimeJS);
  let reloads = 0;
  w.brick._reload = () => reloads++;
  const events = [];
  for (const n of ["recovered", "state-lost", "action-error"]) w.brick.on(n, (d) => events.push([n, d]));
  return { w, doc: w.document, sent, events, reloads: () => reloads, release: () => held.shift()() };
}
const actions = (sent) => sent.filter((r) => r.method === "POST").map((r) => [r.body.handle, r.body.action, r.body.args]);

async function main() {
// 1. One action in flight per root; the next reads the handle the first
// response brought, and two waiting $sets of one field collapse.
{
  let n = 0;
  const { w, doc, sent, release } = setup(root("H0"), (r) => ({ hold: true, body: root("H" + ++n) }));
  click(w, doc.getElementById("inc"));
  click(w, doc.getElementById("inc"));
  w.brick.action(doc.getElementById("inc"), "$set", ["name", "a"]);
  w.brick.action(doc.getElementById("inc"), "$set", ["name", "ab"]);
  await tick();
  assert.equal(actions(sent).length, 1, "one in flight");
  for (let i = 0; i < 3; i++) { release(); await tick(); }
  assert.deepEqual(actions(sent), [["H0", "inc", []], ["H1", "inc", []], ["H2", "$set", ["name", "ab"]]], "sequential, current handle, $sets merged");
}

// 2. 410: fresh GET with Brick-Recover, morph, replay brick:model values
// that differ as $set, re-run the failed action once, emit "recovered".
{
  const { w, doc, sent, events, reloads } = setup(root("OLD"), (r) => {
    if (r.method === "GET") return { body: `<html><body><h1>x</h1>${root("NEW0")}</body></html>` };
    if (r.body.handle === "OLD") return { status: 410 };
    return { body: root(r.body.handle === "NEW0" ? "NEW1" : "NEW2", "Sara") };
  });
  const name = doc.getElementById("name");
  name.value = "Sara";
  click(w, doc.getElementById("inc"));
  await tick(60);
  const get = sent.find((r) => r.method === "GET");
  assert.equal(get.url, "https://site.test/join/");
  assert.equal(get.headers["Brick-Recover"], "b1");
  assert.deepEqual(actions(sent), [["OLD", "inc", []], ["NEW0", "inc", []]], "one replay request");
  assert.deepEqual(sent.filter((r) => r.method === "POST")[1].body.sets, [["name", "Sara"]], "the held value rides as sets");
  assert.equal(doc.getElementById("name"), name, "field kept");
  assert.equal(name.value, "Sara", "typed value kept");
  assert.deepEqual(events.map((e) => e[0]), ["recovered"]);
  assert.equal(reloads(), 0, "no reload");
}

// 2b. A named recover action receives the fields as one dict; "off" reloads;
// a cancelled "state-gone" does nothing; a still-410 server reloads at most
// RELOAD_MAX times, then emits "state-lost".
{
  const { w, doc, sent } = setup(root("OLD", "", ' brick:recover="restore"'), (r) => {
    if (r.method === "GET") return { body: root("NEW0", "", ' brick:recover="restore"') };
    return r.body.handle === "OLD" ? { status: 410 } : { body: root("NEW1", "", ' brick:recover="restore"') };
  });
  doc.getElementById("name").value = "Al";
  click(w, doc.getElementById("inc"));
  await tick(60);
  assert.deepEqual(actions(sent), [["OLD", "inc", []], ["NEW0", "restore", [{ name: "Al" }]], ["NEW1", "inc", []]]);

  const off = setup(root("OLD", "", ' brick:recover="off"'), () => ({ status: 410 }));
  click(off.w, off.doc.getElementById("inc"));
  await tick();
  assert.equal(off.reloads(), 1, "off reloads");
  assert.equal(off.sent.filter((r) => r.method === "GET").length, 0);

  const cancel = setup(root("OLD"), () => ({ status: 410 }));
  cancel.w.brick.on("state-gone", (d) => d.preventDefault());
  click(cancel.w, cancel.doc.getElementById("inc"));
  await tick();
  assert.equal(cancel.reloads() + cancel.sent.filter((r) => r.method === "GET").length, 0, "cancelled");

  const loop = setup(root("OLD"), (r) => r.method === "GET" ? { body: root("NEW") } : { status: 410 });
  for (let i = 0; i < 6; i++) { click(loop.w, loop.doc.getElementById("inc")); await tick(40); }
  assert.ok(loop.sent.filter((r) => r.method === "GET").length <= 2, "at most 2 recoveries per root");
  assert.equal(loop.reloads(), 3, "reload guard");
  assert.ok(loop.events.some((e) => e[0] === "state-lost"), "state-lost after the guard");
}

// 2c. A 5xx is never replayed and never recovers.
{
  const { w, doc, sent, events } = setup(root("H"), () => ({ status: 500, body: "boom" }));
  click(w, doc.getElementById("inc"));
  await tick();
  assert.equal(sent.length, 1);
  assert.equal(JSON.stringify(events.map(([n, { action, message, status }]) => [n, { action, message, status }])), JSON.stringify([["action-error", { action: "inc", message: "boom", status: 500 }]]));
}

// 3. Loading state is counted: the first of two actions to finish does not
// clear aria-busy or brick:loading.
{
  const { w, doc, release } = setup(root("H"), () => ({ hold: true, body: root("H") }));
  const form = doc.querySelector("form");
  click(w, doc.getElementById("inc"));
  click(w, doc.getElementById("inc"));
  release();
  await tick();
  assert.equal(form.getAttribute("aria-busy"), "true", "still busy");
  assert.equal(doc.getElementById("spin").hidden, false, "loading still shown");
  release();
  await tick();
  assert.equal(form.hasAttribute("aria-busy"), false);
  assert.equal(doc.getElementById("spin").hidden, true);
}

// 4. A submit sends a pending debounced brick:model value first.
{
  const { w, doc, sent } = setup(root("H"), () => ({ body: "" }));
  fill(w, doc.getElementById("name"), "Zoe");
  doc.querySelector("form").dispatchEvent(new w.SubmitEvent("submit", { bubbles: true, cancelable: true }));
  await tick();
  assert.deepEqual(actions(sent).map((a) => a[1]), ["$set", "save"]);
  await tick(400);
  assert.equal(actions(sent).length, 2, "the debounce does not send it again");
}

// 5. A listener that cancels the submit cancels the action.
{
  const { w, doc, sent } = setup(root("H"), () => ({ body: "" }));
  const form = doc.querySelector("form");
  form.addEventListener("submit", (e) => e.preventDefault());
  form.dispatchEvent(new w.SubmitEvent("submit", { bubbles: true, cancelable: true }));
  await tick();
  assert.equal(sent.length, 0);
}

// 6. Nothing is sent while an IME composition is open; the value goes on end.
{
  const { w, doc, sent } = setup(root("H"), () => ({ body: "" }));
  const name = doc.getElementById("name");
  name.dispatchEvent(new w.CompositionEvent("compositionstart", { bubbles: true }));
  fill(w, name, "に");
  name.dispatchEvent(new w.InputEvent("input", { bubbles: true, isComposing: true }));
  await tick(400);
  assert.equal(sent.length, 0, "silent while composing");
  name.value = "日本";
  name.dispatchEvent(new w.CompositionEvent("compositionend", { bubbles: true }));
  await tick(400);
  assert.deepEqual(actions(sent), [["H", "$set", ["name", "日本"]]]);
}

// 8. brick:click may call a built-in.
{
  const { w, doc, sent } = setup(`<div data-brick="b1" data-brick-state="H" data-brick-token="T"><button id="t" brick:click='$toggle("open")'>t</button><button id="r" brick:click="$refresh">r</button></div>`, () => ({ body: "" }));
  click(w, doc.getElementById("t"));
  click(w, doc.getElementById("r"));
  await tick();
  assert.deepEqual(actions(sent), [["H", "$toggle", ["open"]], ["H", "$refresh", []]]);
}

// 9. A radio group's signal is the checked radio, not the last one.
{
  const { doc } = setup(`<div data-brick="b1" data-brick-state="H" data-brick-token="T"><input type="radio" name="p" value="a" brick:model="plan" checked><input type="radio" name="p" value="b" brick:model="plan"><b id="v" brick:text="plan"></b></div>`, () => ({ body: "" }));
  await tick(); // bound on DOMContentLoaded
  assert.equal(doc.getElementById("v").textContent, "a");
}

// 10. brick:keep names attributes a morph leaves as the client set them.
{
  const box = (h) => `<div data-brick="b1" data-brick-state="${h}" data-brick-token="T"><details id="d" brick:keep="open"><summary>s</summary></details><details id="e"><summary>s</summary></details><button id="inc" brick:click="inc">+</button></div>`;
  const { w, doc } = setup(box("H"), () => ({ body: box("H2") }));
  doc.getElementById("d").setAttribute("open", "");
  doc.getElementById("e").setAttribute("open", "");
  click(w, doc.getElementById("inc"));
  await tick();
  assert.equal(doc.getElementById("d").hasAttribute("open"), true, "kept");
  assert.equal(doc.getElementById("e").hasAttribute("open"), false, "without brick:keep, the server's wins");
}
}

try {
  await main();
  console.log("queue ok");
} catch (err) {
  console.error(err);
  process.exitCode = 1;
} finally {
  windows.forEach((w) => w.close());
}
