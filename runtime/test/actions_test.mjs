// Tests the action half of runtime.js in jsdom.
import { readFileSync } from "node:fs";
import assert from "node:assert/strict";
import { JSDOM, VirtualConsole } from "jsdom";
import { tick, click } from "./dom.mjs";

const runtimeJS = readFileSync(new URL("../runtime.js", import.meta.url), "utf8");

const page = `<!doctype html><body>
<div class="counter" data-brick="c1" data-brick-state="H" data-brick-token="T">
  <span id="n">0</span>
  <input id="keep" value="x">
  <button id="inc" brick:click="increment">+</button>
  <button id="add" brick:click='add(10, "a b", [1, true])'>+10</button>
  <form id="f" brick:submit="save"><input name="title" value="Hello"><button id="go">go</button></form>
  <select id="s" brick:change="pick"><option value="a">a</option><option value="b">b</option></select>
</div>
<button id="outside" brick:click="increment">not in a component</button>
</body>`;

function setup(respond) {
  const dom = new JSDOM(page, { url: "https://site.test/", runScripts: "outside-only", virtualConsole: new VirtualConsole() });
  const w = dom.window;
  const sent = [];
  w.fetch = async (url, init) => {
    sent.push({ url, init, body: JSON.parse(init.body) });
    const r = respond(sent.at(-1).body);
    return { status: r.status || 200, ok: !r.status || r.status < 400, statusText: "x", text: async () => r.body || "" };
  };
  w.eval(runtimeJS);
  let reloads = 0;
  w.brick._reload = () => reloads++;
  return { w, doc: w.document, sent, reloads: () => reloads };
}

// A click posts the handle, token, action and no args, and the response
// patches the component in place.
{
  const { w, doc, sent } = setup(() => ({ body: `<div class="counter" data-brick="c1" data-brick-state="H" data-brick-token="T"><span id="n">1</span><input id="keep" value="x"><button id="inc" brick:click="increment">+</button></div>` }));
  const keep = doc.getElementById("keep");
  keep.value = "typed";
  click(w, doc.getElementById("inc"));
  const root = doc.querySelector(".counter");
  assert.equal(root.getAttribute("aria-busy"), "true", "busy while the request runs");
  await tick();
  assert.equal(sent[0].url, "/_brick/action");
  assert.equal(sent[0].init.method, "POST");
  assert.equal(sent[0].init.headers["Brick-Action"], "1");
  assert.equal(JSON.stringify(sent[0].body), '{"handle":"H","token":"T","action":"increment","args":[]}');
  assert.equal(doc.getElementById("n").textContent, "1", "patched");
  assert.equal(doc.getElementById("keep"), keep, "unchanged element kept");
  assert.equal(keep.value, "typed", "typed value kept");
  assert.equal(root.hasAttribute("aria-busy"), false, "busy cleared");
  assert.equal(doc.querySelector(".counter"), root, "root kept, not replaced");
}

// Arguments are JSON literals; a form passes its fields; a select its value.
{
  const { w, doc, sent } = setup(() => ({ body: "" }));
  click(w, doc.getElementById("add"));
  doc.getElementById("f").dispatchEvent(new w.SubmitEvent("submit", { bubbles: true, cancelable: true }));
  const s = doc.getElementById("s");
  s.value = "b";
  s.dispatchEvent(new w.Event("change", { bubbles: true }));
  click(w, doc.getElementById("outside"));
  await tick();
  assert.equal(JSON.stringify(sent.map((x) => [x.body.action, x.body.args])), JSON.stringify([["add", [10, "a b", [1, true]]], ["save", [{ title: "Hello" }]], ["pick", ["b"]]]), "args; outside a component nothing is sent");
}

// 410 reloads the page; another error is reported and leaves the component.
{
  const gone = setup(() => ({ status: 410 }));
  click(gone.w, gone.doc.getElementById("inc"));
  await tick();
  assert.equal(gone.reloads(), 1, "expired state reloads");

  const bad = setup(() => ({ status: 422, body: "action failed" }));
  const errors = [];
  bad.w.brick.on("action-error", (d) => errors.push(d));
  click(bad.w, bad.doc.getElementById("inc"));
  await tick();
  // root, id and status say which component failed and how.
  assert.equal(JSON.stringify(errors.map(({ action, message, id, status }) => ({ action, message, id, status }))), JSON.stringify([{ action: "increment", message: "action failed", id: "c1", status: 422 }]));
  assert.equal(errors[0].root, bad.doc.querySelector('[data-brick="c1"]'));
  assert.equal(bad.doc.getElementById("n").textContent, "0", "component unchanged");
}

// A form with no named fields passes no extra argument.
{
  const { w, doc, sent } = setup(() => ({ body: "" }));
  doc.body.insertAdjacentHTML("beforeend", '<div data-brick-state="H2" data-brick-token="T"><form id="bound" brick:submit="save"><input brick:model="x"></form></div>');
  doc.getElementById("bound").dispatchEvent(new w.SubmitEvent("submit", { bubbles: true, cancelable: true }));
  await tick();
  assert.equal(JSON.stringify(sent.at(-1).body.args), "[]", "no fields, no argument");
}

// The engine's no-JavaScript fields never reach an action: a
// rewritten form posts exactly what it posted before the rewrite.
{
  const { w, doc, sent } = setup(() => ({ body: "" }));
  doc.body.insertAdjacentHTML("beforeend", '<div data-brick-state="H3" data-brick-token="T"><form id="nojs" brick:submit="save" method="post"><input brick:model="x" name="brick:model:x" value="1"><input name="title" value="Hi"><input type="hidden" name="brick:state" value="H3"><input type="hidden" name="brick:token" value="T"><input type="hidden" name="brick:action" value="save"></form></div>');
  doc.getElementById("nojs").dispatchEvent(new w.SubmitEvent("submit", { bubbles: true, cancelable: true }));
  await tick();
  assert.equal(JSON.stringify(sent.at(-1).body.args), '[{"title":"Hi"}]', "brick: fields filtered out");
  doc.getElementById("nojs").querySelector("[name=title]").remove();
  doc.getElementById("nojs").dispatchEvent(new w.SubmitEvent("submit", { bubbles: true, cancelable: true }));
  await tick();
  assert.equal(JSON.stringify(sent.at(-1).body.args), "[]", "only brick: fields: no argument");
}

// brick.action calls an action from code, with the same request a click
// makes; outside a component it rejects and sends nothing.
{
  const { w, doc, sent } = setup(() => ({ body: "" }));
  await w.brick.action(doc.getElementById("n"), "move", [3, "done"]);
  assert.equal(JSON.stringify(sent.at(-1).body), '{"handle":"H","token":"T","action":"move","args":[3,"done"]}');
  await assert.rejects(w.brick.action(doc.getElementById("outside"), "move"), /not inside a stateful component/);
  assert.equal(sent.length, 1, "nothing sent from outside");
}

// A patch keeps a mounted component's enhancement: unchanged server
// markup leaves it alone (no remount, live state kept); changed markup runs
// its cleanup and mounts it again, so a button its mount unhid stays shown.
{
  const enh = (n, hidden) => `<div class="counter" data-brick="c1" data-brick-state="H" data-brick-token="T"><span id="n">${n}</span><div data-brick-type="Enh" id="enh"><button id="plus"${hidden ? " hidden" : ""}>+</button><i>${n > 1 ? "changed" : "same"}</i></div><button id="inc" brick:click="increment">+</button></div>`;
  let n = 0;
  const { w, doc } = setup(() => ({ body: enh(++n, true) }));
  doc.querySelector(".counter").outerHTML = enh(0, true);
  let mounts = 0, cleanups = 0;
  w.brick.mount("Enh", (el) => { mounts++; el.querySelector("#plus").hidden = false; el.dataset.live = String(mounts); return () => cleanups++; });
  await tick();
  assert.equal(mounts, 1);
  click(w, doc.getElementById("inc"));
  await tick();
  assert.equal(mounts, 1, "unchanged server markup: no remount");
  assert.equal(doc.getElementById("enh").dataset.live, "1", "live state kept");
  assert.equal(doc.getElementById("plus").hidden, false, "enhancement kept");
  click(w, doc.getElementById("inc"));
  await tick();
  await tick();
  assert.equal(cleanups, 1, "changed markup: cleanup ran");
  assert.equal(mounts, 2, "and it mounted again");
  assert.equal(doc.getElementById("plus").hidden, false, "enhancement re-applied");
  assert.equal(doc.querySelector("#enh i").textContent, "changed");
}

// A malformed call (arguments must be JSON) is reported, not swallowed.
{
  const { w, doc, sent } = setup(() => ({ body: "" }));
  doc.querySelector(".counter").insertAdjacentHTML("beforeend", `<button id="bad" brick:click="mode('x')">x</button>`);
  const errors = [];
  w.brick.on("action-error", (d) => errors.push(d));
  const origErr = w.console.error; w.console.error = () => {};
  click(w, doc.getElementById("bad"));
  w.console.error = origErr;
  await tick();
  assert.equal(sent.length, 0, "nothing sent");
  assert.equal(errors.length, 1);
  assert.equal(errors[0].action, "mode('x')");
}

// A site mounted under a base path posts actions beside its runtime file,
// not to the root: /ui/_brick/c/runtime.x.js → /ui/_brick/action.
{
  const { w, doc, sent } = setup(() => ({ body: "" }));
  const sc = doc.createElement("script");
  sc.setAttribute("src", "/ui/_brick/c/runtime.3133e7e2.js");
  doc.head.append(sc);
  click(w, doc.getElementById("inc"));
  await tick();
  assert.equal(sent.at(-1).url, "/ui/_brick/action");
}

console.log("actions ok");
