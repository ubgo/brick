// End-to-end check of the demo site in a real, headless Chrome over the
// DevTools protocol: mounting, actions, bindings and navigation.
// Usage: node e2e_chrome.mjs http://127.0.0.1:18090   (task e2e runs it)
//        E2E_BASE=/engine node e2e_chrome.mjs http://127.0.0.1:18090
// With E2E_BASE the demo is mounted under that path (site BasePath): every
// path the suite opens or expects is prefixed, and a request to the server
// outside the base (other than / and the favicon), or any 4xx/5xx, fails.
import assert from "node:assert/strict";
import { openChrome, sleep } from "./cdp.mjs";

const origin = (process.argv[2] || "http://127.0.0.1:18090").replace(/\/$/, "");
// B is the mount path ("" unmounted, "/engine" mounted).
const B = (process.env.E2E_BASE || "").replace(/\/$/, "");
const base = origin + B;

// MODEL_FLUSH_MS outlasts brick:model's 250 ms debounce, so the typed value
// has reached the server before the form is submitted.
const MODEL_FLUSH_MS = 400;
// SETTLE_MS lets a just-changed field's $set round trip finish, or a page's
// client modules mount, before the next click.
const SETTLE_MS = 300;
// CLOCK_MS outlasts the clock's brick:poll interval (the runtime's floor,
// 1 s) plus a render.
const CLOCK_MS = 6000;

const cdp = await openChrome({ profilePrefix: "brick-e2e-" });
const { send, evaluate, until } = cdp;

try {
  const errors = [];
  cdp.on((method, p) => {
    if (method === "Runtime.exceptionThrown") errors.push(p.exceptionDetails.exception?.description);
    if (method === "Network.responseReceived" && p.response.status >= 400 && !p.response.url.endsWith("/favicon.ico")) errors.push(`HTTP ${p.response.status} ${p.response.url}`);
    if (B && method === "Network.requestWillBeSent" && p.request.url.startsWith(origin)) {
      const path = new URL(p.request.url).pathname;
      if (path !== "/" && path !== "/favicon.ico" && !path.startsWith(B + "/")) errors.push(`outside ${B}/: ${path}`);
    }
  });
  await send("Runtime.enable");
  await send("Page.enable");
  await send("Network.enable");
  await send("Page.navigate", { url: base + "/" });
  await until("document.readyState === 'complete' && !!window.brick && !!window.brick.navigate", "the runtime to load");

  // Actions: the like button increments on the server and patches in place.
  await until("document.querySelector('.likes button') !== null", "Likes");
  await evaluate("window.__marker = 42; document.querySelector('.likes button').click()");
  await until("document.querySelector('.likes button').textContent.includes('1')", "the like to count");

  // Bindings: typing updates brick:text at once; the server echoes after the debounce.
  await evaluate(`(() => { const i = document.querySelector('.signup input[brick\\\\:model="name"]'); i.value = 'Ada'; i.dispatchEvent(new Event('input', {bubbles: true})); })()`);
  assert.equal(await evaluate(`document.querySelector('.signup [brick\\\\:text="name"]').textContent`), "Ada", "brick:text follows typing");
  // Validation: submitting without agreeing shows the error from the server.
  await sleep(MODEL_FLUSH_MS);
  await evaluate("document.querySelector('.signup button').click()");
  await evaluate("window.__errs = []; brick.on('action-error', (d) => window.__errs.push(d))");
  try {
    await until("document.querySelector('.signup .error') !== null", "the validation error");
  } catch (e) {
    console.error("form:", await evaluate("document.querySelector('.signup').outerHTML"));
    console.error("action errors:", JSON.stringify(await evaluate("window.__errs")));
    throw e;
  }
  assert.match(await evaluate("document.querySelector('.signup .error').textContent"), /accept the terms/);
  await evaluate(`(() => { const c = document.querySelector('.signup input[type=checkbox]'); c.checked = true; c.dispatchEvent(new Event('change', {bubbles: true})); })()`);
  await until(`!document.querySelector('.signup [brick\\\\:show="agree"]').hidden`, "brick:show");
  await sleep(SETTLE_MS);
  await evaluate("document.querySelector('.signup button').click()");
  await until("document.querySelector('.signup .done') !== null", "the signup to finish");
  assert.equal(await evaluate("document.querySelector('.signup .done').textContent"), "Welcome, Ada!");

  // Mount runtime: the client component was mounted (its custom element ran).
  assert.ok(await evaluate("!!customElements.get('x-counter')"), "component module loaded");

  // Navigation: clicking an internal link changes page without a reload,
  // and the brick:persist element is the same element afterwards.
  await evaluate("document.getElementById('player').__keep = 7");
  await evaluate(`document.querySelector('a[href="${B}/blog/hello"], a[href="${B}/blog/hello/"]').click()`);
  await until(`location.pathname.startsWith(${JSON.stringify(B + "/blog/hello")})`, "the navigation");
  await until("document.body.textContent.includes('Posted')", "the new page");
  assert.equal(await evaluate("window.__marker"), 42, "no full reload: window state survived");
  assert.match(await evaluate("document.title"), /Hello/);
  assert.equal(await evaluate("document.getElementById('player').__keep"), 7, "brick:persist element kept");

  const goto = async (path, ready) => {
    await send("Page.navigate", { url: base + path });
    await until(`document.readyState === 'complete' && location.pathname + location.search === ${JSON.stringify(B + path)}`, path);
    if (ready) await until(ready, path);
  };

  // Islands: the cached page, filled for this request's user.
  await goto("/features/islands/?as=Ada", "!!document.querySelector('.account')");
  assert.equal(await evaluate("document.querySelector('.account').textContent"), "Hi, Ada");

  // Polling: the clock's time changes without a reload (brick:poll, $refresh,
  // a loader calling the host's now() capability).
  await goto("/features/live/", "!!document.querySelector('.clock time') && !!window.brick");
  const t0 = await evaluate("document.querySelector('.clock time').textContent");
  await until(`document.querySelector('.clock time').textContent !== ${JSON.stringify(t0)}`, "the clock to refresh", CLOCK_MS);

  // The kit, imported remotely: one tab panel at a time, keyboard included.
  await goto("/features/kit/", "document.querySelectorAll('[role=tabpanel]:not([hidden])').length === 1");
  await evaluate("document.querySelectorAll('[role=tab]')[1].click()");
  assert.equal(await evaluate("[...document.querySelectorAll('[role=tabpanel]')].map(p => !p.hidden).join()"), "false,true");
  assert.equal(await evaluate("document.querySelector('.bk-next').hidden"), false, "carousel buttons shown");

  // Client modules: Opener's module imports Modal's and opens the dialog.
  await goto("/features/client/", "!!document.querySelector('.opener')");
  await sleep(SETTLE_MS);
  await evaluate("document.querySelector('.opener').click()");
  await until("document.getElementById('hello-modal').hasAttribute('open')", "the dialog to open");

  // Images: the resized copy loads.
  await goto("/features/images/", "document.querySelector('img.team') && document.querySelector('img.team').complete");
  assert.ok(await evaluate("document.querySelector('img.team').naturalWidth") > 0, "image decoded");

  assert.deepEqual(errors, [], "no uncaught exceptions");
  console.log("e2e ok");
} catch (err) {
  console.error(err);
  process.exitCode = 1;
} finally {
  cdp.close();
}
