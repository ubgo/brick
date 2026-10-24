// Tests nested stateful children in jsdom: a
// parent's action sends the stateful children it holds, and the morph
// keeps each one the server answers with a <brick-keep> placeholder.
import { readFileSync } from "node:fs";
import assert from "node:assert/strict";
import { JSDOM, VirtualConsole } from "jsdom";
import { tick, click } from "./dom.mjs";

const runtimeJS = readFileSync(new URL("../runtime.js", import.meta.url), "utf8");

const page = `<!doctype html><body>
<section id="panel" data-brick="p1" data-brick-state="PH" data-brick-token="PT">
  <i id="n">0</i>
  <button id="go" brick:click="next">next</button>
  <div id="row" data-brick="r1" data-brick-state="RH" data-brick-token="RT">
    <input id="typed" value="">
    <button id="toggle" brick:click="toggle">t</button>
    <article id="grand" data-brick="g1" data-brick-state="GH" data-brick-token="GT"><b id="leaf" brick:click="x">grandchild</b></article>
  </div>
  <div id="row2" data-brick="r2" data-brick-state="R2H" data-brick-token="R2T">second</div>
</section>
</body>`;

function setup(respond) {
  const dom = new JSDOM(page, { url: "https://site.test/", runScripts: "outside-only", virtualConsole: new VirtualConsole() });
  const w = dom.window;
  const sent = [];
  w.fetch = async (url, init) => {
    sent.push(JSON.parse(init.body));
    return { status: 200, ok: true, statusText: "OK", text: async () => respond(sent.at(-1)) };
  };
  w.eval(runtimeJS);
  return { w, doc: w.document, sent };
}

// The parent sends its direct stateful children only (not the grandchild),
// and the kept child survives the morph: same element, typed value and
// all; a child the answer renders fresh (r2) is morphed as before.
{
  const { w, doc, sent } = setup(() => `<section data-brick="p1" data-brick-state="PH2" data-brick-token="PT"><i id="n">1</i><button id="go" brick:click="next">next</button><brick-keep data-brick="r1"></brick-keep><div id="row2" data-brick="r2" data-brick-state="R2H-new" data-brick-token="R2T-new">fresh</div></section>`);
  const row = doc.getElementById("row");
  doc.getElementById("typed").value = "half-typed";
  click(w, doc.getElementById("go"));
  await tick();
  assert.deepEqual(sent[0].children, { r1: { handle: "RH", token: "RT" }, r2: { handle: "R2H", token: "R2T" } }, "direct children only, with their handles");
  assert.equal(doc.getElementById("n").textContent, "1", "parent re-rendered");
  assert.equal(doc.getElementById("row"), row, "kept child is the same element");
  assert.equal(row.getAttribute("data-brick-state"), "RH", "kept child keeps its handle");
  assert.equal(doc.getElementById("typed").value, "half-typed", "kept child keeps what was typed");
  assert.equal(doc.querySelector("brick-keep"), null, "no placeholder left in the page");
  assert.equal(doc.getElementById("row2").textContent, "fresh", "a child rendered fresh is morphed");
  assert.equal([...doc.querySelector("[data-brick=p1]").children].map((e) => e.id || e.tagName).join(","), "n,go,row,row2", "order kept");
}

// Each stateful root speaks for its own children; a leaf sends no
// children key, so older servers and exact-body checks see no change.
{
  const { w, doc, sent } = setup((body) => body.handle === "RH"
    ? `<div data-brick="r1" data-brick-state="RH" data-brick-token="RT"><brick-keep data-brick="g1"></brick-keep></div>`
    : `<article data-brick="g1" data-brick-state="GH" data-brick-token="GT"><b id="leaf" brick:click="x">g</b></article>`);
  const grand = doc.getElementById("grand");
  click(w, doc.getElementById("toggle"));
  await tick();
  assert.deepEqual(sent[0].children, { g1: { handle: "GH", token: "GT" } }, "the row speaks for its own child");
  assert.equal(doc.getElementById("grand"), grand, "the row's kept child survives its morph");
  click(w, doc.getElementById("leaf"));
  await tick();
  assert.equal("children" in sent[1], false, "no children, no key");
}

console.log("keep: ok");
