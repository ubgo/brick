// Tests streamed islands and brick.errors (G7) in jsdom: the
// runtime moves every <brick-fill> the inline script left (a CSP blocked
// it, or it carries assets) into its placeholder, at boot and when added
// later; brick.errors reads data-brick-errors on the stateful root.
import { readFileSync } from "node:fs";
import assert from "node:assert/strict";
import { JSDOM, VirtualConsole } from "jsdom";
import { tick } from "./dom.mjs";

const runtimeJS = readFileSync(new URL("../runtime.js", import.meta.url), "utf8");

const page = `<!doctype html><body>
<brick-lazy data-brick-stream="s1"><i id="wait1">loading</i></brick-lazy>
<p id="mid">mid</p>
<brick-lazy data-brick-stream="s2"><i id="wait2">loading</i></brick-lazy>
<form id="f" data-brick="f1" data-brick-state="FH" data-brick-token="FT" data-brick-errors='{"name":"Too short.","email":"Not an e-mail."}'><input id="email"></form>
<div id="g" data-brick="g1" data-brick-state="GH" data-brick-token="GT"><input id="other"></div>
<brick-fill data-brick-fill="s1" hidden><b id="one">one</b></brick-fill>
<brick-fill data-brick-fill="bad id" hidden><b id="bad">bad</b></brick-fill>
</body>`;

function setup() {
  const dom = new JSDOM(page, { url: "https://site.test/", runScripts: "outside-only", virtualConsole: new VirtualConsole() });
  const w = dom.window;
  w.eval(runtimeJS);
  const events = [];
  w.brick.on("island-loaded", (d) => events.push(d));
  return { w, doc: w.document, events };
}

// A fill already in the page at boot lands in its placeholder; one whose
// id is not engine-made is left alone.
{
  const { w, doc, events } = setup();
  await tick();
  assert.equal(doc.getElementById("wait1"), null, "placeholder replaced");
  const one = doc.getElementById("one");
  assert.ok(one && one.parentElement === doc.body, "content moved in place, no wrapper");
  assert.ok(one.compareDocumentPosition(doc.getElementById("mid")) & w.Node.DOCUMENT_POSITION_FOLLOWING, "content sits where the placeholder was");
  assert.equal(doc.querySelector('brick-fill[data-brick-fill="s1"]'), null, "fill element removed");
  assert.ok(doc.getElementById("bad"), "a foreign id is not used as a selector");

  // A fill added later, with assets: the assets are linked, then it lands.
  const f = doc.createElement("brick-fill");
  f.setAttribute("data-brick-fill", "s2");
  f.setAttribute("data-brick-assets", JSON.stringify([{ url: "/_brick/c/x.css", integrity: "sha384-x", css: true }]));
  f.hidden = true;
  f.innerHTML = `<b id="two">two</b>`;
  doc.body.appendChild(f);
  await tick();
  assert.equal(doc.getElementById("wait2"), null, "second placeholder replaced");
  assert.ok(doc.getElementById("two"), "second content in place");
  assert.ok(doc.querySelector('brick-assets link[href="/_brick/c/x.css"]'), "fill's stylesheet linked");
  assert.equal(events.filter((e) => e.stream).length, 2, "island-loaded per fill");
}

// brick.errors reads the root's last render.
{
  const { w, doc } = setup();
  const e = w.brick.errors(doc.getElementById("email"));
  assert.equal(e.has(), true);
  assert.equal(e.has("email"), true);
  assert.equal(e.has("nope"), false);
  assert.equal(e.get("email"), "Not an e-mail.");
  assert.equal(e.get("nope"), "");
  assert.equal(e.first(), "Not an e-mail.", "first by path order (email < name)");
  assert.equal(e.first("name"), "Too short.");
  assert.deepEqual({ ...e.all() }, { name: "Too short.", email: "Not an e-mail." });
  const cleared = [];
  w.brick.on("errors-cleared", (d) => cleared.push(d.path));
  e.clear("email");
  assert.equal(e.has("email"), false);
  assert.equal(e.has("name"), true);
  e.clear();
  assert.equal(e.has(), false);
  assert.equal(doc.getElementById("f").hasAttribute("data-brick-errors"), false);
  assert.deepEqual(cleared, ["email", ""]);
  // A morph that writes new errors is seen at once: each call reads the root.
  doc.getElementById("f").setAttribute("data-brick-errors", '{"email":"again"}');
  assert.equal(e.get("email"), "again");
  // A root without errors, a corrupt attribute and an element outside any root.
  assert.equal(w.brick.errors(doc.getElementById("other")).has(), false);
  doc.getElementById("g").setAttribute("data-brick-errors", "{not json");
  assert.deepEqual({ ...w.brick.errors(doc.getElementById("other")).all() }, {});
  assert.throws(() => w.brick.errors(doc.getElementById("mid")), /not inside a stateful component/);
}

console.log("streampage ok");
