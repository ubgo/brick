// Tests runtime.js's automatic retry of a refused action and the brick-config meta
// (H4, H5, H13) in jsdom. setTimeout is wrapped so a test reads the waits
// the runtime asked for without sleeping through them.
import { readFileSync } from "node:fs";
import assert from "node:assert/strict";
import { JSDOM, VirtualConsole } from "jsdom";
import { tick, click, fill } from "./dom.mjs";

const runtimeJS = readFileSync(new URL("../runtime.js", import.meta.url), "utf8");
const ENVELOPE = "application/vnd.brick.action+json";
const windows = [];

// root is a stateful component with handle h and a counter button.
const root = (h, extra = "") => `<div id="r" data-brick="b1" data-brick-state="${h}" data-brick-token="T"${extra}><input id="name" brick:model="name" value=""><button id="inc" type="button" brick:click="inc">+</button></div>`;
const envelope = (html) => JSON.stringify({ v: 1, html, effects: [] });

// setup loads page (with an optional brick-config meta) and the runtime.
// respond(req) answers each fetch with {status, retryAfter, body, type};
// a 200 POST without a body answers the root with the next handle. Waits
// of holdMs are kept in pending until the test runs them.
function setup(page, respond, { config = null, holdMs = -1 } = {}) {
  const meta = config ? `<meta name="brick-config" content='${JSON.stringify(config)}'>` : "";
  const dom = new JSDOM(`<!doctype html><head>${meta}</head><body>${page}</body>`, { url: "https://site.test/p/", runScripts: "outside-only", virtualConsole: new VirtualConsole() });
  const w = dom.window;
  windows.push(w);
  const realTimeout = w.setTimeout.bind(w);
  const waits = [];
  const intervals = [];
  const pending = [];
  w.setTimeout = (fn, ms = 0, ...a) => {
    waits.push(ms);
    if (ms === holdMs) { pending.push(() => fn(...a)); return 0; }
    return realTimeout(fn, 0, ...a);
  };
  const realInterval = w.setInterval.bind(w);
  w.setInterval = (fn, ms) => { intervals.push(ms); return realInterval(() => {}, 1e6); };
  const sent = [];
  let n = 0;
  w.fetch = (url, init = {}) => {
    const req = { method: init.method || "GET", url, credentials: init.credentials, headers: init.headers || {}, body: init.body ? JSON.parse(init.body) : null };
    sent.push(req);
    const r = respond(req) || {};
    const status = r.status || 200;
    const head = { "Content-Type": r.type || (req.method === "GET" ? "text/html" : ENVELOPE), "Retry-After": r.retryAfter === undefined ? null : String(r.retryAfter) };
    const text = r.body !== undefined ? r.body : status < 400 ? envelope(root("H" + ++n)) : "refused";
    return Promise.resolve({ status, ok: status < 400, statusText: "x", headers: { get: (k) => head[k] === undefined ? null : head[k] }, text: async () => text });
  };
  w.eval(runtimeJS);
  w.brick._reload = () => {};
  const events = [];
  for (const e of ["action-retry", "action-error", "action-cancelled", "recovered", "upload-error"]) w.brick.on(e, (d) => events.push([e, d]));
  return { w, $: (id) => w.document.getElementById(id), sent, waits, intervals, pending, events, posts: () => sent.filter((r) => r.method === "POST") };
}

const tests = [];
const test = (name, fn) => tests.push([name, fn]);

test("503 with Retry-After: waits the seconds asked, sends once more with Brick-Retry, morphs the answer", async () => {
  let calls = 0;
  const t = setup(root("H0"), () => (++calls === 1 ? { status: 503, retryAfter: 2 } : {}));
  click(t.w, t.$("inc"));
  await tick(60);
  assert.equal(t.posts().length, 2);
  assert.ok(t.waits.includes(2000), "waited Retry-After: " + t.waits);
  assert.equal(t.posts()[0].headers["Brick-Retry"], undefined);
  assert.equal(t.posts()[1].headers["Brick-Retry"], "1");
  assert.equal(t.posts()[1].body.handle, "H0", "the retry carries the same handle");
  assert.equal(t.$("r").getAttribute("data-brick-state"), "H1", "the answer landed");
  assert.deepEqual(t.events.map((e) => e[0]), ["action-retry"]);
  assert.deepEqual([t.events[0][1].status, t.events[0][1].retry, t.events[0][1].wait], [503, 1, 2000]);
});

test("the retry bound: a second refusal is reported as action-error (default one retry)", async () => {
  const t = setup(root("H0"), () => ({ status: 503, retryAfter: 1 }));
  click(t.w, t.$("inc"));
  await tick(60);
  assert.equal(t.posts().length, 2, "one retry, no more");
  assert.deepEqual(t.events.map((e) => e[0]), ["action-retry", "action-error"]);
});

test("brick-config retries sets the bound; 0 turns retrying off", async () => {
  const three = setup(root("H0"), () => ({ status: 429, retryAfter: 0 }), { config: { retries: 3 } });
  click(three.w, three.$("inc"));
  await tick(80);
  assert.equal(three.posts().length, 4, "first send + 3 retries");
  assert.equal(three.events.filter((e) => e[0] === "action-error").length, 1);
  const off = setup(root("H0"), () => ({ status: 503, retryAfter: 0 }), { config: { retries: 0 } });
  click(off.w, off.$("inc"));
  await tick(40);
  assert.equal(off.posts().length, 1);
});

test("Retry-After as an HTTP date; no header waits the default second", async () => {
  let calls = 0;
  const at = new Date(Date.now() + 3000).toUTCString();
  const t = setup(root("H0"), () => (++calls === 1 ? { status: 503, retryAfter: at } : {}));
  click(t.w, t.$("inc"));
  await tick(60);
  const wait = t.events[0][1].wait;
  assert.ok(wait > 1000 && wait <= 3000, "date wait " + wait);
  let c2 = 0;
  const d = setup(root("H0"), () => (++c2 === 1 ? { status: 429 } : {}));
  click(d.w, d.$("inc"));
  await tick(60);
  assert.equal(d.events[0][1].wait, 1000);
  assert.equal(d.posts().length, 2);
});

test("a Retry-After past retryMaxWait is not waited for: action-error at once", async () => {
  const t = setup(root("H0"), () => ({ status: 429, retryAfter: 60 }));
  click(t.w, t.$("inc"));
  await tick(40);
  assert.equal(t.posts().length, 1);
  assert.deepEqual(t.events.map((e) => e[0]), ["action-error"]);
  let calls = 0;
  const long = setup(root("H0"), () => (++calls === 1 ? { status: 429, retryAfter: 60 } : {}), { config: { retryMaxWait: 60000 } });
  click(long.w, long.$("inc"));
  await tick(60);
  assert.equal(long.posts().length, 2, "a raised cap waits");
});

test("410 is never retried: it recovers in place", async () => {
  const t = setup(root("OLD"), (r) => {
    if (r.method === "GET") return { body: `<html><body>${root("NEW0")}</body></html>` };
    return r.body.handle === "OLD" ? { status: 410, retryAfter: 1 } : {};
  });
  click(t.w, t.$("inc"));
  await tick(80);
  assert.equal(t.events.filter((e) => e[0] === "action-retry").length, 0);
  assert.deepEqual(t.posts().map((r) => r.body.handle), ["OLD", "NEW0"], "the 410 is followed by the recovery's replay, not a resend");
  assert.ok(t.sent.some((r) => r.method === "GET" && r.headers["Brick-Recover"] === "b1"));
});

test("queue order holds: the next action waits for the retried one and reads its handle; data-loading stays on while waiting", async () => {
  let calls = 0;
  const t = setup(root("H0"), () => (++calls === 1 ? { status: 503, retryAfter: 2 } : {}), { holdMs: 2000 });
  click(t.w, t.$("inc"));
  click(t.w, t.$("inc"));
  await tick(40);
  assert.equal(t.posts().length, 1, "the second waits behind the refused first");
  assert.equal(t.pending.length, 1, "the retry is waiting");
  assert.ok(t.$("r").hasAttribute("data-loading"), "the root still shows loading");
  t.pending.shift()();
  await tick(60);
  assert.deepEqual(t.posts().map((r) => [r.body.handle, r.body.action]), [["H0", "inc"], ["H0", "inc"], ["H1", "inc"]]);
  assert.ok(!t.$("r").hasAttribute("data-loading"));
});

test("a request hook sees retry and can cancel it", async () => {
  const t = setup(root("H0"), () => ({ status: 503, retryAfter: 0 }));
  const seen = [];
  t.w.brick.hook("request", (r) => { seen.push(r.retry); if (r.retry > 0) r.cancel(); });
  click(t.w, t.$("inc"));
  await tick(40);
  assert.deepEqual(seen, [0, 1]);
  assert.equal(t.posts().length, 1);
  assert.deepEqual(t.events.map((e) => e[0]), ["action-retry", "action-cancelled"]);
});

test("a stream is never sent again", async () => {
  const t = setup(root("H0"), () => ({ status: 503, retryAfter: 0, type: "application/vnd.brick.stream+ndjson", body: '{"v":1,"error":{"message":"down"}}\n' }));
  click(t.w, t.$("inc"));
  await tick(40);
  assert.equal(t.posts().length, 1);
});

test("an upload's bytes are never sent again after a 503", async () => {
  const page = `<div id="r" data-brick="u1" data-brick-state="H" data-brick-token="T" data-brick-uploads='{"f":{"max_files":1}}'><input id="in" type="file" brick:upload="f"></div>`;
  const t = setup(page, () => ({}));
  const xhrs = [];
  t.w.XMLHttpRequest = class {
    constructor() { this.headers = {}; this.upload = {}; xhrs.push(this); }
    open(m, u) { this.method = m; this.url = u; }
    setRequestHeader(k, v) { this.headers[k] = v; }
    getResponseHeader(k) { return k === "Retry-After" ? "0" : k === "Content-Type" ? "text/plain" : null; }
    send() { this.status = 503; this.statusText = "x"; this.responseText = "down"; queueMicrotask(() => this.onload()); }
  };
  t.w.brick.upload(t.$("in"), "f", [new t.w.File(["xx"], "a.txt", { type: "text/plain" })]);
  await tick(60);
  assert.equal(xhrs.length, 1, "one PUT only");
  assert.equal(t.posts().length, 0);
  assert.ok(t.events.some((e) => e[0] === "upload-error"));
});

test("brick-config: debounce, pollMin (floored at 250 ms) and actionURL", async () => {
  const page = root("H0") + `<div data-brick="p1" data-brick-state="P" data-brick-token="T" brick:poll="100ms"></div>`;
  const t = setup(page, () => ({}), { config: { debounce: 400, pollMin: 300, actionURL: "https://api.example.com/_brick/action" } });
  fill(t.w, t.$("name"), "x");
  assert.ok(t.waits.includes(400), "debounce from the meta: " + t.waits);
  click(t.w, t.$("inc"));
  await tick(40);
  assert.ok(t.intervals.includes(300), "poll floor from the meta: " + t.intervals);
  assert.ok(t.posts().length > 0 && t.posts().every((r) => r.url === "https://api.example.com/_brick/action" && r.credentials === "include"), "a moved action server gets the session cookie");

  const low = setup(page, () => ({}), { config: { pollMin: 10 } });
  click(low.w, low.$("inc"));
  await tick(40);
  assert.equal(low.posts()[0].credentials, "same-origin", "the default never sends cookies elsewhere");
  assert.ok(low.intervals.includes(250), "never below the safety floor: " + low.intervals);

  const bad = new JSDOM(`<!doctype html><head><meta name="brick-config" content="{nope"></head><body>${root("H0")}</body>`, { runScripts: "outside-only", virtualConsole: new VirtualConsole() });
  windows.push(bad.window);
  bad.window.eval(runtimeJS);
  assert.ok(bad.window.brick, "a malformed meta keeps the defaults and the runtime loads");
});

let failed = 0;
for (const [name, fn] of tests) {
  try { await fn(); console.log("ok  ", name); } catch (err) { failed++; console.log("FAIL", name); console.log(err); }
}
windows.forEach((w) => w.close());
if (failed) { console.log(failed + " failed"); process.exit(1); }
console.log("retry ok");
