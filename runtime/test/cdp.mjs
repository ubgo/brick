// A headless Chrome driven over the DevTools protocol, shared by the
// real-browser suites (e2e_chrome.mjs, e2e_ui_chrome.mjs,
// e2e_both_chrome.mjs) and the accessibility audit (scripts/a11y_ui.mjs), so
// each keeps only its checks. Node only: no npm dependency, the WebSocket and
// fetch are Node's own.
import { spawn } from "node:child_process";
import { existsSync, mkdtempSync, readFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

// CHROME is the browser binary; CHROME=/path/to/chrome overrides the macOS
// default.
export const CHROME = process.env.CHROME || "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome";

// START_WAIT_MS is how long Chrome may take to open its DevTools endpoint,
// polled every POLL_MS; a cold start on a busy machine takes a few seconds.
const START_WAIT_MS = 10000;
// POLL_MS is the polling interval for startup and for until().
export const POLL_MS = 50;
// UNTIL_MS is until()'s default patience: long enough for a server action's
// round trip and a re-render on a loaded machine, short enough that a real
// failure reports quickly.
export const UNTIL_MS = 5000;

export const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

// openChrome starts a headless Chrome with a fresh profile and connects to
// its first page. Chrome picks its own debugging port (port 0) and writes it
// to the profile's DevToolsActivePort file, so suites running at the same
// time never collide on a port.
//
// It returns:
//   send(method, params)  a DevTools command; rejects with the protocol error
//   on(fn)                fn(method, params) for every protocol event
//   once(method)          a promise of the next event with that method
//   evaluate(expr)        the page value of expr (promises awaited); throws the page's exception
//   until(expr, what, ms) polls expr until truthy, or throws "timed out waiting for <what>"
//   close()               closes the connection and the browser
//
// args are extra Chrome switches (e.g. "--disable-features=BackForwardCache").
export async function openChrome({ args = [], profilePrefix = "brick-cdp-" } = {}) {
  const profile = mkdtempSync(join(tmpdir(), profilePrefix));
  const chrome = spawn(CHROME, ["--headless=new", "--hide-scrollbars", "--remote-debugging-port=0", `--user-data-dir=${profile}`, "--no-first-run", ...args, "about:blank"], { stdio: "ignore" });
  const portFile = join(profile, "DevToolsActivePort");
  let target;
  for (let waited = 0; waited < START_WAIT_MS && !target; waited += POLL_MS) {
    await sleep(POLL_MS);
    if (!existsSync(portFile)) continue;
    const port = readFileSync(portFile, "utf8").split("\n")[0].trim();
    try { target = (await (await fetch(`http://127.0.0.1:${port}/json`)).json()).find((t) => t.type === "page"); } catch {}
  }
  if (!target) { chrome.kill(); throw new Error("Chrome did not start (set CHROME=/path/to/chrome)"); }

  const ws = new WebSocket(target.webSocketDebuggerUrl);
  await new Promise((r, j) => { ws.addEventListener("open", r); ws.addEventListener("error", j); });
  let seq = 0;
  const pending = new Map();
  const listeners = [];
  const waiters = [];
  ws.addEventListener("message", (e) => {
    const m = JSON.parse(e.data);
    if (m.id) {
      const p = pending.get(m.id);
      if (p) { pending.delete(m.id); m.error ? p.rej(new Error(m.error.message)) : p.res(m.result); }
      return;
    }
    for (let i = waiters.length - 1; i >= 0; i--) if (waiters[i].method === m.method) { waiters[i].r(m.params); waiters.splice(i, 1); }
    for (const fn of listeners) fn(m.method, m.params);
  });
  const send = (method, params = {}) => new Promise((res, rej) => {
    const id = ++seq;
    pending.set(id, { res, rej });
    ws.send(JSON.stringify({ id, method, params }));
  });
  const evaluate = async (expr) => {
    const r = await send("Runtime.evaluate", { expression: expr, awaitPromise: true, returnByValue: true });
    if (r.exceptionDetails) throw new Error(r.exceptionDetails.exception?.description || r.exceptionDetails.text);
    return r.result.value;
  };
  const until = async (expr, what, ms = UNTIL_MS) => {
    const end = Date.now() + ms;
    let last;
    while (Date.now() < end) {
      try { if (await evaluate(expr)) return; } catch (e) { last = e; }
      await sleep(POLL_MS);
    }
    throw new Error("timed out waiting for " + what + (last ? ` (${last.message.split("\n")[0]})` : ""));
  };
  return {
    send,
    evaluate,
    until,
    on: (fn) => { listeners.push(fn); },
    once: (method) => new Promise((r) => waiters.push({ method, r })),
    close: () => { try { ws.close(); } catch {} chrome.kill(); },
  };
}

// KEYS maps a key name to what Input.dispatchKeyEvent needs: its code, its
// Windows virtual key code (Chrome requires it for non-text keys) and, for
// keys that type, the text.
const KEYS = {
  Enter: { code: "Enter", keyCode: 13, text: "\r" }, Escape: { code: "Escape", keyCode: 27 }, Tab: { code: "Tab", keyCode: 9 },
  ArrowDown: { code: "ArrowDown", keyCode: 40 }, ArrowUp: { code: "ArrowUp", keyCode: 38 }, ArrowLeft: { code: "ArrowLeft", keyCode: 37 }, ArrowRight: { code: "ArrowRight", keyCode: 39 },
  Home: { code: "Home", keyCode: 36 }, End: { code: "End", keyCode: 35 }, k: { code: "KeyK", keyCode: 75 },
  " ": { code: "Space", keyCode: 32, text: " " },
};

// MODIFIERS are Input.dispatchKeyEvent's modifier bits.
export const MODIFIERS = { alt: 1, ctrl: 2, meta: 4, shift: 8 };

// INPUT_SETTLE_MS is a pause after each synthetic input, so the page's
// handlers (and any microtask they queue) run before the next step reads it.
const INPUT_SETTLE_MS = 60;

// input returns person-like input helpers over a connection: click presses
// the mouse at the element's centre (failing when something else covers
// that point, as it would a person's click), key presses a named key (with
// MODIFIERS), and type focuses a field, selects its text and types over it.
export function input(cdp) {
  const js = JSON.stringify;
  const box = async (sel) => {
    await cdp.until(`(() => { const e = document.querySelector(${js(sel)}); return e && e.getClientRects().length > 0; })()`, "visible " + sel);
    const at = await cdp.evaluate(`(() => { const e = document.querySelector(${js(sel)}); e.scrollIntoView({block: "center", inline: "center"}); const r = e.getBoundingClientRect(); const x = r.left + r.width / 2, y = r.top + r.height / 2; const hit = document.elementFromPoint(x, y); return {x, y, ok: !!hit && (e === hit || e.contains(hit)), hit: hit ? hit.outerHTML.slice(0, 120) : "nothing"}; })()`);
    if (!at.ok) throw new Error(`${sel} is covered at its centre by ${at.hit}`);
    return at;
  };
  const click = async (sel) => {
    const { x, y } = await box(sel);
    await cdp.send("Input.dispatchMouseEvent", { type: "mouseMoved", x, y });
    await cdp.send("Input.dispatchMouseEvent", { type: "mousePressed", x, y, button: "left", clickCount: 1 });
    await cdp.send("Input.dispatchMouseEvent", { type: "mouseReleased", x, y, button: "left", clickCount: 1 });
    await sleep(INPUT_SETTLE_MS);
  };
  const key = async (name, modifiers = 0) => {
    const k = KEYS[name];
    if (!k) throw new Error(`key ${name}: add it to KEYS in cdp.mjs`);
    const types = Boolean(k.text) && !modifiers;
    const common = { key: name, code: k.code, windowsVirtualKeyCode: k.keyCode, nativeVirtualKeyCode: k.keyCode, modifiers };
    await cdp.send("Input.dispatchKeyEvent", { type: types ? "keyDown" : "rawKeyDown", ...common, ...(types ? { text: k.text } : {}) });
    await cdp.send("Input.dispatchKeyEvent", { type: "keyUp", ...common });
    await sleep(INPUT_SETTLE_MS);
  };
  const type = async (sel, text) => {
    await click(sel);
    await cdp.evaluate(`(() => { const e = document.querySelector(${js(sel)}); e.focus(); if (e.select) e.select(); })()`);
    await cdp.send("Input.insertText", { text });
    await sleep(INPUT_SETTLE_MS);
  };
  return { click, key, type };
}
