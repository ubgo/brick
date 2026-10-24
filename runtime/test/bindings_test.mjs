// Tests the bindings half of runtime.js in jsdom.
import { readFileSync } from "node:fs";
import assert from "node:assert/strict";
import { JSDOM, VirtualConsole } from "jsdom";
import { fill as type } from "./dom.mjs";

const runtimeJS = readFileSync(new URL("../runtime.js", import.meta.url), "utf8");

const page = `<!doctype html><body>
<form class="c" data-brick="c1" data-brick-state="H" data-brick-token="T" brick:submit="save">
  <input id="name" brick:model="name" value="Al">
  <input id="qty" type="number" brick:model.debounce.50ms="qty" value="1">
  <input id="agree" type="checkbox" brick:model.lazy="agree">
  <input id="city" brick:model.lazy="city" value="">
  <p>Hello, <span id="hello" brick:text="name">Al</span></p>
  <p id="thanks" brick:show="agree" hidden>Thanks!</p>
  <p id="warn" brick:show="!agree">Please agree</p>
  <span id="spin" brick:loading hidden>Saving…</span>
  <button id="go">Save</button>
</form>
<div class="p" data-brick="p1" data-brick-state="P" data-brick-token="T" brick:poll="1s"><b id="tick">0</b></div>
</body>`;

// Every window is closed at the end, or a poll timer keeps node running.
const windows = [];

function setup(respond = () => ({ body: "" }), opts = {}) {
  const dom = new JSDOM(page, { url: "https://site.test/", runScripts: "outside-only", virtualConsole: new VirtualConsole(), ...opts });
  const w = dom.window;
  windows.push(w);
  const sent = [];
  let resolveFetch;
  w.fetch = (url, init) => {
    const body = JSON.parse(init.body);
    sent.push(body);
    const r = respond(body);
    const answer = { status: r.status || 200, ok: !r.status || r.status < 400, statusText: "x", text: async () => r.body || "" };
    if (r.hold) return new Promise((res) => { resolveFetch = () => res(answer); });
    return Promise.resolve(answer);
  };
  w.eval(runtimeJS);
  return { w, doc: w.document, sent, release: () => resolveFetch && resolveFetch() };
}
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

// Timing: the debounce is 250 ms (runtime.js DEBOUNCE_MS), the qty field's
// own is 50 ms, and polling is at least one second (POLL_MIN_MS); waits
// leave wide margins because these tests also run under -race on a busy
// machine.
const SETTLE_MS = 10; // a resolved fetch and the patch it triggers
const PAST_DEBOUNCE_MS = 600; // well past the 250 ms default debounce
const PAST_CUSTOM_DEBOUNCE_MS = 400; // well past qty's 50 ms debounce
const WITHIN_DEBOUNCE_WINDOW_MS = 300; // past the default debounce: a lazy field would have sent by now if it were not lazy
const JUST_PAST_ONE_POLL_MS = 1150; // one poll interval and a little
const ONE_AND_A_HALF_POLLS_MS = 1600; // room for exactly one refresh
async function main() {
// Typing updates brick:text at once and sends $set after the debounce, once.
{
  const { w, doc, sent } = setup();
  type(w, doc.getElementById("name"), "Ali");
  assert.equal(doc.getElementById("hello").textContent, "Ali", "local signal, no request");
  type(w, doc.getElementById("name"), "Alia");
  assert.equal(sent.length, 0, "debounced");
  await sleep(PAST_DEBOUNCE_MS);
  assert.equal(JSON.stringify(sent.map((s) => [s.action, s.args])), JSON.stringify([["$set", ["name", "Alia"]]]), "one request with the last value");
  type(w, doc.getElementById("qty"), "5");
  await sleep(PAST_CUSTOM_DEBOUNCE_MS);
  assert.equal(JSON.stringify(sent.at(-1).args), JSON.stringify(["qty", 5]), "number input sends a number; custom debounce");
}

// A lazy checkbox drives brick:show both ways and sends on change.
{
  const { w, doc, sent } = setup();
  assert.equal(doc.getElementById("thanks").hidden, true);
  assert.equal(doc.getElementById("warn").hidden, false);
  const box = doc.getElementById("agree");
  box.checked = true;
  box.dispatchEvent(new w.Event("input", { bubbles: true }));
  box.dispatchEvent(new w.Event("change", { bubbles: true }));
  assert.equal(doc.getElementById("thanks").hidden, false, "shown");
  assert.equal(doc.getElementById("warn").hidden, true, "inverted hidden");
  await sleep(SETTLE_MS);
  assert.equal(JSON.stringify(sent.map((s) => s.args)), JSON.stringify([["agree", true]]), "one immediate send");
  const city = doc.getElementById("city");
  type(w, city, "Lyon");
  await sleep(WITHIN_DEBOUNCE_WINDOW_MS);
  assert.equal(sent.length, 1, "lazy text input waits for change");
  city.dispatchEvent(new w.Event("change", { bubbles: true }));
  await sleep(SETTLE_MS);
  assert.equal(JSON.stringify(sent.at(-1).args), JSON.stringify(["city", "Lyon"]));
}

// Loading state: brick:loading shown and the button disabled during a submit.
{
  const { w, doc, sent, release } = setup(() => ({ hold: true, body: "" }));
  doc.querySelector("form").dispatchEvent(new w.SubmitEvent("submit", { bubbles: true, cancelable: true }));
  assert.equal(doc.getElementById("spin").hidden, false, "loading shown");
  assert.equal(doc.getElementById("go").disabled, true, "button disabled");
  assert.equal(sent[0].action, "save");
  release();
  await sleep(SETTLE_MS);
  assert.equal(doc.getElementById("spin").hidden, true, "loading hidden again");
  assert.equal(doc.getElementById("go").disabled, false, "button enabled again");
}

// The server's re-render re-reads the signals (validation errors shown).
{
  const { w, doc } = setup(() => ({ body: `<form class="c" data-brick="c1" data-brick-state="H" data-brick-token="T" brick:submit="save"><input id="name" brick:model="name" value="Zed"><p class="error">Too short</p><p>Hello, <span id="hello" brick:text="name">Zed</span></p><span id="spin" brick:loading hidden>Saving…</span><button id="go">Save</button></form>` }));
  doc.querySelector("form").dispatchEvent(new w.SubmitEvent("submit", { bubbles: true, cancelable: true }));
  await sleep(SETTLE_MS);
  assert.equal(doc.querySelector(".error").textContent, "Too short");
  type(w, doc.getElementById("name"), "Zoe");
  assert.equal(doc.getElementById("hello").textContent, "Zoe", "bindings live after the patch");
}

// brick:poll refreshes with $refresh, at most once a second.
{
  // jsdom reports a page as hidden unless it pretends to be visual, and
  // polling pauses while hidden — which the first half checks.
  const hidden = setup();
  await sleep(JUST_PAST_ONE_POLL_MS);
  assert.equal(hidden.sent.filter((s) => s.action === "$refresh").length, 0, "no polling while hidden");
  const { sent } = setup((b) => ({ body: b.action === "$refresh" ? `<div class="p" data-brick="p1" data-brick-state="P" data-brick-token="T" brick:poll="1s"><b id="tick">1</b></div>` : "" }), { pretendToBeVisual: true });
  await sleep(ONE_AND_A_HALF_POLLS_MS);
  const refreshes = sent.filter((s) => s.action === "$refresh");
  assert.equal(refreshes.length, 1, "one refresh in the first second and a half");
}

}

try {
  await main();
  console.log("bindings ok");
} catch (err) {
  console.error(err);
  process.exitCode = 1;
} finally {
  windows.forEach((w) => w.close());
}
