// Tests runtime.js's server push in jsdom with a fake
// EventSource: one stream per page carrying every listen token, a
// "refresh" event refreshing exactly the roots that listen to its topic
// (coalesced per tick), hidden tabs deferring all but keep-alive roots,
// no reconnect when an action only refreshed a token, a catch-up refresh
// after a reconnect, backoff after a refused stream, and closing once no
// listener is left.
import { readFileSync } from "node:fs";
import assert from "node:assert/strict";
import { JSDOM, VirtualConsole } from "jsdom";
import { tick } from "./dom.mjs";

const runtimeJS = readFileSync(new URL("../runtime.js", import.meta.url), "utf8");
const ENVELOPE = "application/vnd.brick.action+json";
// RETRY_MS is the runtime's first wait after a refused stream (PUSH_RETRY_MS).
const RETRY_MS = 1000;

// root renders one listening stateful root; tok is its listen token.
const root = (id, topics, tok, extra = "") =>
  `<div data-brick="${id}" data-brick-state="H${id}" data-brick-token="T" data-brick-listen="${topics}" data-brick-listen-token="${tok}"${extra}><p>${id}</p></div>`;

async function setup(html, answer = (handle) => null) {
  const dom = new JSDOM(`<!doctype html><body>${html}</body>`, { url: "https://site.test/", runScripts: "outside-only", pretendToBeVisual: true, virtualConsole: new VirtualConsole() });
  const w = dom.window;
  const sent = [];
  w.fetch = (url, init = {}) => {
    const b = JSON.parse(init.body);
    sent.push(b.handle + ":" + b.action);
    const h = answer(b.handle) || w.document.querySelector(`[data-brick-state="${b.handle}"]`).outerHTML;
    return Promise.resolve({ status: 200, ok: true, statusText: "OK", headers: { get: () => ENVELOPE }, text: async () => JSON.stringify({ v: 1, html: h, effects: [] }) });
  };
  const streams = [];
  w.EventSource = class {
    constructor(url, opts) { this.url = url; this.opts = opts; this.readyState = 0; this.closed = false; this.handlers = {}; streams.push(this); }
    addEventListener(type, fn) { (this.handlers[type] ||= []).push(fn); }
    close() { this.closed = true; this.readyState = 2; }
    fire(type, data) { (this.handlers[type] || []).forEach((fn) => fn({ type, data })); }
  };
  let hidden = false;
  Object.defineProperty(w.document, "hidden", { configurable: true, get: () => hidden });
  w.eval(runtimeJS);
  await tick();
  const hide = (on) => { hidden = on; w.document.dispatchEvent(new w.Event("visibilitychange")); };
  return { w, sent, streams, hide };
}

const tokensOf = (url) => new URL(url, "https://site.test/").searchParams.getAll("l");

async function main() {
// 1. One stream for the page, at the events endpoint, with every token;
// a topic refreshes exactly its listeners, and a burst coalesces to one
// refresh per root.
{
  const { sent, streams } = await setup(root("a", "orders order:1", "TA") + root("b", "orders", "TB") + root("c", "chat", "TC"));
  assert.equal(streams.length, 1, "one EventSource per page");
  const [es] = streams;
  assert.equal(new URL(es.url, "https://site.test/").pathname, "/_brick/events");
  assert.deepEqual(tokensOf(es.url).sort(), ["TA", "TB", "TC"], "every token, no topic names");
  es.fire("open");
  es.fire("refresh", "order:1");
  await tick();
  assert.deepEqual(sent, ["Ha:$refresh"], "only the root listening to order:1");
  sent.length = 0;
  es.fire("refresh", "orders");
  es.fire("refresh", "orders");
  es.fire("refresh", "orders");
  await tick();
  assert.deepEqual(sent.sort(), ["Ha:$refresh", "Hb:$refresh"], "a burst is one refresh per root");
  sent.length = 0;
  es.fire("refresh", "nobody");
  await tick();
  assert.deepEqual(sent, [], "a topic nobody listens to refreshes nothing");
}

// 2. An action re-render brings a fresh token for the same topics: the
// stream stays; a changed topic set reopens it with the new tokens.
{
  let next = null;
  const { w, sent, streams } = await setup(root("a", "orders", "T1"), () => next);
  const [es] = streams;
  es.fire("open");
  next = root("a", "orders", "T2");
  es.fire("refresh", "orders");
  await tick();
  assert.deepEqual(sent, ["Ha:$refresh"]);
  assert.equal(w.document.querySelector("[data-brick-state]").getAttribute("data-brick-listen-token"), "T2", "the morph kept the fresh token");
  await tick();
  assert.equal(streams.length, 1, "a fresh token alone does not reconnect");
  w.document.body.insertAdjacentHTML("beforeend", root("b", "chat", "T3"));
  await tick();
  assert.equal(streams.length, 2, "a new topic reopens the stream");
  assert.ok(es.closed, "the old stream is closed");
  assert.deepEqual(tokensOf(streams[1].url).sort(), ["T2", "T3"], "with the current tokens");
  // Removing every listener closes the stream.
  w.document.querySelectorAll("[data-brick-state]").forEach((el) => el.remove());
  await tick();
  assert.ok(streams[1].closed, "no listener left, no stream");
  assert.equal(streams.length, 2);
}

// 3. A hidden tab defers refreshes until it is shown, except for a
// keep-alive root; a topic heard twice while hidden refreshes once.
{
  const { sent, streams, hide } = await setup(root("a", "orders", "TA") + root("k", "orders", "TK", " data-brick-listen-keep"));
  const [es] = streams;
  es.fire("open");
  hide(true);
  es.fire("refresh", "orders");
  es.fire("refresh", "orders");
  await tick();
  assert.deepEqual(sent, ["Hk:$refresh"], "only keep-alive refreshes while hidden");
  sent.length = 0;
  hide(false);
  await tick();
  assert.deepEqual(sent, ["Ha:$refresh"], "the deferred root refreshes once shown, once");
}

// 4. After a dropped connection the browser reconnects by itself; the
// reopened stream refreshes every listener once (what was published
// meanwhile is not replayed). The first open does not.
{
  const { sent, streams } = await setup(root("a", "orders", "TA") + root("b", "chat", "TB"));
  const [es] = streams;
  es.fire("open");
  await tick();
  assert.deepEqual(sent, [], "the first open refreshes nothing");
  es.readyState = 0; // CONNECTING: the browser retries
  es.fire("error");
  es.fire("open");
  await tick();
  assert.deepEqual(sent.sort(), ["Ha:$refresh", "Hb:$refresh"], "a reconnect catches up");
  assert.equal(streams.length, 1, "the browser's own reconnect, not a new stream");
}

// 5. A refused stream (403, 503) closes; the runtime opens a new one after
// a backoff, and gives up after its retries with a push-error event.
{
  const { w, streams } = await setup(root("a", "orders", "TA"));
  const errors = [];
  w.brick.on("push-error", (d) => errors.push(d));
  streams[0].readyState = 2;
  streams[0].fire("error");
  await tick();
  assert.equal(streams.length, 1, "no retry before the backoff");
  await tick(RETRY_MS + 50);
  assert.equal(streams.length, 2, "retried after the backoff");
  assert.deepEqual(tokensOf(streams[1].url), ["TA"]);
  streams[1].fire("open");
  assert.deepEqual(errors, []);
}
}

main().then(() => console.log("push_test ok"), (err) => { console.error(err); process.exit(1); });
