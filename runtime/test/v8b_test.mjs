// Tests the v8 batch B runtime in jsdom: renderless answers and replies
// and URL-synced state: the query effect, and
// Back/Forward restoring queried fields in place.
import { readFileSync } from "node:fs";
import assert from "node:assert/strict";
import { JSDOM, VirtualConsole } from "jsdom";
import { tick, click, popped } from "./dom.mjs";

const runtimeJS = readFileSync(new URL("../runtime.js", import.meta.url), "utf8");
const navJS = readFileSync(new URL("../nav.js", import.meta.url), "utf8");
const ENVELOPE = "application/vnd.brick.action+json";

// setup loads html at url with the runtime (and nav.js when opts.nav).
// respond(body) returns the envelope object for an action; a GET returns
// opts.pages[path] as HTML.
async function setup(html, url, respond, opts = {}) {
  const dom = new JSDOM(`<!doctype html><html><head><title>t</title></head><body>${html}</body></html>`, { url, runScripts: "outside-only", pretendToBeVisual: true, virtualConsole: new VirtualConsole() });
  const w = dom.window;
  const sent = [];
  const pages = [];
  w.fetch = async (u, init = {}) => {
    if (init.method === "POST") {
      const body = JSON.parse(init.body);
      sent.push(body);
      const env = Object.assign({ v: 1, version: 1, effects: [] }, respond(body));
      return { status: 200, ok: true, statusText: "OK", headers: { get: () => ENVELOPE }, text: async () => JSON.stringify(env) };
    }
    pages.push(String(u));
    const p = new URL(String(u), url).pathname;
    const body = (opts.pages || {})[p];
    return { ok: Boolean(body), url: String(u), headers: { get: () => "text/html" }, text: async () => body || "" };
  };
  w.scrollTo = () => {};
  w.eval(runtimeJS);
  if (opts.nav) w.eval(navJS);
  w.brick._load = () => {};
  await tick();
  return { w, doc: w.document, $: (id) => w.document.getElementById(id), sent, pages };
}


// ---- 8 · renderless answers -------------------------------------------------

// brick.action resolves with the reply; the root takes the new handle and
// token without a morph, and the next action sends that handle.
{
  const html = `<div id="r" data-brick="c1" data-brick-state="default.H1" data-brick-token="T1"><input id="q" value="a"><span id="n">0</span></div>`;
  const { w, $, sent } = await setup(html, "https://site.test/p/", (b) => (b.action === "suggest" ? { handle: "default.H2", reply: ["ada", "al"] } : { html: `<div id="r" data-brick="c1" data-brick-state="default.H3" data-brick-token="T1"><input id="q" value="a"><span id="n">1</span></div>` }));
  const span = $("n");
  const reply = await w.brick.action($("q"), "suggest", ["a"]);
  assert.equal(JSON.stringify(reply), JSON.stringify(["ada", "al"]), "brick.action resolves with the reply");
  assert.equal($("r").getAttribute("data-brick-state"), "default.H2", "the root takes the new handle");
  assert.equal($("n"), span, "no morph");
  assert.equal(span.textContent, "0", "the view is left as it is");
  const none = await w.brick.action($("q"), "bump");
  assert.equal(none, undefined, "no reply resolves undefined");
  assert.equal(sent[1].handle, "default.H2", "the next action uses the new handle");
  assert.equal($("n").textContent, "1", "a rendering answer still morphs");
}

// ---- 10 · the query effect ---------------------------------------------------

// The effect merges its parameters into the URL, leaves a foreign one
// alone, replaces or pushes, and adds nothing when the URL is unchanged.
{
  const html = `<div id="r" data-brick="c1" data-brick-state="H" data-brick-token="T"><button id="go" brick:click="go">go</button></div>`;
  let effects = [];
  const { w, $ } = await setup(html, "https://site.test/p/?search=x&other=1", () => ({ html: "", effects }));
  const len0 = w.history.length;
  effects = [{ kind: "query", data: { set: { page: "2", status: ["open", "late"] }, del: ["search"] }, replace: true }];
  click(w, $("go"));
  await tick();
  assert.equal(w.location.search, "?other=1&page=2&status=open&status=late", "merged, foreign kept");
  assert.equal(w.history.length, len0, "replace adds no entry");
  effects = [{ kind: "query", data: { set: { page: "3" } } }];
  click(w, $("go"));
  await tick();
  assert.equal(w.history.length, len0 + 1, "push adds an entry");
  click(w, $("go"));
  await tick();
  assert.equal(w.history.length, len0 + 1, "an unchanged URL adds nothing");
}

// Back after a pushed query restores the fields in place: the owning root
// gets $refresh with sets from the URL (a missing parameter sends the
// declared value), and nav.js fetches no page. A Back that changes the
// path is a navigation.
{
  const q = JSON.stringify({ page: ["page", 1], search: ["q", ""], status: ["status", []] }).replace(/"/g, "&quot;");
  const html = `<div id="r" data-brick="c1" data-brick-state="H" data-brick-token="T" data-brick-query="${q}"><button id="go" brick:click="go">go</button></div>`;
  let effects = [];
  const { w, $, sent, pages } = await setup(html, "https://site.test/list?search=acme", () => ({ html: "", effects }), { nav: true, pages: { "/other": "<!doctype html><html><head><title>o</title></head><body><main>other</main></body></html>" } });
  effects = [{ kind: "query", data: { set: { page: "2", status: ["open"] } } }];
  click(w, $("go"));
  await tick();
  assert.equal(w.location.search, "?search=acme&page=2&status=open");
  effects = [];
  await popped(w, () => w.history.back());
  await tick();
  assert.equal(w.location.search, "?search=acme");
  const last = sent.at(-1);
  assert.equal(last.action, "$params", "Back sends $params ($refresh plus the brick: {params:} hook)");
  assert.deepEqual(last.sets, [["page", 1], ["q", "acme"], ["status", []]], "fields from the URL, declared for missing ones");
  assert.equal(pages.length, 0, "no page fetch");
  await popped(w, () => w.history.forward());
  await tick();
  assert.deepEqual(sent.at(-1).sets, [["page", "2"], ["q", "acme"], ["status", ["open"]]], "Forward restores too");
  assert.equal(pages.length, 0, "still no page fetch");
}

// A popstate that changes a parameter no root owns is a navigation.
{
  const q = JSON.stringify({ page: ["page", 1] }).replace(/"/g, "&quot;");
  const html = `<div id="r" data-brick="c1" data-brick-state="H" data-brick-token="T" data-brick-query="${q}"></div>`;
  const { w, sent, pages } = await setup(html, "https://site.test/list?tab=a", () => ({}), { nav: true, pages: { "/list": "<!doctype html><html><head><title>l</title></head><body><main>list</main></body></html>" } });
  w.history.pushState({ brick: true }, "", "/list?tab=b");
  w.brick._shown(w.location.href); // as nav.js records a page it showed
  await popped(w, () => w.history.back());
  await tick(20);
  assert.equal(sent.length, 0, "no action for a foreign parameter");
  assert.equal(pages.length, 1, "nav.js navigates");
}

console.log("v8b runtime ok");
