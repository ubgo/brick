// brick navigation. Internal links and forms fetch the next
// page and patch it into the current one instead of reloading, so unchanged
// parts — a header, a playing video, a mounted component — stay as they are.
// Any failure falls back to a full page load.
(() => {
  if (!window.brick || window.brick.navigate) return;
  const brick = window.brick;
  // CACHE_SIZE bounds the snapshot cache (least recently used goes first):
  // enough for back/forward through a browsing session, small enough that
  // the kept HTML strings stay a few MB at most.
  const CACHE_SIZE = 20;
  // PREFETCH_TTL (ms) is how long a prefetched page may be shown without
  // fetching it again: a hover-to-click gap is seconds, and older
  // speculative copies risk showing stale data. A page actually visited is
  // reused regardless (back/forward shows what was seen).
  const PREFETCH_TTL = 30000;
  const cache = new Map(); // url -> {html, url, at, prefetched}
  // NAV_PROGRESS_MS is how long a navigation fetch runs before <html> gets
  // data-navigating: a fast page never flashes a progress bar.
  const NAV_PROGRESS_MS = 150;
  // HOVER_INTENT_MS is how long a pointer rests on a brick:prefetch link
  // before it prefetches, so passing over a menu does not fetch every item.
  const HOVER_INTENT_MS = 65;
  // RESTORE_AGAIN_MS is the window in which the first morph after a back
  // navigation restores the scroll position once more (content that was
  // still loading may have made the first restore land short).
  const RESTORE_AGAIN_MS = 500;

  // ---- fetching and the snapshot cache ------------------------------------

  function remember(url, entry) {
    cache.delete(url);
    cache.set(url, entry);
    while (cache.size > CACHE_SIZE) cache.delete(cache.keys().next().value);
  }

  // ---- fragment mode -------------------------------
  // The site layer marks where the page sits inside its layout with two
  // comments and names the rest of the document with a fingerprint
  // (<meta name="brick-layout">). A navigation sends that fingerprint as
  // Brick-Nav; when the next page's layout renders to the same bytes the
  // server answers with only the page (Brick-Fragment), which is spliced
  // between the marks. Anything else is a full page, as before.
  const PAGE_START = "brick:page";
  const PAGE_END = "/brick:page";
  let layout = (document.querySelector('meta[name="brick-layout"]') || { content: "" }).content;
  // pageMarks finds the one pair of marks under root, both children of one
  // element with the start first; null otherwise (no layout, a page that
  // renders children twice, markup that moved them), which means full
  // pages only.
  function pageMarks(root) {
    const w = document.createTreeWalker(root, NodeFilter.SHOW_COMMENT);
    let start = null, end = null;
    for (let n = w.nextNode(); n; n = w.nextNode()) {
      if (n.data === PAGE_START) { if (start) return null; start = n; }
      else if (n.data === PAGE_END) { if (end || !start) return null; end = n; }
    }
    return start && end && start.parentNode === end.parentNode ? [start, end] : null;
  }

  // fetchPage requests url (a GET, or init's method and body) and returns
  // the HTML page it answers with and the final URL after redirects; any
  // other answer (an error status, a download, JSON) throws, which the
  // caller turns into a full page load. A GET from a page with its marks in
  // place asks for a fragment; the entry records whether it got one and
  // against which layout.
  async function fetchPage(url, init) {
    const headers = { Accept: "text/html" };
    const get = !init || !init.method || init.method === "GET";
    if (get && layout && !(init && init.cache === "no-store") && pageMarks(document.body)) headers["Brick-Nav"] = layout;
    const res = await fetch(url, { credentials: "same-origin", headers, ...init });
    const type = res.headers.get("Content-Type") || "";
    if (!res.ok || !type.includes("text/html")) throw new Error("not a page");
    const fragment = res.headers.get("Brick-Fragment") === "1";
    return { html: await res.text(), url: res.url || url, fragment, layout: res.headers.get("Brick-Layout") || "" };
  }

  async function load(url, init) {
    const hit = !init && cache.get(url);
    // A fragment fits only the layout it was cut from.
    if (hit && hit.fragment && hit.layout !== layout) cache.delete(url);
    else if (hit && (!hit.prefetched || Date.now() - hit.at < PREFETCH_TTL)) return hit;
    const entry = { ...(await fetchPage(url, init)), at: Date.now(), prefetched: false };
    if (!init) remember(url, entry);
    return entry;
  }

  // ---- showing a page -----------------------------------------------------

  function mergeAssets(doc) {
    const have = new Set([...document.querySelectorAll("script[src], link[rel=stylesheet][href]")].map((e) => e.getAttribute("src") || e.getAttribute("href")));
    let target = document.querySelector("brick-assets");
    if (!target) { target = document.createElement("brick-assets"); document.body.appendChild(target); }
    doc.querySelectorAll("brick-assets script[src], brick-assets link[rel=stylesheet][href]").forEach((el) => {
      const url = el.getAttribute("src") || el.getAttribute("href");
      if (have.has(url)) return;
      have.add(url);
      const copy = document.createElement(el.tagName.toLowerCase());
      for (const a of [...el.attributes]) copy.setAttribute(a.name, a.value);
      target.appendChild(copy);
    });
  }

  function announce(text) {
    let live = document.getElementById("brick-live");
    if (!live) {
      live = document.createElement("div");
      live.id = "brick-live";
      live.setAttribute("aria-live", "polite");
      live.setAttribute("style", "position:absolute;width:1px;height:1px;overflow:hidden;clip:rect(0 0 0 0)");
      document.documentElement.appendChild(live);
    }
    live.textContent = text;
  }

  // patchIn morphs a fetched page's body, title and description into the
  // current document.
  function patchIn(doc) {
    mergeAssets(doc);
    // Keep the current <brick-assets> (modules already running); morph the rest.
    const assets = document.querySelector("brick-assets");
    doc.querySelectorAll("brick-assets").forEach((e) => e.remove());
    if (assets) assets.remove();
    brick.morph(document.body, doc.body);
    if (assets) document.body.appendChild(assets);
    document.title = doc.title;
    const desc = doc.querySelector('meta[name="description"]');
    const cur = document.querySelector('meta[name="description"]');
    if (desc && cur) cur.setAttribute("content", desc.getAttribute("content") || "");
    const lay = doc.querySelector('meta[name="brick-layout"]');
    layout = lay ? lay.getAttribute("content") || "" : "";
  }

  // spliceIn morphs a fragment's page into the region between the marks,
  // leaving the layout around it untouched. The region is wrapped for the
  // morph (display: contents, so nothing reflows) and unwrapped in the same
  // task, before anything paints.
  function spliceIn(doc, marks) {
    // The fragment's asset blocks only say what to load; the page keeps
    // its own <brick-assets>, as patchIn does.
    mergeAssets(doc);
    doc.querySelectorAll("brick-assets").forEach((e) => e.remove());
    const [start, end] = marks;
    const wrap = document.createElement("brick-page");
    wrap.style.display = "contents";
    const old = [];
    for (let n = start.nextSibling; n && n !== end; n = n.nextSibling) old.push(n);
    end.before(wrap);
    wrap.append(...old);
    const next = document.createElement("brick-page");
    next.append(...doc.body.childNodes);
    brick.morph(wrap, next);
    wrap.replaceWith(...wrap.childNodes);
    document.title = doc.title;
    const desc = doc.querySelector('meta[name="description"]');
    const cur = document.querySelector('meta[name="description"]');
    if (desc && cur) cur.setAttribute("content", desc.getAttribute("content") || "");
  }

  // show patches entry in. carry holds the brick:scroll positions to put
  // on the new page's containers (G20); it is applied inside the swap, which
  // a view transition runs later, and recorded for the new history entry.
  function show(entry, url, push, replace, restore, carry, popped) {
    const doc = new DOMParser().parseFromString(entry.html, "text/html");
    const swap = () => {
      // The marks are looked up again inside the swap, which a view
      // transition runs later; gone by then, the full page is loaded.
      const marks = entry.fragment && pageMarks(document.body);
      if (entry.fragment && !marks) { brick._load(entry.url || url); return; }
      if (marks) spliceIn(doc, marks); else patchIn(doc);
      applyBoxes(carry);
      const k = keyOf();
      if (k) boxes.set(k, readBoxes());
    };
    if (push) history[replace ? "replaceState" : "pushState"]({ brick: true, key: newKey() }, "", entry.url || url);
    if (document.startViewTransition) document.startViewTransition(swap); else swap();
    markCurrent();
    const hash = new URL(entry.url || url, location.href).hash;
    const target = hash && document.getElementById(decodeURIComponent(hash.slice(1)));
    if (target) target.scrollIntoView();
    else if (restore) {
      // Back/forward: put the reader where they were, once now and once
      // after the first morph that follows soon.
      window.scrollTo(restore[0], restore[1]);
      if (typeof brick.hook === "function") {
        const off = brick.hook("morphed", () => { off(); window.scrollTo(restore[0], restore[1]); });
        setTimeout(off, RESTORE_AGAIN_MS);
      }
    } else window.scrollTo(0, 0);
    // Back/forward: content still loading may have cut a container short,
    // so its position is put back once more on the first morph that follows.
    if (popped && carry && typeof brick.hook === "function") {
      const off = brick.hook("morphed", () => { off(); applyBoxes(carry); });
      setTimeout(off, RESTORE_AGAIN_MS);
    }
    const focus = document.querySelector("main") || document.querySelector("h1");
    if (focus) {
      if (!focus.hasAttribute("tabindex")) focus.setAttribute("tabindex", "-1");
      focus.focus({ preventScroll: true });
    }
    announce(document.title);
  }

  // runtimeOf names the brick runtime files a page loads (content-hashed,
  // so a deploy that changed them changes the names). This tab keeps the
  // runtime it started with, so a page built for another runtime is shown
  // with a full load instead of being patched in.
  const RUNTIME = 'script[src*="/c/runtime."], script[src*="/c/nav."]';
  const runtimeOf = (root) => [...root.querySelectorAll(RUNTIME)].map((s) => s.getAttribute("src")).sort().join(" ");
  const ourRuntime = runtimeOf(document);

  // ---- scroll positions: history entry key -> [x, y], kept while the
  // reader scrolls, because by the time popstate fires the entry being
  // left is no longer the current one and its state cannot be written.
  const positions = new Map();
  let keySeq = 0;
  const newKey = () => Date.now().toString(36) + "." + (keySeq++);
  const keyOf = () => (history.state && history.state.key) || "";
  const savePosition = () => { const k = keyOf(); if (k) positions.set(k, [window.scrollX || 0, window.scrollY || 0]); };
  // Saved on every scroll event (at most once a frame; a Map write), so
  // the position is current when a popstate arrives.
  window.addEventListener("scroll", savePosition, { passive: true });

  // ---- scroll containers (G20): an element with brick:scroll="<name>"
  // keeps its position across navigations. A forward navigation carries the
  // current positions to the next page's containers of the same name (a
  // sidebar stays where the reader left it, even when the morph replaced or
  // moved the node, which resets scrollTop); back/forward restore what that
  // history entry had. boxes is history key -> {name: [left, top]}, kept
  // while the reader scrolls, for the same reason as positions.
  const SCROLLER = "[brick\\:scroll]";
  const boxes = new Map();
  // scrollersOf maps each name to its container; with a name used twice the
  // first wins, as with brick:ref.
  const scrollersOf = (root) => {
    const m = new Map();
    root.querySelectorAll(SCROLLER).forEach((el) => {
      const n = el.getAttribute("brick:scroll");
      if (n && !m.has(n)) m.set(n, el);
    });
    return m;
  };
  const readBoxes = () => {
    const o = {};
    scrollersOf(document).forEach((el, n) => { o[n] = [el.scrollLeft, el.scrollTop]; });
    return o;
  };
  const applyBoxes = (saved) => {
    if (!saved) return;
    scrollersOf(document).forEach((el, n) => {
      const p = saved[n];
      if (p) { el.scrollLeft = p[0]; el.scrollTop = p[1]; }
    });
  };
  // Element scroll events do not bubble, so this listens in the capture
  // phase; only a brick:scroll container's scroll is recorded.
  document.addEventListener("scroll", (e) => {
    const t = e.target;
    if (!t || t.nodeType !== 1 || !t.hasAttribute("brick:scroll")) return;
    const k = keyOf();
    if (k) boxes.set(k, readBoxes());
  }, { capture: true, passive: true });

  // navigate shows url in place. The "navigate" event may cancel it
  // (detail.preventDefault()); a cancelled back/forward puts the
  // shown URL back in the address bar. html[data-navigating] marks a fetch
  // that takes longer than NAV_PROGRESS_MS.
  async function navigate(url, { push = true, init, replace = false, popped = false } = {}) {
    let prevented = false;
    brick.emit("navigate", { url, preventDefault: () => { prevented = true; } });
    if (prevented) {
      if (popped) history.pushState({ brick: true, key: newKey() }, "", shown);
      return;
    }
    // On back/forward the current entry is already the one arrived at, so
    // only a forward navigation records where the reader leaves from.
    if (!popped) { savePosition(); const k = keyOf(); if (k) boxes.set(k, readBoxes()); }
    const restore = popped ? positions.get(keyOf()) : null;
    // A container keeps where it is now unless the entry arrived at saved
    // its own position.
    const carry = popped ? { ...readBoxes(), ...(boxes.get(keyOf()) || {}) } : readBoxes();
    // A patch: a GET to this page that
    // changes only query parameters components on it own. Those components
    // get $params with the new values and run their brick: {params:} hook;
    // nothing is fetched and nothing else on the page changes.
    if (!init && typeof brick._patchQuery === "function" && brick._patchQuery(url)) {
      history[replace ? "replaceState" : "pushState"]({ brick: true, key: newKey() }, "", url);
      brick.emit("navigated", { url, patched: true });
      return;
    }
    const html = document.documentElement;
    const progress = setTimeout(() => html.setAttribute("data-navigating", ""), NAV_PROGRESS_MS);
    try {
      const entry = await load(url, init);
      if (entry.fragment && !pageMarks(document.body)) throw new Error("no marks");
      const theirs = runtimeOf(new DOMParser().parseFromString(entry.html, "text/html"));
      if (ourRuntime && theirs && theirs !== ourRuntime) throw new Error("runtime changed");
      show(entry, url, push, replace, restore, carry, popped);
      if (!init) remember(entry.url, entry);
      brick.emit("navigated", { url: entry.url || url });
    } catch {
      brick._load(url);
    } finally {
      clearTimeout(progress);
      html.removeAttribute("data-navigating");
    }
  }

  // ---- refresh: the dev server's live reload -----------------

  // assetURLs lists the stylesheets and scripts a page loads; headStyles
  // its inline <style> blocks in <head>, which the morph never touches.
  const assetURLs = (root) => [...root.querySelectorAll("script[src], link[rel=stylesheet][href]")].map((e) => e.getAttribute("src") || e.getAttribute("href"));
  const headStyles = (root) => [...root.querySelectorAll("head style")].map((e) => e.textContent);

  // refresh fetches the page being shown again and morphs it in place, so a
  // template edit shows without losing the scroll position, focus or typed
  // input. A page that loads a stylesheet or script this one does not have
  // (a changed asset gets a new content-hashed name), or a changed inline
  // head style, needs the new code to run from the start, so it gets a full
  // reload; so does any failed fetch (the dev error page answers 500). Only
  // what the new page adds counts: assets earlier navigations merged in
  // stay loaded and are not a change.
  async function refresh() {
    cache.clear(); // every cached snapshot predates the edit
    try {
      const { html } = await fetchPage(location.href, { cache: "no-store" });
      const doc = new DOMParser().parseFromString(html, "text/html");
      const urls = new Set(assetURLs(document));
      const styles = new Set(headStyles(document));
      if (assetURLs(doc).some((u) => !urls.has(u)) || headStyles(doc).some((t) => !styles.has(t))) throw new Error("assets changed");
      const carry = readBoxes();
      patchIn(doc);
      applyBoxes(carry);
      markCurrent();
      brick.emit("refreshed", { url: location.href });
    } catch {
      brick._reload();
    }
  }

  // ---- brick:current ----------------------------------------

  // trimSlash drops a trailing slash so /docs and /docs/ compare equal.
  const trimSlash = (p) => (p.length > 1 && p.endsWith("/") ? p.slice(0, -1) : p);
  // isCurrent: the link's path is the page's, or (unless exact) a segment
  // prefix of it — /docs is current on /docs/intro, never on /docsearch. A
  // link to the site's root is current on the root only.
  function isCurrent(href, exact) {
    let u;
    try { u = new URL(href, location.href); } catch { return false; }
    if (u.origin !== location.origin) return false;
    const link = trimSlash(u.pathname);
    const page = trimSlash(location.pathname);
    if (link === page) return true;
    if (exact || link === "/" || link === trimSlash(base || "/")) return false;
    return page.startsWith(link + "/");
  }
  // markCurrent sets aria-current="page" and data-current on each
  // brick:current link to the page being shown, and removes them from the
  // ones that no longer match. Links without brick:current are left alone.
  function markCurrent() {
    document.querySelectorAll("a[brick\\:current], a[brick\\:current\\.exact]").forEach((a) => {
      if (isCurrent(a.getAttribute("href") || "", a.hasAttribute("brick:current.exact"))) {
        a.setAttribute("aria-current", "page");
        a.setAttribute("data-current", "");
      } else {
        a.removeAttribute("aria-current");
        a.removeAttribute("data-current");
      }
    });
  }

  // ---- what is intercepted -------------------------------------------------

  const optedOut = (el) => {
    for (let n = el; n && n.nodeType === 1; n = n.parentElement) {
      if (n.getAttribute("brick:nav") === "off") return true;
    }
    return false;
  };

  // base is the path this site is mounted under when one server serves
  // several sites from one origin (<meta name="brick-base">, written by the
  // site layer); "" at the root. A link out of the base belongs to another
  // site, with other assets and runtime, so it is a normal page load.
  const base = (document.querySelector('meta[name="brick-base"]') || { content: "" }).content;
  const inBase = (p) => !base || p === base || p.startsWith(base + "/");

  function internal(href) {
    let u;
    try { u = new URL(href, location.href); } catch { return null; }
    if (u.origin !== location.origin || !/^https?:$/.test(u.protocol)) return null;
    if (!inBase(u.pathname)) return null;
    if (u.pathname === location.pathname && u.search === location.search && u.hash) return null;
    return u;
  }

  document.addEventListener("click", (e) => {
    if (e.defaultPrevented || e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return;
    const a = e.target.closest && e.target.closest("a[href]");
    // A brick:method link (v23) is runtime.js's; its click never navigates
    // with a GET, even if a listener before it left the default alone.
    if (!a || optedOut(a) || a.hasAttribute("download") || (a.getAttribute("brick:method") || "").trim()) return;
    if (a.target && a.target !== "_self") return;
    const u = internal(a.getAttribute("href"));
    if (!u) return;
    e.preventDefault();
    navigate(u.href, { replace: a.getAttribute("brick:nav") === "replace" });
  });

  document.addEventListener("submit", (e) => {
    const form = e.target;
    if (e.defaultPrevented || optedOut(form) || (form.target && form.target !== "_self")) return;
    // A method="dialog" form (or formmethod="dialog" button) only closes
    // its <dialog>; it never sends anything, so it is never a navigation.
    const method = ((e.submitter && e.submitter.getAttribute("formmethod")) || form.getAttribute("method") || "").toLowerCase();
    if (method === "dialog") return;
    const u = internal(form.getAttribute("action") || location.href);
    if (!u) return;
    e.preventDefault();
    cache.clear(); // any write may change what cached pages show
    const data = new FormData(form, e.submitter);
    if ((form.getAttribute("method") || "get").toLowerCase() === "get") {
      u.search = new URLSearchParams(data).toString();
      navigate(u.href);
    } else {
      navigate(u.href, { init: { method: "POST", body: data } });
    }
  });

  // The page being shown, without its hash: a popstate that only changes
  // the hash (a #section link) is the browser scrolling, not a navigation.
  const pageOf = (href) => { const u = new URL(href, location.href); u.hash = ""; return u.href; };
  let shown = pageOf(location.href);
  window.addEventListener("popstate", () => {
    if (pageOf(location.href) === shown) return;
    // The runtime restored a query-only change in place.
    if (brick._popHandled === location.href) { brick._popHandled = ""; shown = pageOf(location.href); return; }
    navigate(location.href, { push: false, popped: true });
  });
  // A query effect pushed or replaced the URL: it is the page shown now.
  brick._shown = (href) => { shown = pageOf(href); };
  brick.on("navigated", (d) => { shown = pageOf(d.url); });

  // ---- prefetch ------------------------------------------------------------

  // A link that opted out of navigation is a full page load, which the
  // browser's own speculation rules prefetch (the site layer writes them);
  // a snapshot here would never be used.
  const prefetch = (a) => {
    if (optedOut(a)) return;
    const u = internal(a.getAttribute("href"));
    if (!u || cache.has(u.href)) return;
    // A failed prefetch is not an error: the click will fetch (or fall back).
    fetchPage(u.href)
      .then(({ html, fragment, layout: cut }) => remember(u.href, { html, url: u.href, fragment, layout: cut, at: Date.now(), prefetched: true }))
      .catch(() => {});
  };
  // A brick:prefetch link prefetches after HOVER_INTENT_MS of hover, and
  // at once on keyboard focus or a press (the click is coming).
  const prefetchable = (t) => {
    const a = t && t.closest && t.closest("a[href][brick\\:prefetch]");
    // A brick:method link (v23) posts on click; its GET is never shown.
    return a && a.getAttribute("brick:prefetch") !== "visible" && !a.hasAttribute("brick:method") ? a : null;
  };
  let hoverTimer = 0;
  document.addEventListener("pointerover", (e) => {
    const a = prefetchable(e.target);
    clearTimeout(hoverTimer);
    if (a) hoverTimer = setTimeout(() => prefetch(a), HOVER_INTENT_MS);
  });
  document.addEventListener("pointerout", () => clearTimeout(hoverTimer));
  ["focusin", "pointerdown"].forEach((t) => document.addEventListener(t, (e) => {
    const a = prefetchable(e.target);
    if (a) prefetch(a);
  }));
  if ("IntersectionObserver" in window) {
    const io = new IntersectionObserver((entries) => entries.forEach((en) => {
      if (en.isIntersecting) { io.unobserve(en.target); prefetch(en.target); }
    }));
    const watch = (root) => root.querySelectorAll && root.querySelectorAll('a[href][brick\\:prefetch="visible"]:not([brick\\:method])').forEach((a) => io.observe(a));
    watch(document);
    new MutationObserver((rs) => rs.forEach((r) => r.addedNodes.forEach(watch))).observe(document.documentElement, { childList: true, subtree: true });
  }

  // The full-load fallbacks; replaceable so tests can observe them.
  brick._load = (url) => { location.href = url; };
  brick._reload = () => { location.reload(); };
  // Our own back/forward restores scroll (above); the browser's would jump
  // before the new page is patched in.
  if ("scrollRestoration" in history) history.scrollRestoration = "manual";
  history.replaceState({ brick: true, key: newKey() }, "", location.href);
  markCurrent();
  // navigate(url, {replace}) shows url in place; replace swaps the history
  // entry instead of adding one (as brick:nav="replace" on a link does).
  brick.navigate = (url, { replace = false } = {}) => navigate(new URL(url, location.href).href, { replace });
  // refresh() re-renders the current page in place (the dev server's live
  // reload calls it), falling back to a full reload when assets changed.
  brick.refresh = refresh;
})();
