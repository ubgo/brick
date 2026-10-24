// Cross-site checks for the showcase and the engine demo mounted on one
// server (go run ./ui -serve ADDR -engine site: /ui/, /engine/, an index at
// /), in a real headless Chrome over the DevTools protocol. The two suites
// check each site under its base (E2E_BASE); this checks what lies between
// them:
//
//   - the index at / links both sites, and /ui and /engine redirect (301)
//     to /ui/ and /engine/;
//   - a click from a /ui page to an /engine/ link (and back) is a full page
//     load, not in-place navigation: the other site's own runtime and assets
//     load, and window state does not survive;
//   - no request leaves the base of the page that made it, and none fails.
//
// Usage: node e2e_both_chrome.mjs http://127.0.0.1:18400   (scripts/e2e_both.sh)
// Prints one line per check and "e2e both ok"; exits non-zero on a failure.
import assert from "node:assert/strict";
import { openChrome, input } from "./cdp.mjs";

const origin = (process.argv[2] || "http://127.0.0.1:18400").replace(/\/$/, "");
const js = (s) => JSON.stringify(s);
// LOAD_MS is the wait for a full page load: a cold start of a site's
// runtime and assets, longer than until()'s default.
const LOAD_MS = 8000;
// The browser opens after the plain-HTTP checks; these are its handles.
let cdp, send, evaluate, until, click;

// problems collects failed requests and requests outside the base of the
// page that made them; page is that base ("/ui" or "/engine"), "" for /.
let problems = [];
let page = "";
async function visit(path, base) {
  page = base;
  problems = [];
  await send("Page.navigate", { url: origin + path });
  await until(`document.readyState === 'complete' && location.pathname === ${js(path)}`, path, LOAD_MS);
}

let failed = 0;
async function check(name, fn) {
  try { await fn(); console.log(`ok   ${name}`); } catch (e) { failed++; console.log(`FAIL ${name}: ${String(e.message || e).split("\n")[0]}`); }
}
const clean = (what) => { if (problems.length) throw new Error(`${what}: ${problems.slice(0, 4).join(" | ")}`); };

// crossTo clicks a link from the current site to another site's page and
// checks it was a full load of that site.
async function crossTo(href, base) {
  await evaluate(`window.__stay = 1; const a = document.createElement('a'); a.id = 'e2e-cross'; a.href = ${js(href)}; a.textContent = 'cross'; a.style.cssText = 'position:fixed;top:8px;left:8px;z-index:99999;padding:4px;background:white'; document.body.append(a)`);
  page = base; // requests from here on belong to the destination
  await click("#e2e-cross");
  await until(`location.pathname === ${js(href)} && document.readyState === 'complete'`, "the other site's page", LOAD_MS);
  assert.equal(await evaluate("window.__stay"), undefined, "a full page load (window state gone), not in-place navigation");
  const runtime = await evaluate(`[...document.querySelectorAll('script[src]')].map((s) => new URL(s.src).pathname).find((p) => /\\/_brick\\/c\\/runtime\\./.test(p)) || ''`);
  assert.ok(runtime.startsWith(base + "/_brick/c/"), `the other site's runtime loaded (${runtime || "none"})`);
  await until("!!window.brick", "the runtime to run");
  assert.equal(await evaluate(`document.querySelector('meta[name=brick-base]')?.content`), base, "the page names its base");
}

try {
  // Plain HTTP first: the index and the base redirects.
  await check("/ links both sites", async () => {
    const html = await (await fetch(origin + "/")).text();
    assert.match(html, /href="\/ui\/"/);
    assert.match(html, /href="\/engine\/"/);
  });
  for (const b of ["/ui", "/engine"]) {
    await check(`${b} redirects (301) to ${b}/`, async () => {
      const r = await fetch(origin + b, { redirect: "manual" });
      assert.equal(r.status, 301);
      assert.equal(new URL(r.headers.get("location"), origin).pathname, b + "/");
    });
  }

  // The back/forward cache is off, so every navigation is a fresh document.
  cdp = await openChrome({ profilePrefix: "brick-e2e-both-", args: ["--disable-features=BackForwardCache"] });
  ({ send, evaluate, until } = cdp);
  ({ click } = input(cdp));
  cdp.on((method, p) => {
    if (method === "Network.requestWillBeSent" && p.request.url.startsWith(origin)) {
      const path = new URL(p.request.url).pathname;
      if (page && path !== "/" && path !== "/favicon.ico" && !path.startsWith(page + "/")) problems.push(`outside ${page}/: ${path}`);
    }
    if (method === "Network.responseReceived" && p.response.status >= 400 && !p.response.url.endsWith("/favicon.ico")) problems.push(`HTTP ${p.response.status} ${p.response.url.replace(origin, "")}`);
    if (method === "Runtime.exceptionThrown") problems.push("exception: " + (p.exceptionDetails.exception?.description || p.exceptionDetails.text).split("\n")[0]);
  });
  await send("Runtime.enable");
  await send("Page.enable");
  await send("Network.enable");
  // Wide enough that the showcase's sidebar is on screen, not AppShell's drawer.
  await send("Emulation.setDeviceMetricsOverride", { width: 1280, height: 900, deviceScaleFactor: 1, mobile: false });

  await check("the index's links open both sites", async () => {
    await visit("/", "");
    for (const b of ["/ui", "/engine"]) {
      await visit("/", "");
      page = b;
      await click(`a[href='${b}/']`);
      await until(`location.pathname === ${js(b + "/")} && document.readyState === 'complete' && !!window.brick`, b + "/", LOAD_MS);
      clean(b);
    }
  });

  await check("a click from /ui to /engine/ is a full load of the engine demo", async () => {
    await visit("/ui/components/button/", "/ui");
    await until("!!window.brick", "the showcase runtime");
    await crossTo("/engine/", "/engine");
    clean("/ui → /engine");
  });

  await check("a click from /engine to /ui/ is a full load of the showcase", async () => {
    await visit("/engine/", "/engine");
    await until("!!window.brick", "the engine demo runtime");
    await crossTo("/ui/components/badge/", "/ui");
    clean("/engine → /ui");
  });

  await check("in-place navigation stays inside the base", async () => {
    await visit("/ui/components/button/", "/ui");
    await until("!!window.brick && !!window.brick.navigate", "navigation");
    await evaluate("window.__stay = 1");
    await click(".docs__side a[href='/ui/components/badge/']");
    await until("location.pathname === '/ui/components/badge/'", "the badge page");
    assert.equal(await evaluate("window.__stay"), 1, "inside the base, navigation is in place");
    clean("/ui in place");
  });
} catch (e) {
  failed++;
  console.log("FAIL harness: " + (e.stack || e));
} finally {
  if (cdp) cdp.close();
}
if (failed) { console.log(`e2e both: ${failed} failed`); process.exit(1); }
console.log("e2e both ok");
process.exit(0);
