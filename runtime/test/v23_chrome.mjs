// Checks brick:method links (v23) in a real, headless Chrome against
// examples/methodlinks: a click sends PATCH, DELETE and POST to a plain
// net/http router through brick.MethodOverride, brick:confirm asks first
// (a cancel sends nothing), a component call carries the attribute to its
// <a>, and a plain link still sends GET. Each base URL is one server; the
// script runs it with navigation off and on.
// Usage: go run ./methodlinks -addr 127.0.0.1:8083 [-nav] (in examples),
//        then node v23_chrome.mjs http://127.0.0.1:8083 [more bases…]
import assert from "node:assert/strict";
import { openChrome, input, sleep } from "./cdp.mjs";

const bases = process.argv.slice(2).map((b) => b.replace(/\/$/, ""));
if (!bases.length) bases.push("http://127.0.0.1:8083");
// SETTLE_MS outlasts a form post's round trip, so a page that must not
// have changed is read after anything that could change it has landed.
const SETTLE_MS = 500;

const cdp = await openChrome({ profilePrefix: "brick-v23-" });
const { send, evaluate, until } = cdp;
const person = input(cdp);
const errors = [];
let answer = true; // what the next confirm dialog is answered with
let dialogs = 0;
cdp.on((m, p) => {
  if (m === "Runtime.exceptionThrown") errors.push(p.exceptionDetails.exception?.description);
  if (m === "Page.javascriptDialogOpening") { dialogs++; send("Page.handleJavaScriptDialog", { accept: answer }); }
});
const ok = (name) => console.log("ok  " + name);
const js = JSON.stringify;
const logIs = (want) => until(`document.querySelector("#log") && document.querySelector("#log").textContent === ${js(want)}`, `#log to read ${want}`);

try {
  await send("Runtime.enable");
  await send("Page.enable");
  for (const base of bases) {
    const nav = (await (await fetch(base + "/")).text()).includes("/c/nav.");
    const tag = nav ? " (navigation on)" : "";
    const load = cdp.once("Page.loadEventFired");
    await send("Page.navigate", { url: base + "/" });
    await load;
    await until("window.brick && document.querySelector('#edit-1')", "the page");
    if (nav) await until("window.brick.navigate", "nav.js");

    await person.click("#edit-1");
    await logIs("PATCH /posts/1");
    ok("a brick:method=patch link arrives as PATCH" + tag);

    answer = false;
    const before = dialogs;
    await person.click("#delete-2");
    await sleep(SETTLE_MS);
    assert.equal(dialogs, before + 1, "the confirm dialog was shown");
    assert.equal(await evaluate(`document.querySelector("#log").textContent`), "PATCH /posts/1");
    assert.ok(await evaluate(`!!document.querySelector("#post-2")`));
    ok("brick:confirm cancelled sends nothing" + tag);

    answer = true;
    await person.click("#delete-2");
    await logIs("DELETE /posts/2");
    assert.ok(await evaluate(`!document.querySelector("#post-2")`));
    ok("brick:confirm accepted sends DELETE" + tag);

    await person.click("#create");
    await logIs("POST /posts");
    await until(`document.querySelector("#post-3")`, "the new post");
    ok("a component call's brick:method reaches its <a> and POSTs" + tag);

    await person.click("#plain");
    await until(`document.body && document.body.textContent.trim() === "post 1"`, "the plain link's page");
    ok("a plain link still sends GET" + tag);
  }
  assert.deepEqual(errors, []);
  ok("no page errors");
} finally {
  cdp.close();
}
