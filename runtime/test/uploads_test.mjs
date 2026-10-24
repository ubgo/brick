// Tests runtime.js's uploads in jsdom:
// brick:upload, progress on <progress brick:upload-progress>,
// data-uploading, $cancelUpload, queue ordering with an action, a 410 as
// upload-error "gone", the advisory checks and the presigned flow. XHR is
// replaced by a fake the test drives; fetch answers actions.
import { readFileSync } from "node:fs";
import assert from "node:assert/strict";
import { JSDOM, VirtualConsole } from "jsdom";
import { tick, click } from "./dom.mjs";

const runtimeJS = readFileSync(new URL("../runtime.js", import.meta.url), "utf8");
const ENVELOPE = "application/vnd.brick.action+json";
const windows = [];

// page is a stateful root with one-file and list upload fields. lim
// overrides data-brick-uploads.
const page = (h = "H", body = "", lim = null) => `<div id="r" data-brick="u1" data-brick-state="${h}" data-brick-token="T" data-brick-uploads='${JSON.stringify(lim || { avatar: { accept: ["image/png"], max_size: 100, max_files: 1 }, photos: { accept: ["image/*"], max_size: 100, max_files: 2, many: true } })}'>
  <input id="in" type="file" brick:upload="avatar">
  <progress id="bar" brick:upload-progress="avatar"></progress>
  <button id="cancel" brick:click='$cancelUpload("avatar")'>cancel</button>
  <button id="save" brick:click="save">save</button>
  <i id="out">${body}</i>
</div>`;

const envelope = (html, extra = {}) => JSON.stringify(Object.assign({ v: 1, version: 1, html, effects: [] }, extra));

// setup loads the page; xhrs collects every fake XHR, fetches every
// action body. respond answers fetch; a held answer waits for release().
function setup(html, respond = () => ({})) {
  const dom = new JSDOM(`<!doctype html><body>${html}</body>`, { url: "https://site.test/f/", runScripts: "outside-only", virtualConsole: new VirtualConsole() });
  const w = dom.window;
  windows.push(w);
  const xhrs = [];
  w.XMLHttpRequest = class {
    constructor() { this.headers = {}; this.upload = {}; xhrs.push(this); }
    open(method, url) { this.method = method; this.url = url; }
    setRequestHeader(k, v) { this.headers[k] = v; }
    getResponseHeader(k) { return k === "Content-Type" ? this.type || "" : null; }
    send(body) { this.body = body; this.sent = true; }
    abort() { this.aborted = true; if (this.onabort) this.onabort(); }
    progress(loaded, total) { this.upload.onprogress({ lengthComputable: true, loaded, total }); }
    answer(status, text, type = ENVELOPE) { this.status = status; this.statusText = "x"; this.responseText = text; this.type = type; this.onload(); }
  };
  const fetches = [];
  const held = [];
  w.fetch = (url, init = {}) => {
    const body = init.body ? JSON.parse(init.body) : null;
    fetches.push(body);
    const r = respond(body) || {};
    const answer = { status: r.status || 200, ok: !r.status || r.status < 400, statusText: "x", headers: { get: () => ENVELOPE }, text: async () => envelope(r.html || page("H2")) };
    if (r.hold) return new Promise((res) => held.push(() => res(answer)));
    return Promise.resolve(answer);
  };
  w.eval(runtimeJS);
  const events = [];
  for (const n of ["upload-progress", "upload-done", "upload-error", "action-error"]) w.brick.on(n, (d) => events.push([n, d]));
  const file = (name, type, size) => new w.File(["x".repeat(size)], name, { type });
  return { w, $: (id) => w.document.getElementById(id), xhrs, fetches, events, file, release: () => held.shift()() };
}

const tests = [];
const test = (name, fn) => tests.push([name, fn]);

test("upload: one XHR PUT per file with the brick headers; progress fills <progress>; the answer morphs in", async () => {
  const { w, $, xhrs, events, file } = setup(page());
  const done = w.brick.upload($("in"), "avatar", [file("me.png", "image/png", 10)]);
  await tick();
  assert.equal(xhrs.length, 1);
  const x = xhrs[0];
  assert.equal(x.method, "PUT");
  assert.equal(x.url, "/_brick/upload");
  assert.equal(x.headers["Brick-Upload"], "avatar");
  assert.equal(x.headers["Brick-Handle"], "H");
  assert.equal(x.headers["Brick-Token"], "T");
  assert.equal(x.headers["Brick-Name"], "me.png");
  assert.equal(x.headers["Content-Type"], "image/png");
  assert.ok($("in").hasAttribute("data-uploading"), "the input is marked while sending");
  x.progress(5, 10);
  assert.equal($("bar").value, 5);
  assert.equal($("bar").max, 10);
  x.answer(200, envelope(page("H2", "me.png")));
  await done;
  await tick();
  assert.equal($("out").textContent, "me.png");
  assert.equal($("r").getAttribute("data-brick-state"), "H2");
  assert.ok(!$("in").hasAttribute("data-uploading"));
  assert.deepEqual(events.map((e) => e[0]), ["upload-progress", "upload-progress", "upload-progress", "upload-done"]);
  assert.deepEqual(JSON.parse(JSON.stringify(events[1][1])), { loaded: 5, total: 10, field: "avatar", name: "me.png", size: 10 });
});

test("upload: a change on <input type=file brick:upload> sends its files", async () => {
  const { w, $, xhrs, file } = setup(page());
  Object.defineProperty($("in"), "files", { value: [file("a.png", "image/png", 3)], configurable: true });
  $("in").dispatchEvent(new w.Event("change", { bubbles: true }));
  await tick();
  assert.equal(xhrs.length, 1);
  assert.equal(xhrs[0].headers["Brick-Name"], "a.png");
});

test("upload: $cancelUpload aborts the request in flight and emits upload-error cancelled", async () => {
  const { w, $, xhrs, events, file } = setup(page());
  w.brick.upload($("in"), "avatar", [file("me.png", "image/png", 10)]);
  await tick();
  click(w, $("cancel"));
  await tick();
  assert.ok(xhrs[0].aborted);
  const err = events.find((e) => e[0] === "upload-error");
  assert.equal(err[1].cancelled, true);
  assert.ok(!$("in").hasAttribute("data-uploading"));
});

test("upload: queued behind an action in flight, sent after it lands", async () => {
  const { w, $, xhrs, fetches, file, release } = setup(page(), () => ({ hold: true }));
  click(w, $("save"));
  await tick();
  w.brick.upload($("in"), "avatar", [file("me.png", "image/png", 10)]);
  await tick();
  assert.equal(xhrs.length, 0, "the upload waits for the action");
  release();
  await tick();
  assert.deepEqual(fetches.map((b) => b.action), ["save"]);
  assert.equal(xhrs.length, 1);
  assert.equal(xhrs[0].headers["Brick-Handle"], "H2", "the upload carries the handle the action returned");
});

test("upload: a 410 is upload-error gone, with no recovery or replay", async () => {
  const { w, $, xhrs, fetches, events, file } = setup(page());
  w.brick.upload($("in"), "avatar", [file("me.png", "image/png", 10)]);
  await tick();
  xhrs[0].answer(410, "gone", "text/plain");
  await tick();
  const err = events.find((e) => e[0] === "upload-error");
  assert.equal(err[1].gone, true);
  assert.equal(fetches.length, 0, "no recovery fetch");
});

test("upload: the advisory checks refuse before sending (size, type, count)", async () => {
  const { w, $, xhrs, events, file } = setup(page());
  await w.brick.upload($("in"), "avatar", [file("big.png", "image/png", 500)]);
  await w.brick.upload($("in"), "avatar", [file("a.gif", "image/gif", 5)]);
  w.brick.upload($("in"), "photos", [file("1.png", "image/png", 5), file("2.png", "image/png", 5), file("3.png", "image/png", 5)]);
  await tick();
  const refused = events.filter((e) => e[0] === "upload-error" && e[1].refused).map((e) => e[1].name);
  assert.deepEqual(refused, ["big.png", "a.gif", "3.png"]);
  assert.equal(xhrs.length, 1, "only the first photo is in flight; the second waits in the queue");
});

test("upload: a server error answers upload-error with its message", async () => {
  const { w, $, xhrs, events, file } = setup(page());
  w.brick.upload($("in"), "avatar", [file("me.png", "image/png", 10)]);
  await tick();
  xhrs[0].answer(415, "brick: this type of file is not accepted", "text/plain");
  await tick();
  const err = events.find((e) => e[0] === "upload-error");
  assert.match(err[1].message, /not accepted/);
});

test("upload: presigned — POST for a target, PUT to storage with progress, then $uploaded", async () => {
  const lim = { avatar: { accept: ["image/png"], max_size: 100, max_files: 1, presign: true } };
  const { w, $, xhrs, fetches, events, file } = setup(page("H", "", lim));
  const done = w.brick.upload($("in"), "avatar", [file("me.png", "image/png", 10)]);
  await tick();
  assert.equal(xhrs[0].method, "POST");
  assert.equal(xhrs[0].headers["Brick-Size"], "10");
  assert.equal(xhrs[0].body, null, "the presign request carries no bytes");
  xhrs[0].answer(200, envelope(page("H2", "", lim), { upload: { url: "https://storage.test/p1", method: "PUT", headers: { "x-amz-acl": "private" }, id: "p1" } }));
  await tick();
  assert.equal(xhrs.length, 2);
  assert.equal(xhrs[1].url, "https://storage.test/p1");
  assert.equal(xhrs[1].headers["x-amz-acl"], "private");
  assert.equal(xhrs[1].headers["Brick-Token"], undefined, "storage never sees the brick token");
  xhrs[1].progress(10, 10);
  xhrs[1].answer(200, "", "text/plain");
  await done;
  await tick();
  assert.deepEqual(fetches.map((b) => [b.action, b.args, b.handle]), [["$uploaded", ["avatar", "p1"], "H2"]]);
  assert.ok(events.some((e) => e[0] === "upload-done"));
});

let failed = 0;
for (const [name, fn] of tests) {
  try {
    await fn();
  } catch (err) {
    failed++;
    console.error("FAIL " + name + "\n", err);
  }
}
windows.forEach((w) => w.close());
if (failed) {
  process.exitCode = 1;
} else {
  console.log("uploads ok");
}
