// Tests runtime.js's v9 WP-1 features in jsdom: row streams, the
// submit effect, brick.reloadComponent after a failed action
// and the brick.debug / brick.latency console switches.
import { readFileSync } from "node:fs";
import assert from "node:assert/strict";
import { JSDOM, VirtualConsole } from "jsdom";
import { tick } from "./dom.mjs";

const runtimeJS = readFileSync(new URL("../runtime.js", import.meta.url), "utf8");
const ENVELOPE = "application/vnd.brick.action+json";

const windows = [];

// stateful renders a stateful root with its inner markup.
const stateful = (id, inner = "", handle = "H-" + id) => `<div data-brick="${id}" data-brick-state="${handle}" data-brick-token="T">${inner}</div>`;
// list is the chat component: a brick:ignore list the rows effects own.
const list = (rows = "") => `<ul brick:ref="log" brick:ignore>${rows}</ul><span id="n">x</span>`;
const envelope = (html, effects = [], status = 200) => ({ status, type: ENVELOPE, body: JSON.stringify({ v: 1, version: 1, html, effects }) });

// setup loads page with the runtime. respond(req) answers each request:
// req is the POST's JSON body plus {method, url, headers}; a GET (a
// reload's page fetch) has only those three.
function setup(page, respond) {
  const dom = new JSDOM(`<!doctype html><body>${page}</body>`, { url: "https://site.test/app/", runScripts: "outside-only", virtualConsole: new VirtualConsole() });
  const w = dom.window;
  windows.push(w);
  const sent = [];
  w.fetch = (url, init = {}) => {
    const req = { ...(init.body ? JSON.parse(init.body) : {}), method: init.method || "GET", url, headers: init.headers || {}, at: Date.now() };
    sent.push(req);
    const r = respond(req) || {};
    const type = r.type || "text/html";
    return Promise.resolve({ status: r.status || 200, ok: !r.status || r.status < 400, statusText: "x", headers: { get: (h) => (h.toLowerCase() === "content-type" ? type : null) }, text: async () => r.body || "" });
  };
  const warnings = [];
  const logs = [];
  w.console.warn = (m) => warnings.push(String(m));
  w.console.error = (m) => warnings.push(String(m));
  w.console.log = (...a) => logs.push(a);
  w.eval(runtimeJS);
  const submitted = [];
  w.brick._submit = (f) => submitted.push(f);
  const events = [];
  for (const n of ["action-error", "effect"]) w.brick.on(n, (d) => events.push([n, d]));
  return { w, doc: w.document, sent, events, warnings, logs, submitted };
}
const keys = (doc) => [...doc.querySelector('[brick\\:ref="log"]').children].map((li) => li.getAttribute("brick:key") + "=" + li.textContent);
const row = (key, text) => ({ key, html: `<li class="msg">${text}</li>` });

async function main() {
// 1. Rows are inserted at either end, keyed, updated in place and trimmed
// to limit from the other end; the next morph leaves the list alone.
{
  let effects = [];
  const { w, doc } = setup(stateful("a", list()), () => envelope(stateful("a", list()), effects));
  const root = doc.querySelector('[data-brick="a"]');
  effects = [{ kind: "rows", ref: "log", op: "insert", at: "bottom", rows: [row("1", "one"), row("2", "two")] }];
  await w.brick.action(root, "post");
  assert.deepEqual(keys(doc), ["1=one", "2=two"], "appended in order, keyed");
  const two = doc.querySelector('[brick\\:key="2"]');
  effects = [{ kind: "rows", ref: "log", op: "insert", at: "top", limit: 3, rows: [row("3", "three"), row("4", "four"), row("2", "TWO")] }];
  await w.brick.action(root, "post");
  assert.deepEqual(keys(doc), ["3=three", "4=four", "1=one"], "prepended in order; the bottom trimmed to the limit");
  assert.equal(two.isConnected, false, "the trimmed row is gone");
  effects = [{ kind: "rows", ref: "log", op: "insert", rows: [row("4", "FOUR")] }];
  const four = doc.querySelector('[brick\\:key="4"]');
  await w.brick.action(root, "post");
  assert.deepEqual(keys(doc), ["3=three", "4=FOUR", "1=one"], "an existing key updates where it stands");
  assert.equal(doc.querySelector('[brick\\:key="4"]'), four, "the same element, morphed");
  effects = [{ kind: "rows", ref: "log", op: "insert", at: "bottom", limit: 3, rows: [row("5", "five")] }];
  await w.brick.action(root, "post");
  assert.deepEqual(keys(doc), ["4=FOUR", "1=one", "5=five"], "appending trims the top");
  effects = [{ kind: "rows", ref: "log", op: "delete", keys: ["1", "nope"] }];
  await w.brick.action(root, "post");
  assert.deepEqual(keys(doc), ["4=FOUR", "5=five"], "delete by key; an unknown key is ignored");
  effects = [{ kind: "rows", ref: "log", op: "reset", rows: [row("9", "nine")] }];
  await w.brick.action(root, "post");
  assert.deepEqual(keys(doc), ["9=nine"], "reset empties, then inserts");
  effects = [];
  await w.brick.action(root, "post");
  assert.deepEqual(keys(doc), ["9=nine"], "a morph without effects leaves the rows");
}

// 2. Row HTML is parsed as the server's markup, so escaped text stays text;
// an unknown ref warns once and drops the rows.
{
  let effects = [{ kind: "rows", ref: "log", rows: [{ key: "x", html: `<li>&lt;script&gt;alert(1)&lt;/script&gt;</li>` }] }];
  const { w, doc, warnings } = setup(stateful("a", list()), () => envelope(stateful("a", list()), effects));
  const root = doc.querySelector('[data-brick="a"]');
  await w.brick.action(root, "post");
  assert.equal(doc.querySelectorAll("script").length, 0, "no script element");
  assert.equal(doc.querySelector('[brick\\:key="x"]').textContent, "<script>alert(1)</script>", "escaped text stays text");
  effects = [{ kind: "rows", ref: "missing", rows: [row("1", "a")] }];
  await w.brick.action(root, "post");
  await w.brick.action(root, "post");
  assert.equal(warnings.filter((m) => m.includes("rows: no brick:ref")).length, 1, "warned once");
}

// 3. submit natively submits the brick:ref form once the morph is in, after
// every other effect; a flash before it is carried to the next page.
{
  const form = `<form brick:ref="login" action="/session" method="post"><input name="t" value="tok"></form>`;
  const order = [];
  const { w, doc, submitted, warnings } = setup(stateful("a", form), () => envelope(stateful("a", form), [{ kind: "submit", ref: "login" }, { kind: "x-after" }, { kind: "flash", message: "Signing in" }]));
  w.brick.effect("x-after", () => order.push("after"));
  w.brick._submit = (f) => { order.push("submit"); submitted.push(f); };
  await w.brick.action(doc.querySelector('[data-brick="a"]'), "login");
  assert.deepEqual(order, ["after", "submit"], "submit runs last");
  assert.equal(submitted.length, 1);
  assert.equal(submitted[0], doc.querySelector("form"), "the brick:ref form");
  assert.match(w.sessionStorage.getItem("brick:flash") || "", /Signing in/, "the flash is carried");
  // A ref that is not a form is refused.
  const { w: w2, doc: doc2, submitted: s2, warnings: warn2 } = setup(stateful("a", `<div brick:ref="login"></div>`), () => envelope(stateful("a", `<div brick:ref="login"></div>`), [{ kind: "submit", ref: "login" }]));
  await w2.brick.action(doc2.querySelector('[data-brick="a"]'), "login");
  assert.equal(s2.length, 0, "no submit");
  assert.ok(warn2.some((m) => m.includes("submit: no brick:ref")), "warned");
  assert.equal(warnings.length, 0, String(warnings));
  // A ref on the stateful root itself (a component whose root is the form)
  // is found too.
  const rootForm = `<form data-brick="a" data-brick-state="H-a" data-brick-token="T" brick:ref="login" action="/session" method="post"></form>`;
  const { w: w3, doc: doc3, submitted: s3, warnings: warn3 } = setup(rootForm, () => envelope(rootForm, [{ kind: "submit", ref: "login" }]));
  await w3.brick.action(doc3.querySelector('[data-brick="a"]'), "login");
  assert.equal(s3.length, 1, "the root form is submitted: " + warn3.join("; "));
  assert.equal(s3[0], doc3.querySelector("form"));
}

// 4. A 500 fires action-error with the root and status; reloadComponent
// from that listener fetches the page and morphs the fresh component in,
// replaying nothing.
{
  const fresh = stateful("a", `<span id="n">fresh</span>`, "H-new");
  let page = 200;
  const { w, doc, sent, events } = setup(`<p>other</p>` + stateful("a", `<span id="n">stale</span>`), (r) => {
    if (r.method === "GET") return { status: page, body: `<!doctype html><body>${fresh}</body>` };
    return { status: 500, body: "boom" };
  });
  const reloads = [];
  w.brick.on("action-error", (d) => { if (d.status === 500) reloads.push(w.brick.reloadComponent(d.root)); });
  const root = doc.querySelector('[data-brick="a"]');
  await w.brick.action(root, "save");
  const err = events.find((e) => e[0] === "action-error")[1];
  assert.equal(err.status, 500, "status on action-error");
  assert.equal(err.root, root, "root on action-error");
  assert.equal(err.id, "a");
  await Promise.all(reloads);
  assert.equal(doc.getElementById("n").textContent, "fresh", "fresh render morphed in");
  assert.equal(root.getAttribute("data-brick-state"), "H-new", "new handle");
  const get = sent.find((r) => r.method === "GET");
  assert.equal(get.headers["Brick-Recover"], "a", "asks for the component");
  assert.equal(sent.filter((r) => r.method === "POST").length, 1, "nothing replayed");
  // A page that cannot be fetched rejects, and fires no action-error.
  page = 404;
  const before = events.length;
  await assert.rejects(w.brick.reloadComponent(root), /404/);
  assert.equal(events.length, before, "no action-error from a reload");
  await assert.rejects(w.brick.reloadComponent(doc.querySelector("p")), /not inside a stateful component/);
}

// 5. brick.debug logs each request and answer; brick.latency delays each
// request; both start off.
{
  const { w, doc, sent, logs } = setup(stateful("a", `<span id="n">0</span>`), () => envelope(stateful("a", `<span id="n">1</span>`)));
  const root = doc.querySelector('[data-brick="a"]');
  await w.brick.action(root, "inc");
  assert.equal(logs.length, 0, "off by default");
  w.brick.debug(true);
  await w.brick.action(root, "inc", [3]);
  assert.ok(logs.some((a) => a[0] === "brick:" && a[1] === "→" && a[2] === "inc"), "the request is logged");
  assert.ok(logs.some((a) => a[0] === "brick:" && a[1] === "←" && a[3] === 200 && a[4].v === 1), "the envelope is logged");
  w.brick.debug(false);
  const n = logs.length;
  w.brick.latency(60);
  const t0 = Date.now();
  const p = w.brick.action(root, "inc");
  await tick(10);
  assert.equal(sent.length, 2, "not sent yet");
  await p;
  assert.equal(sent.length, 3);
  assert.ok(sent[2].at - t0 >= 55, "sent after the latency");
  assert.equal(logs.length, n, "debug off again");
  w.brick.latency(0);
  const q = w.brick.action(root, "inc");
  assert.equal(sent.length, 4, "no latency: sent in the same task");
  await q;
}
}

try {
  await main();
  console.log("v9 ok");
} catch (err) {
  console.error(err);
  process.exitCode = 1;
} finally {
  windows.forEach((w) => w.close());
}
