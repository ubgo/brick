// Tests runtime.js's v6 half in jsdom: the
// action envelope, effects applied after the morph, flashes that survive
// leaving the page, and events delivered between components with their
// loop guards.
import { readFileSync } from "node:fs";
import assert from "node:assert/strict";
import { JSDOM, VirtualConsole } from "jsdom";
import { tick, click } from "./dom.mjs";

const runtimeJS = readFileSync(new URL("../runtime.js", import.meta.url), "utf8");
const ENVELOPE = "application/vnd.brick.action+json";

const windows = [];

// stateful renders a stateful root: id, handle, its on: listeners, extra
// attributes and inner markup.
const stateful = (id, { on, name, up, extra = "", inner = "" } = {}) =>
  `<div data-brick="${id}" data-brick-state="H-${id}" data-brick-token="T"${on ? ` data-brick-on='${JSON.stringify(on)}'` : ""}${name ? ` data-brick-name="${name}"` : ""}${up ? ` data-brick-up='${JSON.stringify(up)}'` : ""}${extra}>${inner}</div>`;

// envelope answers an action with html and effects as the v6 server does.
const envelope = (html, effects = [], status = 200) => ({ status, type: ENVELOPE, body: JSON.stringify({ v: 1, version: 1, html, effects }) });

// setup loads page with the runtime. respond(req) answers each POST: req
// is {handle, action, args, headers}; it returns {status, body, type,
// hold}. before(w) runs before the runtime starts (to seed storage).
function setup(page, respond, before) {
  const dom = new JSDOM(`<!doctype html><body>${page}</body>`, { url: "https://site.test/app/", runScripts: "outside-only", virtualConsole: new VirtualConsole() });
  const w = dom.window;
  windows.push(w);
  const sent = [];
  const held = [];
  w.fetch = (url, init = {}) => {
    const req = { ...JSON.parse(init.body), headers: init.headers };
    sent.push(req);
    const r = respond(req) || {};
    const type = r.type || "text/html";
    const answer = { status: r.status || 200, ok: !r.status || r.status < 400, statusText: "x", headers: { get: (h) => (h.toLowerCase() === "content-type" ? type : null) }, text: async () => r.body || "" };
    if (r.hold) return new Promise((res) => held.push(() => res(answer)));
    return Promise.resolve(answer);
  };
  const warnings = [];
  w.console.warn = (m) => warnings.push(String(m));
  w.console.error = (m) => warnings.push(String(m));
  if (before) before(w);
  w.eval(runtimeJS);
  const assigned = [];
  w.brick._assign = (u) => assigned.push(u);
  const events = [];
  for (const n of ["action-error", "event-loop", "effect"]) w.brick.on(n, (d) => events.push([n, d]));
  return { w, doc: w.document, sent, held, events, warnings, assigned, release: () => held.shift()() };
}
// plain copies a value from the jsdom realm, so deepEqual compares data
// rather than prototypes.
const plain = (x) => JSON.parse(JSON.stringify(x));
const calls = (sent) => sent.map((r) => [r.handle, r.action, r.args]);

async function main() {
// 1. The runtime asks for the envelope; its html is morphed in.
{
  const { w, doc, sent } = setup(stateful("a", { inner: `<span id="n">0</span>` }), () => envelope(stateful("a", { inner: `<span id="n">1</span>` })));
  await w.brick.action(doc.getElementById("n"), "inc");
  assert.match(sent[0].headers.Accept, /^application\/vnd\.brick\.action\+json/, "asks for the envelope");
  assert.equal(doc.getElementById("n").textContent, "1", "envelope html morphed");
}

// 2. Effects run after the morph, in recorded order; an unregistered kind
// warns and is emitted as "effect".
{
  const seen = [];
  const { w, doc, events, warnings } = setup(stateful("a", { inner: `<span id="n">0</span>` }), () => envelope(stateful("a", { inner: `<span id="n">1</span>` }), [
    { kind: "x-one", data: { k: 1 } }, { kind: "title", text: "(3) Inbox" }, { kind: "x-two" }, { kind: "x-unknown" },
  ]));
  w.brick.effect("x-one", (ef) => seen.push(["one", doc.getElementById("n").textContent, ef.data.k, doc.title]));
  w.brick.effect("x-two", () => seen.push(["two", doc.title]));
  await w.brick.action(doc.getElementById("n"), "inc");
  assert.deepEqual(seen, [["one", "1", 1, ""], ["two", "(3) Inbox"]], "after the morph, in order");
  assert.equal(doc.title, "(3) Inbox");
  assert.deepEqual(events.filter((e) => e[0] === "effect").map((e) => e[1].kind), ["x-unknown"], "unregistered kind emitted");
  assert.ok(warnings.some((m) => m.includes("x-unknown")), "unregistered kind warned");
  assert.throws(() => w.brick.effect("confetti", () => {}), /x-/, "custom kinds start with x-");
}

// 3. The terminal effect runs last; while one is present a dispatch still
// reaches brick.on but enqueues no listener.
{
  const order = [];
  const page = stateful("a") + stateful("b", { on: { saved: "$refresh" } });
  const { w, doc, sent, assigned } = setup(page, (r) => (r.handle === "H-a" ? envelope(stateful("a"), [
    { kind: "redirect", url: "/contacts/42" }, { kind: "x-mark" }, { kind: "dispatch", name: "saved", data: { id: 1 } },
  ]) : envelope(stateful("b"))));
  w.brick.effect("x-mark", () => order.push("mark:" + assigned.length));
  w.brick.on("saved", (d) => order.push("saved:" + d.id));
  await w.brick.action(doc.querySelector('[data-brick="a"]'), "save");
  await tick();
  assert.deepEqual(order, ["mark:0", "saved:1"], "non-terminal first");
  assert.deepEqual(assigned, ["https://site.test/contacts/42"], "redirect applied");
  assert.deepEqual(calls(sent), [["H-a", "save", []]], "no listener enqueued while leaving");
}

// 4. A redirect URL the browser re-check refuses is not followed.
{
  const { w, doc, assigned } = setup(stateful("a"), () => envelope(stateful("a"), [{ kind: "redirect", url: "javascript:alert(1)" }]));
  await w.brick.action(doc.querySelector('[data-brick="a"]'), "go");
  assert.deepEqual(assigned, [], "javascript: refused");
}

// 5. navigate goes through nav.js's brick.navigate when it is loaded.
{
  const { w, doc, assigned } = setup(stateful("a"), () => envelope(stateful("a"), [{ kind: "navigate", url: "/next" }]));
  const went = [];
  w.brick.navigate = (u) => went.push(u);
  await w.brick.action(doc.querySelector('[data-brick="a"]'), "go");
  assert.deepEqual([went, assigned], [["https://site.test/next"], []], "in-place navigation");
}

// 6. A flash with no listener uses the default display; a "flash"
// listener replaces it; brick.effect("flash") replaces the handler.
{
  const answer = () => envelope(stateful("a"), [{ kind: "flash", message: "Saved", level: "success" }]);
  const s1 = setup(stateful("a"), answer);
  await s1.w.brick.action(s1.doc.querySelector('[data-brick="a"]'), "save");
  const box = s1.doc.getElementById("brick-flash");
  assert.equal(box && box.getAttribute("role"), "status", "default display");
  assert.equal(box.textContent, "Saved");

  const s2 = setup(stateful("a"), answer);
  const got = [];
  s2.w.brick.on("flash", (f) => got.push(f));
  await s2.w.brick.action(s2.doc.querySelector('[data-brick="a"]'), "save");
  assert.deepEqual(plain(got), [{ message: "Saved", level: "success" }], "flash emitted");
  assert.equal(s2.doc.getElementById("brick-flash"), null, "a listener replaces the default display");

  const s3 = setup(stateful("a"), answer);
  const mine = [];
  s3.w.brick.effect("flash", (ef) => mine.push(ef.message));
  await s3.w.brick.action(s3.doc.querySelector('[data-brick="a"]'), "save");
  assert.deepEqual(mine, ["Saved"], "handler replaced");
  assert.equal(s3.doc.getElementById("brick-flash"), null);
}

// 7. A flash survives a redirect in one sessionStorage entry, which the
// next page reads once; a stale or throwing storage loses it quietly.
{
  const { w, doc } = setup(stateful("a"), () => envelope(stateful("a"), [{ kind: "flash", message: "Bye", level: "info" }, { kind: "redirect", url: "/x" }]));
  const shown = [];
  w.brick.on("flash", (f) => shown.push(f));
  await w.brick.action(doc.querySelector('[data-brick="a"]'), "go");
  assert.deepEqual(shown, [], "not shown on the page being left");
  const stored = w.sessionStorage.getItem("brick:flash");
  assert.ok(stored && JSON.parse(stored).flashes[0].message === "Bye", "stored");

  const next = setup("", () => ({}), (nw) => nw.sessionStorage.setItem("brick:flash", stored));
  const got = [];
  next.w.brick.on("flash", (f) => got.push(f.message));
  await tick();
  assert.deepEqual(got, ["Bye"], "next page shows it");
  assert.equal(next.w.sessionStorage.getItem("brick:flash"), null, "deleted on read");

  const stale = setup("", () => ({}), (nw) => nw.sessionStorage.setItem("brick:flash", JSON.stringify({ at: Date.now() - 60000, flashes: [{ kind: "flash", message: "old" }] })));
  const old = [];
  stale.w.brick.on("flash", (f) => old.push(f));
  await tick();
  assert.deepEqual(old, [], "older than 30s is ignored");

  const blocked = setup(stateful("a"), () => envelope(stateful("a"), [{ kind: "flash", message: "x" }, { kind: "redirect", url: "/x" }]), (nw) => {
    Object.defineProperty(nw, "sessionStorage", { get() { throw new Error("SecurityError"); } });
  });
  await blocked.w.brick.action(blocked.doc.querySelector('[data-brick="a"]'), "go");
  assert.equal(blocked.assigned.length, 1, "redirect still happens with storage blocked");
}

// 8. A flash survives an in-place navigation and is shown once
// "navigated" fires.
{
  const { w, doc } = setup(stateful("a"), () => envelope(stateful("a"), [{ kind: "flash", message: "Moved" }, { kind: "navigate", url: "/next" }]));
  const shown = [];
  w.brick.on("flash", (f) => shown.push(f.message));
  w.brick.navigate = (u) => setTimeout(() => w.brick.emit("navigated", { url: u }));
  await w.brick.action(doc.querySelector('[data-brick="a"]'), "go");
  assert.deepEqual(shown, [], "held while navigating");
  await tick();
  assert.deepEqual(shown, ["Moved"], "shown after navigated");
  assert.equal(w.sessionStorage.getItem("brick:flash"), null, "storage copy cleared");
}

// 9. download clicks a temporary <a download>; url changes history only.
{
  const { w, doc, sent } = setup(stateful("a"), () => envelope(stateful("a"), [{ kind: "download", url: "/files/r.csv", filename: "report.csv" }, { kind: "url", url: "/app/?page=2" }]));
  const clicked = [];
  doc.addEventListener("click", (e) => { if (e.target.tagName === "A") { clicked.push([e.target.href, e.target.getAttribute("download")]); e.preventDefault(); } }, true);
  await w.brick.action(doc.querySelector('[data-brick="a"]'), "go");
  assert.deepEqual(clicked, [["https://site.test/files/r.csv", "report.csv"]], "download link clicked");
  assert.equal(doc.querySelectorAll("a").length, 0, "temporary link removed");
  assert.equal(w.location.search, "?page=2", "url pushed");
  assert.equal(sent.length, 1, "no fetch for url");
  const other = setup(stateful("a"), () => envelope(stateful("a"), [{ kind: "url", url: "https://evil.example/x" }]));
  await other.w.brick.action(other.doc.querySelector('[data-brick="a"]'), "go");
  assert.ok(other.warnings.some((m) => m.includes("effect URL refused")), "url is same-origin only");
}

// 10. A dispatch reaches brick.on, then every on: listener in document
// order except the source, with literal args first and the data last;
// listener requests carry the event chain.
{
  const roots = { a: stateful("a", { on: { "contact-deleted": "$refresh" } }), c: stateful("c", { on: { "contact-deleted": 'reload("x")' } }), b: stateful("b", { on: { "contact-deleted": "$refresh" } }), d: stateful("d", { on: { other: "$refresh" } }) };
  const page = roots.a + roots.c + roots.b + roots.d;
  const { w, doc, sent } = setup(page, (r) => envelope(roots[r.handle.slice(2)], r.handle === "H-a" && r.action === "remove" ? [{ kind: "dispatch", name: "contact-deleted", data: { id: 42 } }] : []));
  const bus = [];
  w.brick.on("contact-deleted", (d) => bus.push(d));
  await w.brick.action(doc.querySelector('[data-brick="a"]'), "remove", [42]);
  await tick();
  assert.deepEqual(plain(bus), [{ id: 42, $source: "a" }], "emitted with $source");
  assert.deepEqual(calls(sent), [["H-a", "remove", [42]], ["H-c", "reload", ["x", { id: 42 }]], ["H-b", "$refresh", []]], "document order, source excluded");
  assert.equal(sent[1].headers["Brick-Event-Chain"], "contact-deleted", "chain header");
}

// 11. to = a component name reaches only that name; to = "self" only the
// source's own listener.
{
  const roots = { a: stateful("a", { on: { ping: "$refresh" } }), b: stateful("b", { name: "Count", on: { ping: "$refresh" } }), c: stateful("c", { name: "Other", on: { ping: "$refresh" } }) };
  const page = roots.a + roots.b + roots.c;
  const s1 = setup(page, (r) => envelope(roots[r.handle.slice(2)], r.action === "go" ? [{ kind: "dispatch", name: "ping", to: "Count" }] : []));
  await s1.w.brick.action(s1.doc.querySelector('[data-brick="a"]'), "go");
  await tick();
  assert.deepEqual(calls(s1.sent).slice(1), [["H-b", "$refresh", []]], "to=Count");
  const s2 = setup(page, (r) => envelope(roots[r.handle.slice(2)], r.action === "go" ? [{ kind: "dispatch", name: "ping", to: "self" }] : []));
  await s2.w.brick.action(s2.doc.querySelector('[data-brick="a"]'), "go");
  await tick();
  assert.deepEqual(calls(s2.sent).slice(1), [["H-a", "$refresh", []]], "to=self");
}

// 12. A call-site listener (data-brick-up) hears only its own child: a
// stateful child's action, or a presentational child's client dispatch.
{
  const rowA = stateful("p-1", { up: { deleted: "row_deleted" }, inner: `<button id="del1">x</button>` });
  const rowB = stateful("p-2", { inner: `<button id="del2">x</button>` });
  const pres = `<span id="chip" data-brick-up='{"deleted":"chip_gone"}'><button id="x" brick:click='$dispatch("deleted", {"id": 7})'>x</button></span>`;
  const page = stateful("p", { inner: rowA + rowB + pres });
  const { w, doc, sent } = setup(page, (r) => envelope(r.handle === "H-p" ? null : stateful(r.handle.slice(2)), r.action === "remove" ? [{ kind: "dispatch", name: "deleted", data: { id: r.handle } }] : []));
  await w.brick.action(doc.getElementById("del1"), "remove");
  await tick();
  assert.deepEqual(calls(sent).slice(1), [["H-p", "row_deleted", [{ id: "H-p-1" }]]], "parent hears its child");
  sent.length = 0;
  await w.brick.action(doc.getElementById("del2"), "remove");
  await tick();
  assert.deepEqual(calls(sent).slice(1), [], "a child without brick:on: is not heard");
  sent.length = 0;
  click(w, doc.getElementById("x"));
  await tick();
  assert.deepEqual(calls(sent), [["H-p", "chip_gone", [{ id: 7 }]]], "presentational child heard by its writer");
}

// 13. Identical waiting listener jobs collapse into one.
{
  const page = stateful("b", { on: { tick: "$refresh" } }) + `<div id="out"></div>`;
  const { w, doc, sent, release } = setup(page, () => ({ hold: true, ...envelope(stateful("b", { on: { tick: "$refresh" } })) }));
  const out = doc.getElementById("out");
  for (let i = 0; i < 5; i++) w.brick.dispatch(out, "tick", {});
  await tick();
  release(); await tick();
  release(); await tick();
  assert.equal(sent.length, 2, "one in flight and one waiting, the rest collapsed");
}

// 14. ★ Depth: an event more than 4 hops from the user's action reaches
// brick.on but no listener, and "event-loop" fires.
{
  const page = [1, 2, 3, 4, 5].map((i) => stateful("l" + i, { on: { ["e" + i]: "$refresh" } })).join("") + `<div id="out"></div>`;
  const { w, doc, sent, events } = setup(page, (r) => {
    const i = Number(r.handle.slice(3));
    return envelope(stateful("l" + i, { on: { ["e" + i]: "$refresh" } }), [{ kind: "dispatch", name: "e" + (i + 1) }]);
  });
  w.brick.dispatch(doc.getElementById("out"), "e1");
  for (let i = 0; i < 6; i++) await tick();
  assert.deepEqual(sent.map((r) => r.handle), ["H-l1", "H-l2", "H-l3", "H-l4"], "stops after 4 hops");
  assert.deepEqual(plain(events.filter((e) => e[0] === "event-loop").map((e) => e[1].chain)), [["e1", "e2", "e3", "e4", "e5"]]);
}

// 15. ★ Cycle: ping -> pong -> ping stops at the second ping.
{
  const page = stateful("a", { on: { ping: "$refresh" } }) + stateful("b", { on: { pong: "$refresh" } }) + `<div id="out"></div>`;
  const { w, doc, sent, events } = setup(page, (r) => envelope(stateful(r.handle.slice(2)), [{ kind: "dispatch", name: r.handle === "H-a" ? "pong" : "ping" }]));
  w.brick.dispatch(doc.getElementById("out"), "ping");
  for (let i = 0; i < 5; i++) await tick();
  assert.deepEqual(sent.map((r) => r.handle), ["H-a", "H-b"], "cycle stopped");
  assert.deepEqual(plain(events.filter((e) => e[0] === "event-loop").map((e) => e[1].chain)), [["ping", "pong", "ping"]]);
}

// 16. ★ Fan-out: at most 50 listener actions per event.
{
  const page = Array.from({ length: 55 }, (_, i) => stateful("f" + i, { on: { all: "$refresh" } })).join("") + `<div id="out"></div>`;
  const { w, doc, sent, warnings } = setup(page, () => ({ hold: true }));
  w.brick.dispatch(doc.getElementById("out"), "all");
  await tick();
  assert.equal(sent.length, 50, "capped at 50");
  assert.ok(warnings.some((m) => m.includes("more than 50")), "warned");
}

// 17. $dispatch in brick:click sends nothing itself and works outside any
// stateful component; reserved and bad names are refused.
{
  const page = `<button id="sel" brick:click='$dispatch("contact-selected", {"id": 42})'>pick</button><button id="bad" brick:click='$dispatch("recovered")'>x</button>` + stateful("b", { on: { "contact-selected": "open" } });
  const { w, doc, sent, events } = setup(page, () => envelope(stateful("b")));
  const bus = [];
  w.brick.on("contact-selected", (d) => bus.push(d));
  click(w, doc.getElementById("sel"));
  await tick();
  assert.deepEqual(plain(bus), [{ id: 42, $source: null }], "emitted");
  assert.deepEqual(calls(sent), [["H-b", "open", [{ id: 42 }]]], "only the listener's own request");
  click(w, doc.getElementById("bad"));
  await tick();
  assert.ok(events.some((e) => e[0] === "action-error" && /reserved/.test(e[1].message)), "reserved name refused");
  assert.throws(() => w.brick.dispatch(doc.body, "Contact-Saved"), /bad event name/, "uppercase refused");
  assert.throws(() => w.brick.dispatch(doc.body, "brick:x"), /reserved/, "brick: prefix refused");
}

// 18. $parent.save runs save on the stateful root around the current one.
{
  const page = stateful("p", { inner: stateful("c", { inner: `<button id="close" brick:click="$parent.save(1)">close</button>` }) });
  const { w, doc, sent, events } = setup(page, () => envelope(null));
  click(w, doc.getElementById("close"));
  await tick();
  assert.deepEqual(calls(sent), [["H-p", "save", [1]]], "parent's handle and action");
  const lone = setup(stateful("c", { inner: `<button id="close" brick:click="$parent.save">close</button>` }), () => envelope(null));
  click(lone.w, lone.doc.getElementById("close"));
  await tick();
  assert.equal(lone.sent.length, 0, "no parent, no request");
  assert.ok(lone.events.some((e) => e[0] === "action-error"), "reported");
}

// 19. An error envelope (actionmw.FlashErrors) shows its flash and still
// fails the action.
{
  const { w, doc, events } = setup(stateful("a"), () => envelope(null, [{ kind: "flash", message: "Something went wrong", level: "danger" }], 422));
  const shown = [];
  w.brick.on("flash", (f) => shown.push(f.level));
  doc.querySelector('[data-brick="a"]').setAttribute("brick:click", "boom");
  click(w, doc.querySelector('[data-brick="a"]'));
  await tick();
  assert.deepEqual(shown, ["danger"], "flash applied");
  assert.ok(events.some((e) => e[0] === "action-error" && e[1].message === "Something went wrong"), "action-error with the message");
}
}

try {
  await main();
  console.log("effects ok");
} catch (err) {
  console.error(err);
  process.exitCode = 1;
} finally {
  windows.forEach((w) => w.close());
}
