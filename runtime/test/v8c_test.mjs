// Tests the v8 batch C runtime in jsdom: lazy islands and
// streamed action answers.
import { readFileSync } from "node:fs";
import assert from "node:assert/strict";
import { JSDOM, VirtualConsole } from "jsdom";
import { tick, click } from "./dom.mjs";

const runtimeJS = readFileSync(new URL("../runtime.js", import.meta.url), "utf8");
const ENVELOPE = "application/vnd.brick.action+json";
const ISLAND = "application/vnd.brick.island+json";
const STREAM = "application/vnd.brick.stream+ndjson";

// setup loads html with the runtime. islands maps a ticket to its answer
// ({status, html, assets}); act(body) answers an action with {type, text}.
// Observers are fakes the test fires by hand: observed lists what a
// "visible" placeholder watches, idle the queued idle callbacks.
async function setup(html, { islands = {}, act = null, url = "https://site.test/p/?q=1" } = {}) {
  const dom = new JSDOM(`<!doctype html><html><head><title>t</title></head><body><brick-assets><script type="module" src="/_brick/c/runtime.abc.js" nonce="N1"></script></brick-assets>${html}</body></html>`, { url, runScripts: "outside-only", pretendToBeVisual: true, virtualConsole: new VirtualConsole() });
  const w = dom.window;
  const gets = [];
  const posts = [];
  const observed = [];
  const idle = [];
  w.IntersectionObserver = class {
    constructor(cb) { this.cb = cb; this.els = []; }
    observe(el) { this.els.push(el); observed.push(this); }
    disconnect() { this.done = true; }
    fire() { if (!this.done) this.cb(this.els.map((target) => ({ target, isIntersecting: true }))); }
  };
  w.requestIdleCallback = (fn) => idle.push(fn);
  w.fetch = async (u, init = {}) => {
    if (init.method === "POST") {
      const body = JSON.parse(init.body);
      posts.push({ body, headers: init.headers });
      const a = act(body);
      return { status: a.status || 200, ok: !a.status || a.status < 400, statusText: "OK", headers: { get: () => a.type }, text: async () => a.text };
    }
    gets.push({ url: String(u), headers: init.headers });
    const t = new URL(String(u), url).searchParams.get("t");
    const ans = islands[t] || { status: 410 };
    const status = ans.status || 200;
    return {
      status, ok: status < 400, statusText: "", headers: { get: (h) => (h === "Brick-Island-Error" ? (ans.failed ? "1" : null) : ISLAND) },
      text: async () => JSON.stringify({ v: 1, html: ans.html || "", assets: ans.assets || [] }),
    };
  };
  w.brick_reloads = 0;
  w.eval(runtimeJS);
  w.brick._reload = () => { w.brick_reloads++; };
  await tick();
  return { w, doc: w.document, $: (id) => w.document.getElementById(id), gets, posts, observed, idle };
}

const lazy = (ticket, when, inner = "") => `<brick-lazy data-brick-lazy="${ticket}" data-when="${when}">${inner}</brick-lazy>`;

// ---- 14 · lazy islands ------------------------------------------------------

// load fetches at once with the page's query; visible waits for the
// observer (watching the loading block, the placeholder has no box);
// idle waits for the idle callback. Each answer replaces its placeholder,
// once.
{
  const { w, doc, $, gets, observed, idle } = await setup(
    `<main>${lazy("T1", "load", "<i>…</i>")}${lazy("T2", "visible", '<div id="skel" class="skeleton"></div>')}${lazy("T3", "idle")}</main>`,
    { islands: { T1: { html: '<p id="a">one</p>' }, T2: { html: '<p id="b">two</p>' }, T3: { html: '<p id="c">three</p>' } } },
  );
  await tick();
  assert.equal(gets.length, 1, "only load fetched");
  assert.match(gets[0].url, /^\/_brick\/island\?t=T1$/, "beside the action endpoint");
  assert.equal(gets[0].headers.Accept, ISLAND);
  assert.equal(gets[0].headers["Brick-Query"], "q=1", "the page's query rides along");
  assert.equal($("a").textContent, "one");
  assert.equal(doc.querySelector('brick-lazy[data-brick-lazy="T1"]'), null, "placeholder replaced");
  assert.equal(doc.querySelector('brick-lazy[data-brick-lazy="T2"]').style.display, "contents", "a placeholder adds no box");
  assert.equal(observed.length, 1, "visible observes");
  assert.equal(observed[0].els[0], $("skel"), "it watches the loading block");
  observed[0].fire();
  await tick();
  assert.equal($("b").textContent, "two");
  assert.equal(gets.length, 2);
  assert.equal(idle.length, 1, "idle queued");
  idle[0]();
  await tick();
  assert.equal($("c").textContent, "three");
  observed[0].fire();
  await tick();
  assert.equal(gets.length, 3, "one request per island");
  void w;
}

// The answer's assets the page lacks are appended once, with the page's
// nonce; a lazy answer may hold another lazy island; an empty visible
// placeholder gets a sentinel to observe.
{
  const { doc, $, gets, observed } = await setup(lazy("T1", "load"), {
    islands: {
      T1: { html: `<section id="outer">outer ${lazy("T2", "visible")}</section>`, assets: [{ url: "/_brick/c/runtime.abc.js", integrity: "x" }, { url: "/_brick/c/Chart.1.css", integrity: "sha384-a", css: true }, { url: "/_brick/c/Chart.1.js", integrity: "sha384-b" }] },
      T2: { html: '<p id="inner">inner</p>', assets: [{ url: "/_brick/c/Chart.1.css", integrity: "sha384-a", css: true }] },
    },
  });
  await tick();
  const links = [...doc.querySelectorAll("brick-assets link, brick-assets script")].map((e) => e.getAttribute("href") || e.getAttribute("src"));
  assert.deepEqual(links, ["/_brick/c/runtime.abc.js", "/_brick/c/Chart.1.css", "/_brick/c/Chart.1.js"], "missing assets appended once");
  const mod = doc.querySelector('script[src="/_brick/c/Chart.1.js"]');
  assert.equal(mod.type, "module");
  assert.equal(mod.getAttribute("integrity"), "sha384-b");
  assert.equal(mod.getAttribute("nonce"), "N1", "the page's nonce");
  assert.ok($("outer"), "outer landed");
  assert.equal(observed.length, 1, "the nested placeholder is observed");
  assert.equal(observed[0].els[0].tagName, "SPAN", "an empty placeholder gets a sentinel");
  observed[0].fire();
  await tick();
  assert.equal($("inner").textContent, "inner", "nested lazy island landed");
  assert.equal(gets.length, 2);
  assert.equal(doc.querySelectorAll('link[href="/_brick/c/Chart.1.css"]').length, 1, "no duplicate asset");
}

// A placeholder a morph inserts loads too; an expired ticket (410) reloads
// the page once; a failure emits island-error and leaves the placeholder.
{
  const html = `<div id="r" data-brick="c1" data-brick-state="default.H" data-brick-token="T"><button id="go" brick:click="more">more</button></div>`;
  const { w, $, gets } = await setup(html + lazy("OLD", "load", '<i id="wait">…</i>') + lazy("BAD", "load", '<i id="bad">…</i>'), {
    islands: { NEW: { html: '<p id="new">new</p>' }, BAD: { status: 500 } },
    act: () => ({ type: ENVELOPE, text: JSON.stringify({ v: 1, version: 1, effects: [], html: `<div id="r" data-brick="c1" data-brick-state="default.H2" data-brick-token="T"><button id="go" brick:click="more">more</button>${lazy("NEW", "load", "<i>…</i>")}</div>` }) }),
  });
  const errors = [];
  w.brick.on("island-error", (d) => errors.push(d.message));
  await tick();
  assert.equal(w.brick_reloads, 1, "410 reloads once");
  assert.ok($("wait"), "the stale placeholder stays until the reload");
  click(w, $("go"));
  await tick(60);
  assert.equal($("new").textContent, "new", "a morph-inserted placeholder loads");
  assert.ok(gets.some((g) => g.url.endsWith("t=NEW")));
  void errors;
}
{
  const { w, $ } = await setup(lazy("BAD", "load", '<i id="bad">…</i>'), { islands: { BAD: { status: 500 } } });
  const errors = [];
  w.brick.on("island-error", (d) => errors.push(d.message));
  await tick();
  // The listener may attach after the first fetch settled; fire another.
  w.document.body.insertAdjacentHTML("beforeend", lazy("BAD", "load", '<i id="bad2">…</i>'));
  await tick();
  assert.ok(errors.length >= 1 && /500/.test(errors[0]), "island-error fires");
  assert.ok($("bad"), "a failed island keeps its placeholder");
}

// ---- 16 · streamed action answers --------------------------------------------

const chat = `<div id="r" data-brick="c1" data-brick-state="default.H" data-brick-token="T"><p id="ans" brick:ref="answer">old</p><button id="ask" brick:click="ask">ask</button></div>`;
const final = (answer) => JSON.stringify({ v: 1, version: 1, effects: [], html: `<div id="r" data-brick="c1" data-brick-state="default.H2" data-brick-token="T"><p id="ans" brick:ref="answer">${answer}</p><button id="ask" brick:click="ask">ask</button></div>` });
const line = (ref, text, replace) => JSON.stringify({ stream: Object.assign({ ref, text }, replace ? { replace: true } : {}) });

// BV: chunks are appended in order as text (never parsed as HTML), replace
// resets the text, and the final morph wins. Writing chunks with
// innerHTML makes the <b> an element and fails "as text".
{
  let seen = null;
  const { w, $, posts } = await setup(chat, {
    act: () => ({ type: STREAM, text: [line("answer", "Hel"), line("answer", "lo <b>x</b>"), line("missing", "dropped")].join("\n") + "\n" + final("Hello &lt;b&gt;x&lt;/b&gt;") }),
  });
  w.brick.hook("morph", ({ from }) => { if (from.id === "ans" && seen === null) seen = from.textContent; });
  const warned = [];
  w.console.warn = (m) => warned.push(m);
  click(w, $("ask"));
  await tick(60);
  assert.match(posts[0].headers.Accept, /application\/vnd\.brick\.stream\+ndjson/, "the runtime accepts a stream");
  assert.equal(seen, "oldHello <b>x</b>", "chunks appended in order, before the final morph");
  assert.equal($("ans").children.length, 0, "chunks land as text");
  assert.equal($("ans").textContent, "Hello <b>x</b>", "the final render wins");
  assert.equal($("r").getAttribute("data-brick-state"), "default.H2", "the envelope landed");
  assert.equal(warned.length, 1, "an unknown ref warns once");
}
{
  const { w, $ } = await setup(chat, {
    act: () => ({ type: STREAM, text: [line("answer", "draft"), line("answer", "final", true)].join("\n") + "\n" + final("done") }),
  });
  let mid = null;
  w.brick.hook("morph", ({ from }) => { if (from.id === "ans" && mid === null) mid = from.textContent; });
  click(w, $("ask"));
  await tick(60);
  assert.equal(mid, "final", "replace resets the text");
  assert.equal($("ans").textContent, "done");
}

// An error line fires action-error; the streamed text stays.
{
  const { w, $ } = await setup(chat, {
    act: () => ({ type: STREAM, text: line("answer", " partial") + "\n" + JSON.stringify({ v: 1, error: { status: 422, message: "the action failed (error abc)" } }) + "\n" }),
  });
  const errs = [];
  w.brick.on("action-error", (d) => errs.push(d.message));
  click(w, $("ask"));
  await tick(60);
  assert.deepEqual(errs, ["the action failed (error abc)"], "action-error with the message");
  assert.equal($("ans").textContent, "old partial", "streamed text stays until the next render");
  assert.equal($("r").getAttribute("data-brick-state"), "default.H", "no morph");
}

// A readable body is applied chunk by chunk as it arrives, a line split
// across reads included.
{
  const { w, $ } = await setup(chat, { act: () => ({ type: ENVELOPE, text: final("x") }) });
  w.TextDecoder = TextDecoder;
  const enc = new TextEncoder();
  const first = line("answer", "A") + "\n";
  const second = line("answer", "B") + "\n";
  let release;
  const gate = new Promise((r) => { release = r; });
  const reads = [first + second.slice(0, 7), second.slice(7), "GATE", final("end") + "\n"];
  w.fetch = async () => ({
    status: 200, ok: true, statusText: "OK", headers: { get: () => STREAM },
    body: { getReader: () => ({ read: async () => {
      if (!reads.length) return { done: true };
      let p = reads.shift();
      if (p === "GATE") { await gate; p = reads.shift(); }
      return { done: false, value: enc.encode(p) };
    } }) },
  });
  click(w, $("ask"));
  await tick(30);
  assert.equal($("ans").textContent, "oldAB", "chunks show before the answer ends");
  release();
  await tick(60);
  assert.equal($("ans").textContent, "end", "then the envelope lands");
}

console.log("v8c: ok");
