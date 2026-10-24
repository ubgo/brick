// brick mount runtime. Runs each component type's client code
// once per element — for elements on the page and ones inserted later — and
// runs the cleanup it returns when the element is removed.
(() => {
  if (window.brick) return;
  const types = new Map();
  const cleanups = new Map();
  const mounted = new WeakSet();
  const bus = new EventTarget();
  const sel = "[data-brick-type]";
  // STATEFUL marks a stateful component's root: the element
  // an action patches, and the scope every brick:* event looks up to.
  const STATEFUL = "[data-brick-state]";
  // PERSIST selects the elements a morph moves instead of re-creating.
  const PERSIST = "[brick\\:persist][id]";
  const propsOf = (el) => {
    try { return JSON.parse(el.getAttribute("data-brick-props") || "{}"); } catch { return {}; }
  };
  // serverHTML is each mounted element's markup as the server sent it,
  // recorded before its mount enhanced it; the morph compares against it.
  const serverHTML = new WeakMap();
  const run = (el) => {
    const fn = types.get(el.getAttribute("data-brick-type"));
    if (!fn || mounted.has(el) || !el.isConnected) return;
    mounted.add(el);
    serverHTML.set(el, el.outerHTML);
    const done = fn(el, propsOf(el));
    if (typeof done === "function") cleanups.set(el, done);
  };
  // waiting holds the IntersectionObserver of an element whose mount waits
  // for it to scroll into view, so removing it first stops the observer
  // (unmount) instead of leaking it.
  const waiting = new WeakMap();
  const schedule = (el) => {
    const when = el.getAttribute("data-brick-mount");
    if (when === "visible" && "IntersectionObserver" in window) {
      const io = new IntersectionObserver((entries) => {
        if (entries.some((e) => e.isIntersecting)) { io.disconnect(); waiting.delete(el); run(el); }
      });
      waiting.set(el, io);
      io.observe(el);
    } else if (when === "idle" && "requestIdleCallback" in window) {
      requestIdleCallback(() => run(el));
    } else {
      run(el);
    }
  };
  const each = (root, fn) => {
    if (root.nodeType !== 1) return;
    if (root.matches(sel)) fn(root);
    root.querySelectorAll(sel).forEach(fn);
  };
  const unmount = (el) => {
    const io = waiting.get(el);
    if (io) { io.disconnect(); waiting.delete(el); }
    const done = cleanups.get(el);
    cleanups.delete(el);
    mounted.delete(el);
    if (done) done();
  };
  new MutationObserver((records) => {
    for (const r of records) {
      r.removedNodes.forEach((n) => { if (!n.isConnected) each(n, unmount); });
      r.addedNodes.forEach((n) => { each(n, schedule); if (n.nodeType === 1) scanJS(n); });
    }
  }).observe(document.documentElement, { childList: true, subtree: true });
  // ---- the morph ---------------------------------------------------------

  // keyOf is a node's morph key among its siblings: its id, else its
  // brick:key (sibling-scoped, so two copies of a component may reuse
  // keys), else a stateful child's data-brick id.
  const keyOf = (n) => n.nodeType === 1 ? (n.id || n.getAttribute("brick:key") || n.getAttribute("data-brick") || "") : "";
  // warnedKeys holds each duplicate key already reported, so a list
  // re-rendered every second warns once rather than every time.
  const warnedKeys = new Set();
  function warnDuplicateKeys(next) {
    const seen = new Set();
    for (const n of next.childNodes) {
      const k = n.nodeType === 1 && !n.id ? n.getAttribute("brick:key") : null;
      if (!k) continue;
      if (seen.has(k) && !warnedKeys.has(k)) { warnedKeys.add(k); console.warn("brick: duplicate brick:key " + JSON.stringify(k) + " among siblings; only the first is matched"); }
      seen.add(k);
    }
  }
  // IGNORE / IGNORE_SELF / REPLACE are the morph controls. Each
  // applies only when the old and the new element both carry it, so the
  // server can take the control away by rendering the element without it.
  const IGNORE = "brick:ignore";
  const IGNORE_SELF = "brick:ignore.self";
  const REPLACE = "brick:replace";
  const both = (a, b, attr) => a.nodeType === 1 && b.nodeType === 1 && a.hasAttribute(attr) && b.hasAttribute(attr);
  const persistent = (n) => n.nodeType === 1 && n.hasAttribute("brick:persist") && n.id;
  // A <brick-keep data-brick=X> in an action's answer stands for the
  // stateful child X the page already has: the morph keeps
  // that element wholesale, as it keeps a brick:persist one. keepKey is the
  // shared map key for both: an id, or "\0" + the child's data-brick id.
  const KEEP = "brick-keep[data-brick]";
  const keepKey = (n) => n.nodeType !== 1 ? "" : persistent(n) ? n.id : n.tagName === "BRICK-KEEP" ? "\0" + n.getAttribute("data-brick") : "";
  const sameKind = (a, b) => a.nodeType === b.nodeType && (a.nodeType !== 1 || a.tagName === b.tagName);

  function syncAttrs(from, to) {
    // A matched field keeps what the user typed, unless the new page sets a
    // different value than the old page did.
    const pageValueChanged = from.getAttribute("value") !== to.getAttribute("value");
    // brick:keep="open aria-expanded" names attributes the client owns
    // (a <details> the user opened, a menu's expanded flag): the morph
    // leaves them as they are instead of resetting them to the server's.
    const keep = new Set((to.getAttribute("brick:keep") || from.getAttribute("brick:keep") || "").split(/\s+/).filter(Boolean));
    for (const a of [...from.attributes]) if (!to.hasAttribute(a.name) && !keep.has(a.name)) from.removeAttribute(a.name);
    for (const a of [...to.attributes]) if (from.getAttribute(a.name) !== a.value && !keep.has(a.name)) from.setAttribute(a.name, a.value);
    // A deferred field the server has not received yet keeps
    // what the user typed even when it is not focused.
    if ((from.tagName === "INPUT" || from.tagName === "TEXTAREA") && pageValueChanged && from !== document.activeElement && !deferredHeld(from)) {
      from.value = to.getAttribute("value") || "";
    }
    restick(from);
    // An exit or entrance playing keeps its classes and marker through a
    // morph, which would otherwise strip them (and a hide's hidden would
    // cut the exit short): the motion finishes, then settles the element.
    const m = motionHeld.get(from);
    if (m) m();
  }

  // remounting patches a mounted component. Its client code changed its
  // DOM after the server sent it (unhid a button, built chips), so syncing
  // it to new server markup would undo that. When the server's markup is
  // unchanged the element is left alone, live state and all; otherwise its
  // cleanup runs, it is patched, and it is mounted again over the new
  // markup.
  function remounting(from, to, work) {
    if (from.nodeType !== 1 || !mounted.has(from)) { work(); return; }
    if (serverHTML.get(from) === to.outerHTML) return;
    unmount(from);
    work();
    schedule(from);
  }

  function morphNode(from, to, keep) {
    if (from.nodeType === 3 || from.nodeType === 8) {
      if (from.nodeValue !== to.nodeValue) from.nodeValue = to.nodeValue;
      return;
    }
    if (both(from, to, IGNORE) || !morphAllowed(from, to)) return;
    const self = both(from, to, IGNORE_SELF);
    remounting(from, to, () => { if (!self) syncAttrs(from, to); patch(from, to, keep); });
  }

  // morphAllowed asks the morph hooks whether from may be
  // patched into to this time. Hooks run synchronously, inside the morph;
  // a hook that throws is reported and counts as allowing.
  function morphAllowed(from, to) {
    for (const fn of hooks.morph) {
      let ok;
      try { ok = fn({ from, to }); } catch (err) { console.error(err); continue; }
      if (ok === false) return false;
    }
    return true;
  }

  // morph patches parent's children into next's. Persistent elements the
  // new page keeps are collected once, before anything changes, so one that
  // moves to another parent is moved rather than removed and recreated.
  function morph(parent, next) {
    patch(parent, next, kept(parent, next));
  }

  // morphElement patches one element into another (an action's result).
  function morphElement(el, next) {
    if (!morphAllowed(el, next)) {
      // The handle and token are the engine's, not the author's markup: a
      // stale one would make the next action act on old state.
      for (const a of ["data-brick-state", "data-brick-token"]) if (next.hasAttribute(a)) el.setAttribute(a, next.getAttribute(a));
      return;
    }
    const keep = kept(el, next);
    remounting(el, next, () => { syncAttrs(el, next); patch(el, next, keep); });
  }

  // kept maps id → element for the persistent elements under from that the
  // new markup still has, collected before patching changes anything.
  function kept(from, next) {
    const keep = new Map();
    const wanted = new Set([...next.querySelectorAll(PERSIST)].map((n) => n.id));
    from.querySelectorAll(PERSIST).forEach((n) => { if (wanted.has(n.id)) keep.set(n.id, n); });
    const children = new Set([...next.querySelectorAll(KEEP)].map((n) => n.getAttribute("data-brick")));
    from.querySelectorAll(STATEFUL).forEach((n) => {
      const id = n.getAttribute("data-brick");
      if (id && children.has(id)) keep.set("\0" + id, n);
    });
    return keep;
  }

  // movingOut reports a kept element the new markup puts elsewhere.
  const movingOut = (o, keep) => keep.get(o.id) === o || (o.hasAttribute("data-brick") && keep.get("\0" + o.getAttribute("data-brick")) === o);

  function patch(parent, next, keep) {
    // An element playing its exit transition is already gone as far as
    // the morph is concerned: never matched, never removed twice.
    const olds = [...parent.childNodes].filter((o) => !isLeaving(o));
    const used = new Set();
    let cursor = 0;
    const take = (want) => {
      const key = keyOf(want);
      if (key) {
        const i = olds.findIndex((o, j) => !used.has(j) && keyOf(o) === key && sameKind(o, want));
        if (i >= 0) { used.add(i); return olds[i]; }
        return null;
      }
      for (let i = cursor; i < olds.length; i++) {
        if (used.has(i)) continue;
        const o = olds[i];
        if (keyOf(o)) continue; // a keyed node only matches its key
        if (sameKind(o, want)) { used.add(i); cursor = i + 1; return o; }
      }
      return null;
    };
    const result = [];
    warnDuplicateKeys(next);
    for (const want of [...next.childNodes]) {
      if (keepKey(want) && keep.has(keepKey(want))) {
        result.push(keep.get(keepKey(want)));
        continue;
      }
      const match = take(want);
      if (match && both(match, want, REPLACE)) {
        // A fresh copy, not a patch: the mutation observer unmounts the
        // old element's client code and mounts the new one's.
        match.remove();
        result.push(document.importNode(want, true));
        continue;
      }
      if (match) { morphNode(match, want, keep); result.push(match); continue; }
      const fresh = document.importNode(want, true);
      // A persistent element or kept child inside new markup is the kept
      // one, moved in.
      if (fresh.querySelectorAll) {
        fresh.querySelectorAll(PERSIST + ", " + KEEP).forEach((c) => { const k = keep.get(keepKey(c)); if (k) c.replaceWith(k); });
      }
      result.push(fresh);
    }
    olds.forEach((o, i) => {
      if (used.has(i) || result.includes(o)) return;
      if (o.nodeType === 1 && movingOut(o, keep)) return; // moving elsewhere
      removeAnimated(o);
    });
    // Put result in order, stepping over elements still leaving, which
    // stay where they were until their transition ends.
    let ref = parent.firstChild;
    for (const n of result) {
      while (ref && isLeaving(ref)) ref = ref.nextSibling;
      if (ref === n) { ref = n.nextSibling; continue; }
      parent.insertBefore(n, ref);
    }
  }

  // ---- hooks ----------------------------------------------------

  // Events (brick.on) tell; hooks decide. Each name holds its functions in
  // registration order. request and response hooks may return a promise,
  // which is awaited; morph hooks run inside the morph, so they are
  // synchronous; morphed and ready are notifications with a fixed order.
  const hooks = { request: [], response: [], morph: [], morphed: [], ready: [] };
  let ready = false;
  // PROTECTED are the request headers a hook may not change: the CSRF
  // floor and the protocol (D6). Every Brick-* header is protected too.
  const PROTECTED = new Set(["content-type", "accept"]);
  const isProtected = (h) => PROTECTED.has(h.toLowerCase()) || h.toLowerCase().startsWith("brick-");

  // requestHooks runs the request hooks over headers (changed in place)
  // and reports whether one cancelled. Changing, adding or removing a
  // protected header throws, which fails the action.
  async function requestHooks(info, headers) {
    if (!hooks.request.length) return false;
    const before = { ...headers };
    let cancelled = false;
    const r = Object.assign({}, info, { headers, cancel: () => { cancelled = true; } });
    for (const fn of [...hooks.request]) {
      await fn(r);
      if (cancelled) return true;
    }
    for (const k of new Set([...Object.keys(before), ...Object.keys(headers)])) {
      if (isProtected(k) && before[k] !== headers[k]) throw new Error("brick: a request hook may not change the " + k + " header");
    }
    return false;
  }

  // notify runs a read-only hook's functions; one that throws is reported
  // and the rest still run.
  async function notify(name, detail) {
    for (const fn of [...hooks[name]]) {
      try { await fn(detail); } catch (err) { console.error(err); }
    }
  }

  // CANCELLED is what post returns when a request hook cancelled the job.
  const CANCELLED = "cancelled";

  // ---- page config ----------------------------------

  // CONFIG is the host's tuning of this runtime: the engine writes
  // Options.Runtime as <meta name="brick-config" content='{…}'> just before
  // the runtime script, and it is read once, here. Every key is optional
  // and a missing, wrong-typed or out-of-range value keeps the default, so
  // a page without the meta behaves exactly as one written before it.
  // Keys (times in ms): debounce, pollMin, actionURL, retries, retryMaxWait.
  const CONFIG = (() => {
    const m = document.querySelector('meta[name="brick-config"]');
    if (!m) return {};
    try {
      const c = JSON.parse(m.getAttribute("content") || "{}");
      return c && typeof c === "object" && !Array.isArray(c) ? c : {};
    } catch {
      console.warn("brick: the brick-config meta is not JSON; the runtime keeps its defaults");
      return {};
    }
  })();
  // configInt reads a non-negative whole number of CONFIG, or def.
  const configInt = (key, def) => (Number.isInteger(CONFIG[key]) && CONFIG[key] >= 0 ? CONFIG[key] : def);

  // ---- retry on 503 and 429 ----------------------------

  // A 503 (the state store is down, or a limiter is) or a 429 (a rate
  // limit) means the action did not run, so sending it again later is
  // safe. RETRIES is how many times one action is sent again before
  // "action-error"; each waits for the answer's Retry-After (seconds or an
  // HTTP date), or RETRY_DEFAULT_MS without one. An answer asking for
  // longer than RETRY_MAX_WAIT_MS is not retried: the server said it will
  // still refuse, and a page frozen that long is worse than an error the
  // user can see. The job keeps its place in the root's queue meanwhile
  // (data-loading stays on), so the order and the handle chain hold.
  const RETRYABLE = new Set([429, 503]);
  const RETRIES = configInt("retries", 1);
  const RETRY_MAX_WAIT_MS = configInt("retryMaxWait", 5000);
  const RETRY_DEFAULT_MS = 1000;

  // ---- debugging ----------------------------------------------

  // debugOn logs every request the runtime sends and every answer it
  // lands; latencyMs delays each of those requests, to see loading states
  // and races on a fast local server. Both are off until brick.debug /
  // brick.latency turn them on from the console, and neither is in the
  // brick-config meta: they are for a developer's own browser, never a
  // page's behaviour.
  let debugOn = false;
  let latencyMs = 0;
  const debugLog = (...a) => { if (debugOn) console.log("brick:", ...a); };
  // lag waits latencyMs; without a latency it returns null, so the caller
  // can skip the await and keep a request in the task that started it.
  const lag = () => (latencyMs > 0 ? new Promise((r) => setTimeout(r, latencyMs)) : null);

  // retryWait is how long res asks the client to wait, in ms.
  function retryWait(res) {
    const h = (res.headers && typeof res.headers.get === "function" && res.headers.get("Retry-After")) || "";
    if (/^\s*\d+\s*$/.test(h)) return parseInt(h, 10) * 1000;
    const at = h ? Date.parse(h) : NaN;
    return Number.isFinite(at) ? Math.max(0, at - Date.now()) : RETRY_DEFAULT_MS;
  }

  // ---- view transitions on action morphs -----------------------

  const TRANSITION = "brick:transition";
  let transitioning = false;
  const reducedMotion = () => typeof matchMedia === "function" && matchMedia("(prefers-reduced-motion: reduce)").matches;

  // TRANSITION_NONE is the server's "no transition for this answer"
  // (effects.go TransitionNone).
  const TRANSITION_NONE = "none";

  // transitioned runs work inside a view transition when the root opted in
  // and the browser can; otherwise (no API, reduced motion, another
  // transition running) it runs work plainly. It resolves once work has
  // run, so effects that follow see the new DOM. choice is the action's
  // own transition effect (v22), undefined when it recorded none: "none"
  // runs work plainly, anything else is the types to use, with or without
  // brick:transition on the root.
  async function transitioned(root, work, choice) {
    if (choice === TRANSITION_NONE || (choice === undefined && !root.hasAttribute(TRANSITION)) || typeof document.startViewTransition !== "function" || reducedMotion() || transitioning) { work(); return; }
    const types = (choice === undefined ? root.getAttribute(TRANSITION) || "" : choice).split(/\s+/).filter(Boolean);
    transitioning = true;
    let vt;
    try {
      vt = types.length ? document.startViewTransition({ update: work, types }) : document.startViewTransition(work);
    } catch {
      // A browser without transition types takes the callback form only.
      vt = document.startViewTransition(work);
    }
    const done = () => { transitioning = false; };
    Promise.resolve(vt.finished).then(done, done);
    await vt.updateCallbackDone;
  }

  // transitionChoice is the types of an answer's last transition effect,
  // or undefined when it has none.
  function transitionChoice(effects) {
    let choice;
    if (Array.isArray(effects)) for (const ef of effects) if (ef && ef.kind === "transition") choice = typeof ef.name === "string" ? ef.name : "";
    return choice;
  }

  // ---- modelable children (v22, modelable.go) -------------------------------

  // MODEL_FIELD is the parent field a modelable child is bound to (written
  // by the call site) and MODEL_VALUE the child's model value as JSON
  // (written by the child on every render).
  const MODEL_FIELD = "data-brick-model";
  const MODEL_VALUE = "data-brick-model-value";

  // modelableChanged sends the parent a $set of its bound field when an answer
  // of the child root changed the child's model value. The child owns the
  // value going up, the parent owns it on its own render going down
  // (keep.go adopts a child whose model differs), so a change makes one
  // trip each way and stops: the parent's answer keeps the child, whose
  // value now agrees.
  function modelableChanged(root, before) {
    const field = root.getAttribute(MODEL_FIELD);
    const now = root.getAttribute(MODEL_VALUE);
    if (!field || now === null || now === before) return;
    const parent = root.parentElement && root.parentElement.closest(STATEFUL);
    if (!parent) return;
    let value;
    try { value = JSON.parse(now); } catch { return; }
    send(parent, "$set", [field, value], undefined, { collapse: true });
  }

  // ---- exit transitions ---------------------------------------

  // brick:leave="classes" animates an element out before it is removed (a
  // morph that drops it, a rows delete or limit trim) or hidden (a JS
  // hide): the classes are added, the runtime waits for the transition or
  // animation they start, then removes or hides it. One attribute is
  // enough for a transition too, because a transition takes its timing
  // from the style after the change: "transition-opacity duration-300
  // opacity-0" animates.
  const LEAVE = "brick:leave";
  // LEAVING marks an element while it plays, for CSS and tests.
  const LEAVING = "data-brick-leaving";
  // LEAVE_CAP_MS bounds the wait, so an animation that never ends (an
  // infinite keyframe, a typo'd class) cannot leave a ghost in the page.
  const LEAVE_CAP_MS = 2000;
  // LEAVE_SLACK_MS is added to the computed duration before the fallback
  // timer stops waiting for an end event, which can land a frame late.
  const LEAVE_SLACK_MS = 50;
  // leaving maps each element playing its exit to the function that
  // cancels it (a JS show during a hide).
  const leaving = new WeakMap();
  // removing holds the elements whose exit ends in removal (a morph or rows
  // drop): the morph and rows treat them as gone already. An exit that
  // ends in hidden (brick:show, a JS hide) is not one, so a morph during it
  // still patches the element instead of inserting a second copy.
  const removing = new WeakSet();
  const isLeaving = (n) => n.nodeType === 1 && removing.has(n);
  // motionHeld maps an element playing brick:leave or brick:enter to the
  // function that puts its motion state back after a morph synced its
  // attributes to the server's.
  const motionHeld = new WeakMap();

  // motionOf is el's computed motion in ms: [the longest duration plus
  // delay, the longest duration alone], over transitions and animations.
  function motionOf(el) {
    if (typeof getComputedStyle !== "function") return [0, 0];
    const cs = getComputedStyle(el);
    const ms = (s) => String(s || "").split(",").map((x) => { const v = parseFloat(x); return Number.isFinite(v) ? (/ms\s*$/.test(x) ? v : v * 1000) : 0; });
    let total = 0, dur = 0;
    for (const [d, l] of [[cs.transitionDuration, cs.transitionDelay], [cs.animationDuration, cs.animationDelay]]) {
      const ds = ms(d), ls = ms(l);
      ds.forEach((v, i) => { dur = Math.max(dur, v); total = Math.max(total, v + (ls[i % ls.length] || 0)); });
    }
    return [total, dur];
  }

  // leave plays el's brick:leave classes and then calls done(restore),
  // where restore takes the classes off again (a hide keeps the element).
  // Without the attribute, under reduced motion, or when the classes start
  // no motion, done runs at once. The element is inert while it plays, so
  // a row being deleted cannot be clicked twice.
  function leave(el, done) {
    const classes = el.nodeType === 1 ? (el.getAttribute(LEAVE) || "").split(/\s+/).filter(Boolean) : [];
    if (leaving.has(el)) return;
    const entered = el.nodeType === 1 && entering.get(el);
    if (entered) entered();
    if (!classes.length || reducedMotion() || !el.isConnected) { done(() => {}); return; }
    const wasInert = el.inert;
    const restore = () => { el.classList.remove(...classes); el.removeAttribute(LEAVING); el.inert = wasInert; };
    // A morph's hidden waits for the exit to finish (done hides it).
    const wasHidden = el.hasAttribute("hidden");
    const hold = () => { el.classList.add(...classes); el.setAttribute(LEAVING, ""); if (!wasHidden) el.removeAttribute("hidden"); el.inert = true; };
    hold();
    const [total, dur] = motionOf(el);
    let timer = 0;
    const stop = () => {
      el.removeEventListener("transitionend", onEnd);
      el.removeEventListener("animationend", onEnd);
      clearTimeout(timer);
      leaving.delete(el);
      motionHeld.delete(el);
    };
    const end = () => { stop(); done(restore); };
    // A shorter transition of the same element ending first is not the end.
    const onEnd = (e) => { if (e.target === el && e.elapsedTime * 1000 >= dur - 1) end(); };
    if (total <= 0) { end(); return; }
    leaving.set(el, () => { stop(); restore(); });
    motionHeld.set(el, hold);
    el.addEventListener("transitionend", onEnd);
    el.addEventListener("animationend", onEnd);
    timer = setTimeout(end, Math.min(total + LEAVE_SLACK_MS, LEAVE_CAP_MS));
  }

  // brick:enter="classes" is the look an element starts from when
  // brick:show or a JS show reveals it: the classes are added
  // before it is unhidden and taken off on the next frame, so a transition
  // on the element plays from them to its own look ("opacity-0" with the
  // element's "transition-opacity duration-300"). Classes that start an
  // animation stay until it ends instead. Nothing plays on the first
  // render, under reduced motion, or without the attribute.
  const ENTER = "brick:enter";
  // ENTERING marks an element while its entrance plays, for CSS and tests.
  const ENTERING = "data-brick-entering";
  // entering maps each element playing its entrance to the function that
  // ends it at once (a hide that starts meanwhile).
  const entering = new WeakMap();
  function enter(el) {
    const classes = (el.getAttribute(ENTER) || "").split(/\s+/).filter(Boolean);
    const prev = entering.get(el);
    if (prev) prev();
    if (!classes.length || reducedMotion() || !el.isConnected) return;
    el.classList.add(...classes);
    el.setAttribute(ENTERING, "");
    // on is whether the start classes are still meant to be on: a morph
    // before the next frame puts them back, one after only the marker.
    let timer = 0, raf = 0, dur = 0, on = true;
    motionHeld.set(el, () => { el.setAttribute(ENTERING, ""); if (on) el.classList.add(...classes); });
    const onEnd = (e) => { if (e.target === el && e.elapsedTime * 1000 >= dur - 1) finish(); };
    const finish = () => {
      clearTimeout(timer);
      if (raf && typeof cancelAnimationFrame === "function") cancelAnimationFrame(raf);
      el.removeEventListener("transitionend", onEnd);
      el.removeEventListener("animationend", onEnd);
      el.classList.remove(...classes);
      el.removeAttribute(ENTERING);
      entering.delete(el);
      motionHeld.delete(el);
    };
    entering.set(el, finish);
    const wait = () => {
      const [total, d] = motionOf(el);
      dur = d;
      if (total <= 0) { finish(); return; }
      el.addEventListener("transitionend", onEnd);
      el.addEventListener("animationend", onEnd);
      timer = setTimeout(finish, Math.min(total + LEAVE_SLACK_MS, LEAVE_CAP_MS));
    };
    const cs = typeof getComputedStyle === "function" ? getComputedStyle(el) : null;
    if (cs && parseFloat(cs.animationDuration) > 0) { wait(); return; }
    // Reading layout commits the start look before the classes come off.
    void el.offsetWidth;
    const step = () => { raf = 0; on = false; el.classList.remove(...classes); wait(); };
    if (typeof requestAnimationFrame === "function") raf = requestAnimationFrame(step); else setTimeout(step);
  }

  // removeAnimated removes n, after its exit transition when it has one.
  // An element hiding when it is dropped stops hiding and leaves for good.
  const removeAnimated = (n) => {
    if (n.nodeType === 1) {
      if (removing.has(n)) return;
      const hiding = leaving.get(n);
      if (hiding) hiding();
      removing.add(n);
    }
    leave(n, () => n.remove());
  };

  // ---- actions ----------------------------------------------

  // "add(10, \"x\")" -> ["add", [10, "x"]]; arguments are JSON literals only.
  // A leading $ names a built-in ($set, $refresh, $toggle).
  const parseCall = (s) => {
    const m = /^\s*(\$?[\w.]+)\s*(?:\((.*)\))?\s*$/s.exec(s || "");
    if (!m) throw new Error("brick: bad action " + JSON.stringify(s));
    return [m[1], m[2] && m[2].trim() ? JSON.parse("[" + m[2] + "]") : []];
  };

  // actionURL is where actions are posted: beside this runtime file
  // (<asset base>c/runtime.<hash>.js → <asset base>action), so a site
  // mounted under a base path, or with its own asset base, posts to its
  // own server. A page without the runtime script (a test) uses the
  // default.
  // crossActions says the host moved the action server (H13). Its
  // requests then carry the session cookie with credentials "include",
  // which the server must allow (CORS with credentials); the default
  // stays "same-origin", so a page never sends cookies anywhere new.
  const crossActions = typeof CONFIG.actionURL === "string" && CONFIG.actionURL !== "";
  const CREDENTIALS = crossActions ? "include" : "same-origin";
  const actionURL = () => {
    // A host's brick-config actionURL (H13) wins: a static export on a
    // CDN posting to a separate action server, which must allow CORS.
    if (crossActions) return CONFIG.actionURL;
    const s = document.querySelector('script[src*="/c/runtime."]');
    return s ? s.getAttribute("src").replace(/c\/runtime\.[^/]*$/, "action") : "/_brick/action";
  };

  // GONE (410) means the server could not load this instance (it expired,
  // the store lost it, the session changed). The action never ran, so the
  // runtime recovers the component in place and runs it again.
  const GONE = 410;
  // RECOVER_MAX recoveries per root within RECOVER_WINDOW_MS, then the
  // page reloads: a server that answers 410 to every fresh handle must not
  // loop. RELOAD_MAX reloads within RELOAD_WINDOW_MS, then the runtime
  // stops and emits "state-lost" (the count lives in sessionStorage, the
  // only thing the runtime ever stores there).
  const RECOVER_MAX = 2;
  const RECOVER_WINDOW_MS = 30000;
  const RELOAD_MAX = 3;
  const RELOAD_WINDOW_MS = 60000;
  const RELOADS_KEY = "brick:reloads";

  // ---- attribute grammar ---------------------------------------

  // GRAMMAR is every brick:<kind> attribute with its closed modifier
  // list, generated from the engine's table (grammar.go, GrammarJS) and
  // compared with it by TestGrammarMatchesRuntime, so a modifier the
  // runtime does not read is a compile error with a file and line rather
  // than silently ignored. Tokens: a bare name is a flag; "name.<ms>" takes
  // a following "<n>ms" ("<ms>?" when it may be left out); "<interval>" is
  // a poll interval ("5s") as a modifier; "<key>" is one of keys. js is
  // each brick:js command with its [min, max] argument count.
  const GRAMMAR = {"attrs":{"blur":["once","debounce.<ms>","throttle.<ms>","optimistic","window"],"change":["once","debounce.<ms>","throttle.<ms>","optimistic"],"click":["once","debounce.<ms>","throttle.<ms>","optimistic","outside","details"],"confirm":["prompt"],"dirty":["remove"],"disable-with":[],"focus":["once","debounce.<ms>","throttle.<ms>","window"],"input":["once","debounce.<ms>","throttle.<ms>","optimistic"],"intersect":["leave","once","half","full","margin.<px>","dwell.<ms>?"],"js":["click","dblclick","input","change","submit","focus","blur","keydown","keyup","mouseenter","mouseleave","once","outside","window","ctrl","meta","shift","alt","<key>"],"keydown":["once","debounce.<ms>","throttle.<ms>","optimistic","window","ctrl","meta","shift","alt","<key>","norepeat"],"keyup":["once","debounce.<ms>","throttle.<ms>","optimistic","window","ctrl","meta","shift","alt","<key>"],"loading":["delay.<ms>?","remove","except"],"model":["lazy","defer","debounce.<ms>","throttle.<ms>"],"poll":["<interval>","visible","keep-alive"],"show":[],"submit":["once","debounce.<ms>","throttle.<ms>","lock"],"text":[],"upload":[],"upload-progress":[]},"keys":["enter","escape","tab","space","up","down","left","right","backspace","delete","a","b","c","d","e","f","g","h","i","j","k","l","m","n","o","p","q","r","s","t","u","v","w","x","y","z","0","1","2","3","4","5","6","7","8","9"],"js":{"addClass":[2,2],"dispatch":[2,3],"focus":[1,1],"hide":[1,1],"removeAttr":[2,2],"removeClass":[2,2],"setAttr":[3,3],"show":[1,1],"toggle":[1,1],"toggleAttr":[2,4],"toggleClass":[2,2]}};
  // KEY_EVENT maps each brick:keydown.<key> name to the KeyboardEvent key
  // it matches.
  const KEY_EVENT = { enter: "Enter", escape: "Escape", tab: "Tab", space: " ", up: "ArrowUp", down: "ArrowDown", left: "ArrowLeft", right: "ArrowRight", backspace: "Backspace", delete: "Delete" };
  // DEBOUNCE_MS is how long brick:model waits after the last keystroke
  // before sending the value: long enough to batch a typed word into one
  // request, short enough to feel live. .debounce.<n>ms overrides it, and
  // it is also the wait of a bare .debounce or .throttle. brick-config
  // debounce changes the page-wide default (H4).
  const DEBOUNCE_MS = configInt("debounce", 250);
  // LOADING_DELAY_MS is how long brick:loading.delay waits before showing,
  // so a fast action never flashes its indicator; .delay.<n>ms overrides.
  const LOADING_DELAY_MS = 200;
  // DWELL_MS is how long brick:intersect.dwell waits when no duration
  // follows it (grammar.go DwellDefaultMS; TestDwellMatchesRuntime keeps
  // the two equal).
  const DWELL_MS = 1000;
  // TIMED_DEFAULT is the wait of a timed modifier written without its
  // duration; any other falls back to DEBOUNCE_MS.
  const TIMED_DEFAULT = { delay: LOADING_DELAY_MS, dwell: DWELL_MS };
  const UNIT_MS = { ms: 1, s: 1000, m: 60000 };
  const INTERVAL = /^(\d+)(ms|s|m)$/;
  const msOf = (s) => {
    const m = INTERVAL.exec(s || "");
    return m ? Number(m[1]) * UNIT_MS[m[2]] : NaN;
  };

  // pxOf reads a "<n>px" length (brick:intersect.margin.200px).
  const pxOf = (s) => { const m = /^(\d+)px$/.exec(s || ""); return m ? Number(m[1]) : NaN; };
  // ARG_OF parses each argument token of the grammar.
  const ARG_OF = { "<ms>": msOf, "<ms>?": msOf, "<px>": pxOf };

  // parseMods turns the dot segments after brick:<kind> into an object:
  // {once, defer, lazy, window, remove, visible: true; debounce, throttle,
  // delay, interval: ms; key: a KeyboardEvent key}. Unknown segments are
  // ignored here; the engine refuses them at compile time.
  function parseMods(kind, segs) {
    const o = {};
    const tokens = GRAMMAR.attrs[kind] || [];
    // timed maps a modifier that takes an argument to its argument token.
    const timed = new Map(tokens.filter((t) => t.includes(".")).map((t) => t.split(".")));
    const known = tokens.filter((t) => !t.includes(".") && !t.startsWith("<"));
    for (let i = 0; i < segs.length; i++) {
      const s = segs[i];
      if (timed.has(s)) {
        const n = (ARG_OF[timed.get(s)] || msOf)(segs[i + 1]);
        if (Number.isFinite(n)) { o[s] = n; i++; } else o[s] = s in TIMED_DEFAULT ? TIMED_DEFAULT[s] : DEBOUNCE_MS;
      } else if (tokens.includes("<key>") && GRAMMAR.keys.includes(s)) {
        o.key = KEY_EVENT[s] || s; // a letter or digit matches itself
      } else if (tokens.includes("<interval>") && INTERVAL.test(s)) {
        o.interval = msOf(s);
      } else if (known.includes(s)) {
        o[s] = true;
      }
    }
    return o;
  }

  // attrsOf lists el's brick:<kind> attributes, with or without
  // modifiers, as {name, value, mods}. HTML lowercases attribute names, and
  // every name in the grammar is lowercase.
  function attrsOf(el, kind) {
    const out = [];
    if (!el || !el.getAttributeNames) return out;
    const base = "brick:" + kind;
    for (const name of el.getAttributeNames()) {
      if (name !== base && !name.startsWith(base + ".")) continue;
      out.push({ name, value: el.getAttribute(name) || "", mods: parseMods(kind, name.split(".").slice(1)) });
    }
    return out;
  }

  // elementsWith lists [el, attr] for every brick:<kind> attribute under
  // root (and on root itself when self is set).
  function elementsWith(root, kind, self = false) {
    const out = [];
    const visit = (el) => attrsOf(el, kind).forEach((a) => out.push([el, a]));
    if (self) visit(root);
    root.querySelectorAll("*").forEach(visit);
    return out;
  }

  // closestWith walks up from start to the first element with a
  // brick:<kind> attribute that accept takes, and returns [el, attr].
  function closestWith(start, kind, accept = () => true) {
    for (let el = start && start.nodeType === 1 ? start : start && start.parentElement; el; el = el.parentElement) {
      for (const a of attrsOf(el, kind)) if (accept(a)) return [el, a];
    }
    return [null, null];
  }

  // ---- deferred values: root -> Map(path -> value). A
  // brick:model.defer field's value waits here and rides, as "sets", on
  // the next request of its root; it is cleared only by a 2xx answer.
  const deferred = new WeakMap();
  const deferredOf = (root) => {
    let d = deferred.get(root);
    if (!d) { d = new Map(); deferred.set(root, d); }
    return d;
  };
  // deferredHeld reports a bound field whose value is waiting unsent.
  function deferredHeld(el) {
    const m = modelOf(el);
    const root = m && el.closest(STATEFUL);
    const d = root && deferred.get(root);
    return Boolean(d && d.has(m.field));
  }
  const same = (a, b) => JSON.stringify(a) === JSON.stringify(b);
  // liveSent is root -> Map(path -> value) of the $sets sent since the
  // root was last rendered: what a reset button must undo on the server.
  const liveSent = new WeakMap();

  // ---- busy state. Every queued or in-flight job of a root is
  // in active; aria-busy, the root's data-loading action list and every
  // brick:loading are derived from that set, so the first of two
  // concurrent actions to finish does not clear them early. A quiet job (a
  // poll) shows only on a brick:loading that names its action.
  const active = new WeakMap(); // root -> Set(job)
  const formCount = new WeakMap(); // form -> in-flight submits (a form may be its own root)
  const disabledBy = new WeakMap(); // form -> buttons the runtime disabled
  const triggerCount = new WeakMap(); // trigger element -> its queued or in-flight jobs
  const loadingState = new WeakMap(); // brick:loading element -> {on, timer}
  const swapped = new WeakMap(); // brick:disable-with button -> {text, disabled}

  // showLoading turns one brick:loading on or off, honouring .delay (show
  // only once the action has run that long) and .remove (hide instead).
  function showLoading(el, mods, on) {
    let st = loadingState.get(el);
    if (!st) { st = { on: false, timer: 0 }; loadingState.set(el, st); }
    const apply = (v) => { if (mods.remove ? !v : v) el.removeAttribute("hidden"); else el.setAttribute("hidden", ""); };
    if (!on) { clearTimeout(st.timer); st.timer = 0; st.on = false; apply(false); return; }
    if (!mods.delay || st.on) { st.on = true; apply(true); return; }
    if (!st.timer) st.timer = setTimeout(() => { st.timer = 0; st.on = true; apply(true); }, mods.delay);
  }

  function showBusy(root) {
    const jobs = [...(active.get(root) || [])];
    const loud = jobs.filter((j) => !j.quiet);
    if (loud.length) root.setAttribute("aria-busy", "true"); else root.removeAttribute("aria-busy");
    const names = [...new Set(loud.flatMap((j) => j.names))];
    if (names.length) root.setAttribute("data-loading", names.join(" ")); else root.removeAttribute("data-loading");
    // A morph strips data-loading from triggers still waiting; put it back.
    loud.forEach((j) => triggersOf(root, j).forEach((t) => { if ((triggerCount.get(t) || 0) > 0) t.setAttribute("data-loading", ""); }));
    elementsWith(root, "loading").forEach(([el, a]) => {
      const targets = loadingTargets(a.value);
      let on;
      if (a.mods.except) on = loud.some((j) => !targets.some((t) => targetMatches(t, j)));
      else on = targets.length ? jobs.some((j) => targets.some((t) => targetMatches(t, j))) : loud.length > 0;
      showLoading(el, a.mods, on);
    });
  }

  // loadingTargets parses a brick:loading value (grammar.go
  // loadingTargets): tokens split on spaces outside parentheses and
  // strings, each a name or a call whose JSON arguments a job's must start
  // with. A token that does not parse is dropped with a warning, so one
  // typo filled in by {{ }} does not light every indicator.
  const targetCache = new Map(); // value -> parsed targets
  function loadingTargets(v) {
    v = String(v || "");
    if (targetCache.has(v)) return targetCache.get(v);
    const toks = [];
    let depth = 0, inStr = false, start = -1;
    for (let i = 0; i < v.length; i++) {
      const c = v[i];
      if (inStr && c === "\\") { i++; continue; }
      if (c === '"') inStr = !inStr;
      else if (!inStr && c === "(") depth++;
      else if (!inStr && c === ")") depth--;
      else if (!inStr && depth === 0 && /\s/.test(c)) { if (start >= 0) { toks.push(v.slice(start, i)); start = -1; } continue; }
      if (start < 0) start = i;
    }
    if (start >= 0) toks.push(v.slice(start));
    const out = [];
    for (const tok of toks) {
      try { const [name, args] = parseCall(tok); out.push({ name, args: tok.includes("(") ? args : null }); } catch (err) { console.warn("brick:loading: " + JSON.stringify(tok) + " is not an action or a call"); }
    }
    targetCache.set(v, out);
    return out;
  }

  // targetMatches reports whether job j is one target t names: by action
  // name, and for a call by its leading arguments. $set("field") also
  // matches a job that carries that field's held value (j.setFields), and
  // a path matches its own descendants ($set("user") lights for user.name).
  function targetMatches(t, j) {
    if (!j.names.includes(t.name)) return false;
    if (!t.args || !t.args.length) return true;
    if (t.name === "$set" && typeof t.args[0] === "string") {
      const under = (f) => typeof f === "string" && (f === t.args[0] || f.startsWith(t.args[0] + "."));
      const rest = t.args.slice(1);
      if (j.action === "$set" && under(j.args[0]) && rest.every((x, i) => same(x, j.args[i + 1]))) return true;
      return !rest.length && (j.setFields || []).some(under);
    }
    if (j.action !== t.name) return false;
    return t.args.every((x, i) => i < j.args.length && same(x, j.args[i]));
  }

  // swapLabel puts a brick:disable-with button into its busy look (disabled,
  // the attribute's text) or back. The old label comes back only if the
  // button still shows the busy text: a re-render that changed it wins.
  // A valueless brick:disable-with only disables: the label stays
  // (there is no text to swap in).
  function swapLabel(b, on) {
    const label = b.getAttribute("brick:disable-with");
    if (label === null) return;
    const prop = b.tagName === "INPUT" ? "value" : "textContent";
    if (on) {
      if (swapped.has(b)) return;
      swapped.set(b, { text: b[prop], disabled: b.hasAttribute("disabled") });
      if (label !== "") b[prop] = label;
      b.setAttribute("disabled", "");
      return;
    }
    const s = swapped.get(b);
    if (!s) return;
    swapped.delete(b);
    if (label !== "" && b[prop] === label) b[prop] = s.text;
    if (!s.disabled) b.removeAttribute("disabled");
  }

  // triggersOf is the element that started a job and its form, which carry
  // data-loading while it waits; the root is left to its action list.
  function triggersOf(root, job) {
    const t = job.trigger;
    if (!t) return [];
    return [...new Set([t, t.closest("form")])].filter((el) => el && el !== root);
  }

  function formBusy(form, delta) {
    const n = (formCount.get(form) || 0) + delta;
    formCount.set(form, n);
    if (delta > 0 && n === 1) {
      const buttons = [...form.querySelectorAll("button:not([disabled]), input[type=submit]:not([disabled])")];
      buttons.forEach((b) => { swapLabel(b, true); b.setAttribute("disabled", ""); });
      disabledBy.set(form, buttons);
    } else if (n === 0) {
      (disabledBy.get(form) || []).forEach((b) => { swapLabel(b, false); b.removeAttribute("disabled"); });
      disabledBy.delete(form);
    }
  }

  // ---- brick:submit.lock ----------------------------

  // lockCount is form -> its queued or in-flight locking submits; locked is
  // form -> what lockForm changed, so it can put back exactly that. Text
  // fields become readonly; choices get aria-disabled and a click guard,
  // never disabled, which would drop them from a re-read FormData (D11).
  const lockCount = new WeakMap();
  const locked = new WeakMap();
  const guarded = new WeakSet(); // choice controls the guard refuses
  const TEXTUAL = "input:not([type]), input[type=text], input[type=email], input[type=search], input[type=tel], input[type=url], input[type=password], input[type=number], input[type=date], input[type=datetime-local], input[type=month], input[type=time], input[type=week], textarea";
  const CHOICE = "select, input[type=checkbox], input[type=radio]";
  function lockForm(form, delta) {
    const n = (lockCount.get(form) || 0) + delta;
    lockCount.set(form, n);
    if (delta > 0 && n === 1) {
      const text = [...form.querySelectorAll(TEXTUAL)].filter((el) => !el.readOnly);
      const choice = [...form.querySelectorAll(CHOICE)].filter((el) => el.getAttribute("aria-disabled") !== "true");
      text.forEach((el) => { el.readOnly = true; });
      choice.forEach((el) => { el.setAttribute("aria-disabled", "true"); guarded.add(el); });
      locked.set(form, { text, choice });
    } else if (n === 0) {
      const l = locked.get(form);
      if (!l) return;
      l.text.forEach((el) => { el.readOnly = false; });
      l.choice.forEach((el) => { el.removeAttribute("aria-disabled"); guarded.delete(el); });
      locked.delete(form);
    }
  }
  // The guard runs in the capture phase, before any brick: listener, so a
  // locked checkbox does not toggle and a locked select does not open.
  const guard = (e) => { if (guarded.has(e.target) && (e.type !== "keydown" || e.key !== "Tab")) e.preventDefault(); };
  ["click", "mousedown", "keydown"].forEach((t) => document.addEventListener(t, guard, true));

  // track adds a job to (delta +1) or removes it from (-1) its root's
  // busy state.
  function track(root, job, delta) {
    let set = active.get(root);
    if (!set) { set = new Set(); active.set(root, set); }
    if (delta > 0) set.add(job); else set.delete(job);
    if (!job.quiet) {
      if (job.form) formBusy(job.form, delta);
      if (job.form && job.lock) lockForm(job.form, delta);
      triggersOf(root, job).forEach((t) => {
        const n = (triggerCount.get(t) || 0) + delta;
        triggerCount.set(t, n);
        if (n > 0) t.setAttribute("data-loading", ""); else t.removeAttribute("data-loading");
        if (delta > 0 && n === 1) swapLabel(t, true);
        if (n === 0) swapLabel(t, false);
      });
    }
    showBusy(root);
  }

  // ---- the queue: one action in flight per stateful root.
  // Each response carries the next handle, so an action reads the root's
  // handle when it is sent, after the one before it was morphed in. Two
  // waiting $sets of one field collapse into the later one. meta.chain is
  // the event chain a listener job was started by; with
  // meta.collapse an identical waiting job (same action and arguments)
  // absorbs this one, so a burst of one event runs each listener once.
  // meta.trigger is the element that started the job (it carries
  // data-loading), meta.quiet marks a poll, and meta.sets are field values
  // sent with the job besides the deferred ones (a reset's). meta.upload is
  // a file the job sends ({field, file, el}).
  const queues = new WeakMap(); // root -> {tail, waiting}
  function send(root, action, args, form, meta = {}) {
    let q = queues.get(root);
    if (!q) { q = { tail: Promise.resolve(), waiting: [], inFlight: 0 }; queues.set(root, q); }
    if (meta.collapse) {
      const key = JSON.stringify(args);
      const twin = q.waiting.find((j) => !j.skip && j.action === action && JSON.stringify(j.args) === key);
      if (twin) return twin.done;
    }
    const job = { action, args, form, skip: false, chain: meta.chain || [], trigger: meta.trigger || null, quiet: Boolean(meta.quiet), names: [action], sets: meta.sets || null, upload: meta.upload || null, lock: Boolean(meta.lock), onFail: meta.onFail || null, reload: meta.reload || null };
    if (action === "$set") {
      q.waiting.forEach((j) => { if (j.action === "$set" && j.args[0] === args[0]) j.skip = true; });
      // A live value is newer than a deferred one of the same field.
      const d = deferred.get(root);
      if (d) d.delete(args[0]);
    }
    q.waiting.push(job);
    track(root, job, +1);
    const go = async () => {
      q.waiting.splice(q.waiting.indexOf(job), 1);
      try {
        // A skipped upload still runs, to report the cancel and release
        // its count; performUpload sends nothing for it.
        if (!job.skip || job.upload) await perform(root, job);
        // brick.action() resolves with the server's brick.reply.
        return job.reply;
      } catch (err) {
        if (job.onFail) job.onFail();
        // root and status let a listener act on the component that failed
        // (brick.reloadComponent(d.root) after a 500).
        brick.emit("action-error", { action, message: String(err && err.message || err), root, id: root.getAttribute("data-brick"), status: (err && err.status) || 0 });
      } finally {
        track(root, job, -1);
        q.inFlight--;
      }
    };
    // An idle root sends at once (the request starts in this task, as a
    // click's always did); a busy one waits its turn.
    const done = q.inFlight++ === 0 ? go() : q.tail.then(go);
    job.done = done;
    q.tail = done;
    return done;
  }

  // ENVELOPE is the action response type that carries effects beside the
  // HTML. The runtime asks for it and still accepts plain
  // text/html, so an older server, or a test that answers HTML, works.
  const ENVELOPE = "application/vnd.brick.action+json";
  const ENVELOPE_VERSION = 1;
  // STREAM is the answer of an action that streams text: one
  // JSON line per chunk ({"stream": {ref, text, replace}}), then the
  // envelope (or {"v", "error"}) as the last line.
  const STREAM = "application/vnd.brick.stream+ndjson";
  const warnedRefs = new Set();

  // applyChunk puts streamed text into the root's brick:ref element as
  // text, never as HTML (D18). An unknown ref is warned about once and its
  // text dropped; the final render still lands.
  function applyChunk(root, c) {
    if (!c || typeof c.ref !== "string") return;
    const el = refIn(root, c.ref);
    if (!el) {
      if (!warnedRefs.has(c.ref)) { warnedRefs.add(c.ref); console.warn("brick: stream: no brick:ref=" + JSON.stringify(c.ref) + " in this component; its text is dropped"); }
      return;
    }
    const text = typeof c.text === "string" ? c.text : "";
    if (c.replace) el.textContent = text; else el.appendChild(document.createTextNode(text));
  }

  // readStream applies each chunk line as it arrives and returns the last
  // line (the envelope or the error). Without a readable body (an older
  // browser, a test's stub) the lines are applied once the answer is in.
  async function readStream(root, res) {
    let last = null;
    const handle = (line) => {
      if (!line.trim()) return;
      let m;
      try { m = JSON.parse(line); } catch { throw new Error("brick: malformed stream line"); }
      if (m && m.stream) applyChunk(root, m.stream); else last = m;
    };
    if (res.body && typeof res.body.getReader === "function" && typeof TextDecoder !== "undefined") {
      const reader = res.body.getReader();
      const dec = new TextDecoder();
      let buf = "";
      for (;;) {
        const { value, done } = await reader.read();
        if (done) break;
        buf += dec.decode(value, { stream: true });
        let i;
        while ((i = buf.indexOf("\n")) >= 0) { handle(buf.slice(0, i)); buf = buf.slice(i + 1); }
      }
      buf += dec.decode();
      if (buf) handle(buf);
    } else {
      (await res.text()).split("\n").forEach(handle);
    }
    return last;
  }

  // post sends one action with the root's current handle, morphs the
  // answer in and applies its effects. It returns GONE instead of throwing
  // on 410; any other failure throws. chain is the event chain that
  // started this action (empty for a user's own action).
  // extra are field values sent besides the root's deferred ones (a
  // recovery's or a reset's); job, when given, is the queued job, whose
  // loading names gain "$set" when the request carries values.
  // sink, when given, receives the answer's reply (the job by default).
  async function post(root, action, args, chain = [], extra = null, job = null, sink = job) {
    // The deferred map is read now, when the job is performed, so a value
    // typed while an earlier action was in flight still goes out.
    const d = deferred.get(root);
    const held = d ? new Map(d) : new Map();
    const sets = new Map([...held, ...(extra || [])]);
    if (sets.size && job) {
      job.setFields = [...sets.keys()];
      if (!job.names.includes("$set")) job.names.push("$set");
      showBusy(root);
    }
    if (action === "$set") { let l = liveSent.get(root); if (!l) { l = new Map(); liveSent.set(root, l); } l.set(args[0], args[1]); }
    const base = { "Content-Type": "application/json", "Brick-Action": "1", Accept: ENVELOPE + ", " + STREAM + ", text/html;q=0.5" };
    if (chain.length) base["Brick-Event-Chain"] = chain.join(">");
    let res, type;
    for (let retry = 0; ; retry++) {
      // Each attempt gets fresh headers and runs the request hooks again,
      // so a hook sees retry (0 on the first send) and can cancel a retry.
      const headers = { ...base };
      if (retry) headers["Brick-Retry"] = String(retry);
      // With no hooks the first request still starts in this task (no await).
      const lagging = lag();
      if (lagging) await lagging;
      debugLog("→", action, args, sets.size ? Object.fromEntries(sets) : "", retry ? "retry " + retry : "");
      if (hooks.request.length && await requestHooks({ root, action, args, trigger: job ? job.trigger : null, retry }, headers)) {
        brick.emit("action-cancelled", { id: root.getAttribute("data-brick"), action });
        return CANCELLED;
      }
      res = await fetch(actionURL(), {
        method: "POST",
        credentials: CREDENTIALS,
        headers,
        body: JSON.stringify(Object.assign({ handle: root.getAttribute("data-brick-state"), token: root.getAttribute("data-brick-token"), action, args }, sets.size ? { sets: [...sets] } : {}, childrenOf(root))),
      });
      type = (res.headers && typeof res.headers.get === "function" && res.headers.get("Content-Type")) || "";
      // A stream that has begun is never sent again: its text may be on
      // the page already.
      if (!RETRYABLE.has(res.status) || type.includes(STREAM) || retry >= RETRIES) break;
      const wait = retryWait(res);
      if (wait > RETRY_MAX_WAIT_MS) break;
      brick.emit("action-retry", { id: root.getAttribute("data-brick"), action, status: res.status, retry: retry + 1, wait });
      await new Promise((r) => setTimeout(r, wait));
    }
    if (res.status === GONE) return GONE;
    // A view transition wraps a loud action's morph on an opted-in root;
    // polls, $set and a recovery's replay (no job) never animate (D3).
    const animate = Boolean(job && !job.quiet && action !== "$set");
    if (type.includes(STREAM)) {
      // Chunks land as they arrive; the last line is the usual envelope,
      // morphed in as always, or the error the action ended with (the
      // streamed text stays until the next render).
      const last = await readStream(root, res);
      if (!last || last.error) throw new Error((last && last.error && last.error.message) || "brick: the stream ended without an answer");
      const env = await land(root, true, res.statusText, ENVELOPE, JSON.stringify(last), chain, d, held, { action, status: res.status, animate });
      if (sink) sink.reply = env ? env.reply : undefined;
      return res.status;
    }
    const env = await land(root, res.ok, res.statusText, type, await res.text(), chain, d, held, { action, status: res.status, animate });
    if (sink) sink.reply = env ? env.reply : undefined;
    return res.status;
  }

  // land applies one answer of the action pipeline (an action's or an
  // upload's): the envelope or bare HTML is morphed in and its effects run;
  // a failure throws, after the effects a middleware attached to it. d and
  // held are the root's deferred map and the values this request carried,
  // cleared once the server has them. It returns the envelope, or null.
  async function land(root, ok, statusText, type, text, chain, d, held, info = {}) {
    let html = text;
    let env = null;
    let effects = null;
    if (type.includes(ENVELOPE)) {
      try { env = JSON.parse(text); } catch { throw new Error("brick: malformed action response"); }
      if (!env || env.v !== ENVELOPE_VERSION) throw new Error("brick: unsupported action response version " + JSON.stringify(env && env.v));
      html = typeof env.html === "string" ? env.html : "";
      effects = Array.isArray(env.effects) ? env.effects : [];
    }
    debugLog("←", info.action || "", info.status || 0, env || html);
    if (hooks.response.length) await notify("response", { root, action: info.action || "", status: info.status || 0, envelope: env });
    if (!ok) {
      // An error answer carries effects only from a middleware that turns
      // the error into a flash (actionmw.FlashErrors); apply them, then
      // fail the action as usual so "action-error" still fires. The error
      // carries the status for that event.
      const fail = (msg) => Object.assign(new Error(msg), { status: info.status || 0 });
      if (!effects) throw fail(text.trim() || statusText);
      applyEffects(root, effects, chain);
      const flash = effects.find((ef) => ef && ef.kind === "flash");
      throw fail((flash && flash.message) || statusText);
    }
    // The server has the values now; one changed since it was sent stays.
    if (d) held.forEach((v, f) => { if (d.has(f) && same(d.get(f), v)) d.delete(f); });
    // A renderless answer has no html: the root only takes the
    // new handle (a sealed store's changes on every save) and its effects.
    if (env && typeof env.html !== "string") {
      if (typeof env.handle === "string" && env.handle) root.setAttribute("data-brick-state", env.handle);
      if (typeof env.token === "string" && env.token) root.setAttribute("data-brick-token", env.token);
      if (effects) applyEffects(root, effects, chain);
      return env;
    }
    const tpl = document.createElement("template");
    tpl.innerHTML = html.trim();
    const next = tpl.content.firstElementChild;
    // data-brick-up is written by the parent's call site, so
    // the child's own re-render does not know it; the root keeps it.
    const up = root.getAttribute("data-brick-up");
    if (next && up !== null && !next.hasAttribute("data-brick-up")) next.setAttribute("data-brick-up", up);
    // A modelable child (modelable.go) keeps the parent field it is bound
    // to across its own re-render, as it keeps data-brick-up.
    const bound = root.getAttribute(MODEL_FIELD);
    if (next && bound !== null && !next.hasAttribute(MODEL_FIELD)) next.setAttribute(MODEL_FIELD, bound);
    const modelBefore = root.getAttribute(MODEL_VALUE);
    if (next) {
      const work = () => { morphElement(root, next); bind(root); showBusy(root); };
      // The action's own transition effect (v22) overrides the root's
      // opt-in and the quiet-job rule; the last one recorded wins.
      const choice = transitionChoice(effects);
      if (info.animate || choice !== undefined) await transitioned(root, work, choice); else work();
      if (hooks.morphed.length) notify("morphed", { root });
    }
    if (effects) applyEffects(root, effects, chain, next);
    modelableChanged(root, modelBefore);
    return env;
  }

  // childrenOf lists the stateful roots directly inside root (no other
  // stateful root between), so the server keeps them instead of rendering
  // them fresh. Each carries its own handle and token, read
  // at send time: a child's own action may have just replaced them.
  function childrenOf(root) {
    const children = {};
    root.querySelectorAll(STATEFUL).forEach((el) => {
      const id = el.getAttribute("data-brick");
      if (id && el.parentElement.closest(STATEFUL) === root) children[id] = { handle: el.getAttribute("data-brick-state"), token: el.getAttribute("data-brick-token") };
    });
    return Object.keys(children).length ? { children } : {};
  }

  async function perform(root, job) {
    if (job.upload) { await performUpload(root, job); return; }
    if (job.reload) {
      try { await refetch(root); job.reload.resolve(); } catch (err) { job.reload.reject(err); }
      return;
    }
    const status = await post(root, job.action, job.args, job.chain, job.sets, job);
    if (status === GONE) await recover(root, job);
    else if (status === CANCELLED && job.onFail) job.onFail();
  }

  // ---- uploads ---------------------------------------------------

  // uploadURL is the upload endpoint, beside the action one.
  const uploadURL = () => actionURL().replace(/action$/, "upload");
  // uploading is root -> Set({field, xhr, cancelled}) of the requests in
  // flight, for $cancelUpload and data-uploading.
  const uploading = new WeakMap();
  // UploadCancelled marks a request $cancelUpload aborted.
  class UploadCancelled extends Error {}

  // limitsOf reads data-brick-uploads: field -> {accept, max_size,
  // max_files, many, presign}, written by the engine.
  function limitsOf(root) {
    try { return JSON.parse(root.getAttribute("data-brick-uploads") || "{}"); } catch { return {}; }
  }
  // accepts mirrors the server's accept check on the browser's claimed
  // type: advisory only, the server sniffs the bytes.
  const accepts = (list, type) => !list || !list.length || list.some((a) => a === type || a === "*/*" || (a.endsWith("/*") && type.startsWith(a.slice(0, -1))));
  // queuedFiles counts each field's files queued or in flight, for
  // data-uploading.
  const queuedFiles = new WeakMap(); // root -> Map(field -> queued count)

  // xhr sends one request with upload progress (fetch has none) and
  // resolves {status, statusText, type, text}. rec.xhr is set so
  // $cancelUpload can abort it; an abort rejects with UploadCancelled.
  function xhrSend(method, url, headers, body, onProgress, rec) {
    return new Promise((resolve, reject) => {
      if (rec.cancelled) { reject(new UploadCancelled("brick: upload cancelled")); return; }
      const x = new XMLHttpRequest();
      rec.xhr = x;
      // Same-origin requests carry the session cookie by default; a
      // presigned storage URL must not get it, so withCredentials stays off
      // except for a moved action server's own upload endpoint (H13).
      x.open(method, url);
      if (crossActions && url === uploadURL()) x.withCredentials = true;
      Object.entries(headers).forEach(([k, v]) => x.setRequestHeader(k, v));
      if (onProgress && x.upload) x.upload.onprogress = (e) => { if (e.lengthComputable) onProgress(e.loaded, e.total); };
      x.onload = () => resolve({ status: x.status, statusText: x.statusText, type: x.getResponseHeader("Content-Type") || "", text: x.responseText || "" });
      x.onerror = () => reject(new Error("brick: upload failed (network)"));
      x.onabort = () => reject(new UploadCancelled("brick: upload cancelled"));
      x.send(body);
    });
  }

  // progressTo sets every <progress brick:upload-progress="field"> of root.
  function progressTo(root, field, loaded, total) {
    elementsWith(root, "upload-progress").forEach(([el, a]) => {
      if (a.value.trim() !== field) return;
      el.max = total || 1;
      el.value = loaded;
    });
  }

  // markUploading sets data-uploading on the field's inputs (re-found
  // after each morph) while any of its files is queued or in flight.
  function markUploading(root, field) {
    const busy = ((queuedFiles.get(root) || new Map()).get(field) || 0) > 0;
    elementsWith(root, "upload").forEach(([el, a]) => {
      if (a.value.trim() !== field) return;
      if (busy) el.setAttribute("data-uploading", ""); else el.removeAttribute("data-uploading");
    });
  }

  // performUpload sends one queued file: proxied (PUT the bytes to the
  // upload endpoint) or, when the sink presigns, POST for a target, PUT
  // straight to storage, then $uploaded. Progress, done and error go to
  // brick.on. A 410 is not recovered: the file is not held after the
  // request, so the UI is told ("gone") and may ask again.
  async function performUpload(root, job) {
    const { field, file } = job.upload;
    const lim = limitsOf(root)[field] || {};
    const detail = { field, name: file.name, size: file.size };
    const rec = { field, xhr: null, cancelled: job.skip };
    let set = uploading.get(root);
    if (!set) { set = new Set(); uploading.set(root, set); }
    set.add(rec);
    const progress = (loaded, total) => {
      progressTo(root, field, loaded, total);
      brick.emit("upload-progress", Object.assign({ loaded, total }, detail));
    };
    try {
      if (rec.cancelled) throw new UploadCancelled("brick: upload cancelled");
      const d = deferred.get(root);
      const held = d ? new Map(d) : new Map();
      const kids = childrenOf(root).children;
      const headers = {
        "Brick-Upload": field,
        "Brick-Handle": root.getAttribute("data-brick-state"),
        "Brick-Token": root.getAttribute("data-brick-token"),
        "Brick-Name": encodeURIComponent(file.name || ""),
        "Content-Type": file.type || "application/octet-stream",
        Accept: ENVELOPE,
      };
      if (held.size) headers["Brick-Sets"] = encodeURIComponent(JSON.stringify([...held]));
      if (kids) headers["Brick-Children"] = encodeURIComponent(JSON.stringify(kids));
      // The request hooks reach uploads too, so a header a site adds to
      // actions (a trace id, a bearer token) is on the upload as well.
      if (hooks.request.length && await requestHooks({ root, action: "$upload", args: [field], trigger: job.upload.el || null }, headers)) throw new UploadCancelled("brick: upload cancelled by a request hook");
      progress(0, file.size);
      let res;
      if (lim.presign) {
        headers["Brick-Size"] = String(file.size);
        res = await xhrSend("POST", uploadURL(), headers, null, null, rec);
        if (res.status === GONE) throw Object.assign(new Error("gone"), { gone: true });
        const env = await land(root, res.status >= 200 && res.status < 300, res.statusText, res.type, res.text, [], d, held);
        const pre = env && env.upload;
        if (!pre || !pre.url) throw new Error("brick: the server sent no upload target");
        const put = await xhrSend(pre.method || "PUT", pre.url, pre.headers || {}, file, progress, rec);
        if (put.status < 200 || put.status >= 300) throw new Error("brick: storage refused the upload (" + put.status + ")");
        if (await post(root, "$uploaded", [field, pre.id]) === GONE) throw Object.assign(new Error("gone"), { gone: true });
      } else {
        res = await xhrSend("PUT", uploadURL(), headers, file, progress, rec);
        if (res.status === GONE) throw Object.assign(new Error("gone"), { gone: true });
        await land(root, res.status >= 200 && res.status < 300, res.statusText, res.type, res.text, [], d, held);
      }
      progress(file.size, file.size);
      brick.emit("upload-done", detail);
    } catch (err) {
      const why = err instanceof UploadCancelled ? { cancelled: true } : err && err.gone ? { gone: true } : {};
      brick.emit("upload-error", Object.assign({ message: String(err && err.message || err) }, detail, why));
    } finally {
      set.delete(rec);
      const q = queuedFiles.get(root);
      if (q) q.set(field, Math.max(0, (q.get(field) || 0) - 1));
      markUploading(root, field);
    }
  }

  // upload queues each file for field on el's stateful root, after the
  // advisory checks (count, declared accept and max_size); a refused file
  // emits "upload-error" and is not sent. Each file is its own job, so the
  // queue keeps uploads in order with the root's other actions.
  function upload(el, field, files) {
    const root = el && el.closest(STATEFUL);
    if (!root) return Promise.reject(new Error("brick.upload: " + field + ": not inside a stateful component"));
    const lim = limitsOf(root)[field];
    if (!lim) return Promise.reject(new Error("brick.upload: " + field + " is not an upload field"));
    let q = queuedFiles.get(root);
    if (!q) { q = new Map(); queuedFiles.set(root, q); }
    const list = [...(files || [])];
    const room = lim.many ? lim.max_files : 1;
    const jobs = [];
    list.forEach((file, i) => {
      const detail = { field, name: file.name, size: file.size };
      let message = "";
      if (lim.many && i >= room) message = field + " holds at most " + room + " files";
      else if (!lim.many && i > 0) message = field + " takes one file";
      else if (lim.max_size && file.size > lim.max_size) message = "the file is too large";
      else if (file.type && !accepts(lim.accept, file.type)) message = "this type of file is not accepted";
      if (message) { brick.emit("upload-error", Object.assign({ message, refused: true }, detail)); return; }
      q.set(field, (q.get(field) || 0) + 1);
      jobs.push(send(root, "$upload", [field], undefined, { trigger: el, upload: { field, file, el } }));
    });
    markUploading(root, field);
    return Promise.all(jobs);
  }

  // cancelUpload ($cancelUpload, client-only) aborts field's request in
  // flight and drops its files still waiting in the queue.
  function cancelUpload(root, field) {
    (uploading.get(root) || new Set()).forEach((rec) => { if (rec.field === field) { rec.cancelled = true; if (rec.xhr) rec.xhr.abort(); } });
    const q = queues.get(root);
    if (q) q.waiting.forEach((j) => { if (j.upload && j.upload.field === field) j.skip = true; });
  }

  // A file input with brick:upload sends its files on change, then clears
  // itself so choosing the same file again fires change again.
  document.addEventListener("change", (e) => {
    const el = e.target;
    const a = attrsOf(el, "upload")[0];
    if (!a || el.type !== "file" || !el.closest(STATEFUL)) return;
    const files = [...el.files];
    if (!files.length) return;
    upload(el, a.value.trim(), files).catch((err) => console.error(err));
    try { el.value = ""; } catch { /* some browsers refuse; harmless */ }
  });

  // ---- effects -------------------------------------------------

  // TERMINAL kinds leave the page, so they run after every other effect,
  // and while one is present no listener actions are enqueued (4.4).
  const TERMINAL = new Set(["redirect", "navigate", "submit"]);
  // FLASH_KEY is the one sessionStorage entry a flash uses to survive a
  // full page load; FLASH_TTL_MS is how old it may be when the next page
  // reads it (a back button an hour later must not replay it).
  const FLASH_KEY = "brick:flash";
  const FLASH_TTL_MS = 30000;
  // FLASH_SHOW_MS is how long the default flash display (used only when
  // nothing listens to "flash") keeps a message on screen.
  const FLASH_SHOW_MS = 5000;
  const effectKinds = new Map(); // kind -> fn(effect, root, ctx)
  const listening = new Map(); // brick.on name -> handler count

  // safeURL re-checks an effect's URL in the browser (the server checked it
  // when it was recorded): http(s) only, and same-origin when asked.
  function safeURL(raw, sameOrigin) {
    let u;
    try { u = new URL(String(raw), location.href); } catch { u = null; }
    if (!u || !/^https?:$/.test(u.protocol) || (sameOrigin && u.origin !== location.origin)) {
      console.warn("brick: effect URL refused: " + JSON.stringify(raw));
      return null;
    }
    return u;
  }

  // runEffect hands one effect to its registered handler; an unregistered
  // kind is reported and emitted as "effect" for a late or debugging
  // handler to see.
  function runEffect(ef, root, ctx) {
    const fn = effectKinds.get(ef.kind);
    if (!fn) {
      console.warn("brick: no handler for effect " + JSON.stringify(ef.kind));
      brick.emit("effect", ef);
      return;
    }
    try { fn(ef, root, ctx); } catch (err) { console.error(err); }
  }

  // applyEffects runs an action's effects after its morph: every
  // non-terminal one in recorded order, then the last terminal one. When
  // the page is leaving, flashes are carried to the next page instead of
  // shown, and dispatches reach brick.on but enqueue no listener.
  function applyEffects(root, effects, chain, next) {
    let terminal = null;
    for (const ef of effects) if (ef && TERMINAL.has(ef.kind)) terminal = ef;
    const ctx = { chain, leaving: Boolean(terminal), next: next || null };
    const carried = [];
    for (const ef of effects) {
      if (!ef || typeof ef.kind !== "string" || TERMINAL.has(ef.kind)) continue;
      if (ctx.leaving && ef.kind === "flash") { carried.push(ef); continue; }
      // A clear in the same answer drops the flashes carried before it.
      if (ctx.leaving && ef.kind === "flash-clear") {
        const level = typeof ef.level === "string" ? ef.level : "";
        for (let i = carried.length - 1; i >= 0; i--) if (!level || (carried[i].level || "info") === level) carried.splice(i, 1);
      }
      runEffect(ef, root, ctx);
    }
    if (!terminal) return;
    carryFlashes(carried, terminal.kind === "navigate" && typeof brick.navigate === "function");
    runEffect(terminal, root, ctx);
  }

  // carryFlashes keeps flashes for the next page: in one sessionStorage
  // entry (a full load reads it at start), and, across an in-place
  // navigation, in memory too, shown once "navigated" fires. A storage
  // that throws (a private window) only loses a flash across a full load.
  function carryFlashes(flashes, inPlace) {
    if (!flashes.length) return;
    try { sessionStorage.setItem(FLASH_KEY, JSON.stringify({ at: Date.now(), flashes })); } catch { /* storage blocked */ }
    if (!inPlace) return;
    const off = brick.on("navigated", () => {
      off();
      try { sessionStorage.removeItem(FLASH_KEY); } catch { /* storage blocked */ }
      flashes.forEach((f) => runEffect(f, null, { chain: [], leaving: false }));
    });
  }

  // takeCarriedFlashes reads and deletes the flash entry a previous page
  // left, returning its flashes when it is fresh enough.
  function takeCarriedFlashes() {
    let saved = null;
    try {
      saved = JSON.parse(sessionStorage.getItem(FLASH_KEY) || "null");
      sessionStorage.removeItem(FLASH_KEY);
    } catch { return []; }
    if (!saved || !Array.isArray(saved.flashes) || !(Date.now() - saved.at < FLASH_TTL_MS)) return [];
    return saved.flashes;
  }

  // defaultFlash is the minimal display used when nothing listens to
  // "flash": a polite live region with the message, removed after
  // FLASH_SHOW_MS. A kit Toaster that listens to "flash" replaces it, and
  // brick.effect("flash", fn) replaces the whole handler.
  function defaultFlash(f) {
    let box = document.getElementById("brick-flash");
    if (!box) {
      box = document.createElement("div");
      box.id = "brick-flash";
      box.setAttribute("role", "status");
      box.setAttribute("aria-live", "polite");
      box.setAttribute("style", "position:fixed;right:1rem;bottom:1rem;z-index:2147483647;display:grid;gap:.5rem;font:14px system-ui,sans-serif");
      document.body.appendChild(box);
    }
    const item = document.createElement("div");
    item.setAttribute("data-level", f.level);
    item.setAttribute("style", "padding:.5rem .75rem;border-radius:.375rem;background:#111;color:#fff;box-shadow:0 2px 8px rgba(0,0,0,.25)");
    item.textContent = f.message;
    box.appendChild(item);
    setTimeout(() => item.remove(), FLASH_SHOW_MS);
  }

  // The engine kinds, registered through the same registry a site uses,
  // so brick.effect(kind, fn) can replace any of them.
  // A transition effect was applied before the morph (land); it has
  // nothing left to do afterwards.
  effectKinds.set("transition", () => {});
  effectKinds.set("title", (ef) => { if (typeof ef.text === "string") document.title = ef.text; });
  effectKinds.set("url", (ef) => {
    const u = safeURL(ef.url, true);
    if (u) history[ef.replace ? "replaceState" : "pushState"]({ brick: true }, "", u.href);
  });
  effectKinds.set("flash", (ef) => {
    const f = { message: String(ef.message || ""), level: ef.level || "info" };
    brick.emit("flash", f);
    if (!listening.get("flash")) defaultFlash(f);
  });
  // "flash-clear" dismisses the flashes on screen: every level, or only
  // ef.level. It is emitted as "flash-clear" ({level}, ""
  // for all) so a Toaster dismisses its own toasts, and it clears the
  // default display too, which costs nothing when that display is unused.
  effectKinds.set("flash-clear", (ef) => {
    const level = typeof ef.level === "string" ? ef.level : "";
    brick.emit("flash-clear", { level });
    const box = document.getElementById("brick-flash");
    if (box) [...box.children].forEach((item) => { if (!level || item.getAttribute("data-level") === level) item.remove(); });
  });
  effectKinds.set("download", (ef) => {
    const u = safeURL(ef.url, false);
    if (!u) return;
    const a = document.createElement("a");
    a.href = u.href;
    a.setAttribute("download", ef.filename || "");
    a.hidden = true;
    document.body.appendChild(a);
    a.click();
    a.remove();
  });
  effectKinds.set("dispatch", (ef, root, ctx) => {
    deliver(ef.name, ef.data, root, ef.to || "", ctx);
  });
  effectKinds.set("redirect", (ef) => {
    const u = safeURL(ef.url, false);
    if (u) brick._assign(u.href);
  });
  effectKinds.set("navigate", (ef) => {
    const u = safeURL(ef.url, false);
    if (!u) return;
    if (u.origin === location.origin && typeof brick.navigate === "function") brick.navigate(u.href);
    else brick._assign(u.href);
  });
  // submit posts a brick:ref form natively once the morph is in: form.submit() fires no submit event, so neither brick:submit
  // nor nav.js intercepts it, and the browser leaves for the form's action
  // as a classic POST would (a login that sets a cookie).
  effectKinds.set("submit", (ef, root) => {
    const form = refIn(root, ef.ref);
    if (!form || form.tagName !== "FORM") { console.warn("brick: submit: no brick:ref=" + JSON.stringify(ef.ref) + " form in this component"); return; }
    brick._submit(form);
  });

  // ---- row streams ------------------------------------------

  // The rows effect edits the children of a brick:ref list, matched by
  // brick:key, without the rows living in the component's state. Each row
  // is server-rendered markup (a component's render, escaped by
  // html/template), parsed as HTML; its first element is the row. The
  // list should carry brick:ignore so the next morph leaves it alone.
  const rowKey = (n) => (n.nodeType === 1 ? n.getAttribute("brick:key") : null);
  const rowsByKey = (list) => {
    const m = new Map();
    for (const c of list.children) { const k = rowKey(c); if (k !== null && !m.has(k) && !isLeaving(c)) m.set(k, c); }
    return m;
  };
  function rowNode(row) {
    const tpl = document.createElement("template");
    tpl.innerHTML = String(row.html || "").trim();
    const el = tpl.content.firstElementChild;
    if (el) el.setAttribute("brick:key", String(row.key));
    return el;
  }
  effectKinds.set("rows", (ef, root) => {
    const list = refIn(root, ef.ref);
    if (!list) {
      if (!warnedRefs.has(ef.ref)) { warnedRefs.add(ef.ref); console.warn("brick: rows: no brick:ref=" + JSON.stringify(ef.ref) + " in this component; the rows are dropped"); }
      return;
    }
    if (ef.op === "delete") {
      const have = rowsByKey(list);
      for (const k of Array.isArray(ef.keys) ? ef.keys : []) { const el = have.get(String(k)); if (el) removeAnimated(el); }
      return;
    }
    if (ef.op === "reset") list.replaceChildren();
    const top = ef.at === "top";
    const have = rowsByKey(list);
    const fresh = document.createDocumentFragment();
    for (const row of Array.isArray(ef.rows) ? ef.rows : []) {
      const el = rowNode(row);
      if (!el) continue;
      const old = have.get(String(row.key));
      // A key already in the list is updated where it stands, keeping
      // its position, focus and mounted client code.
      if (old) morphElement(old, el); else fresh.appendChild(el);
    }
    if (top) list.insertBefore(fresh, list.firstChild); else list.appendChild(fresh);
    // limit trims the end the rows were not added to: newest-first lists
    // drop their oldest rows at the bottom, chat logs at the top.
    const limit = Number.isInteger(ef.limit) && ef.limit > 0 ? ef.limit : 0;
    // Rows still leaving are not counted, or the loop would trim them
    // forever while they play.
    const staying = () => [...list.children].filter((c) => !isLeaving(c));
    for (let rows = staying(); limit && rows.length > limit; rows = staying()) removeAnimated(top ? rows[rows.length - 1] : rows[0]);
    if (root) bind(root);
  });

  // ---- regions ---------------------------------------------

  // The region effect applies one brick:ref element of the action's fresh
  // render (ctx.next) to the live one, which carries brick:ignore so the
  // morph itself left it alone: replace morphs the live children into the
  // fresh ones, append and prepend add the fresh children after or before
  // what earlier answers put there. A region the morph has just inserted
  // whole is no longer in ctx.next and already holds the fresh children.
  const warnedRegions = new Set();
  effectKinds.set("region", (ef, root, ctx) => {
    const warn = (msg) => { const k = ef.ref + " " + msg; if (!warnedRegions.has(k)) { warnedRegions.add(k); console.warn("brick: region " + JSON.stringify(ef.ref) + ": " + msg); } };
    const live = refIn(root, ef.ref);
    if (!live) return warn("no brick:ref with this name in this component; nothing changes");
    const fresh = ctx && ctx.next ? refIn(ctx.next, ef.ref) : null;
    if (!fresh) return;
    if (!live.hasAttribute(IGNORE)) warn("add brick:ignore to the element, or every morph replaces what the region holds");
    if (ef.op === "append") live.append(...fresh.childNodes);
    else if (ef.op === "prepend") live.prepend(...fresh.childNodes);
    else morph(live, fresh);
    if (root) bind(root);
  });
  // ---- URL-synced state -------------------------------------

  // The query effect merges a component's parameters into location.search,
  // leaving every parameter it does not own untouched; an answer that
  // changes nothing (a Back restore echoing the URL) adds no entry.
  effectKinds.set("query", (ef) => {
    const data = ef.data || {};
    const u = new URL(location.href);
    for (const p of Array.isArray(data.del) ? data.del : []) u.searchParams.delete(String(p));
    for (const [p, v] of Object.entries(data.set || {})) {
      u.searchParams.delete(p);
      for (const x of Array.isArray(v) ? v : [v]) u.searchParams.append(p, String(x));
    }
    if (u.href === location.href) return;
    history[ef.replace ? "replaceState" : "pushState"]({ brick: true, key: Date.now().toString(36) + ".q" }, "", u.href);
    seenURL = location.href;
    if (typeof brick._shown === "function") brick._shown(location.href);
  });

  // queryOf reads a root's data-brick-query: param -> [field, declared].
  const queryOf = (root) => {
    try { return JSON.parse(root.getAttribute("data-brick-query") || "{}") || {}; } catch { return {}; }
  };
  // seenURL is the URL the runtime last saw, so a popstate knows which
  // parameters changed.
  let seenURL = typeof location === "undefined" ? "" : location.href;
  bus.addEventListener("navigated", () => { seenURL = location.href; });
  // restoreQuery handles a Back/Forward that changed only the search, and
  // only parameters stateful roots on the page own: each owner gets a
  // $refresh carrying its fields from the URL (a missing parameter sends
  // the declared value), and nothing is fetched (D13). It returns false
  // for any other popstate, which stays a navigation.
  function restoreQuery(from, to) {
    const a = new URL(from), b = new URL(to);
    if (a.origin !== b.origin || a.pathname !== b.pathname) return false;
    const changed = new Set();
    for (const k of new Set([...a.searchParams.keys(), ...b.searchParams.keys()])) {
      if (!same(a.searchParams.getAll(k), b.searchParams.getAll(k))) changed.add(k);
    }
    if (!changed.size) return false;
    const owners = new Map();
    document.querySelectorAll("[data-brick-query]").forEach((root) => {
      const q = queryOf(root);
      if (Object.keys(q).some((p) => changed.has(p))) owners.set(root, q);
    });
    const owned = new Set([...owners.values()].flatMap((q) => Object.keys(q)));
    if (![...changed].every((p) => owned.has(p))) return false;
    owners.forEach((q, root) => {
      const sets = Object.entries(q).map(([p, [field, declared]]) => {
        const vals = b.searchParams.getAll(p);
        if (!vals.length) return [field, declared];
        return [field, Array.isArray(declared) ? vals : vals[0]];
      });
      // $params is $refresh plus the component's brick: {params:} hook
      //: the URL, not the component, changed these fields.
      send(root, "$params", [], undefined, { sets });
    });
    return true;
  }
  // Registered before nav.js's own listener, so a query-only Back is
  // restored in place and nav.js (when present) is told to skip it.
  if (typeof window.addEventListener === "function") window.addEventListener("popstate", () => {
    const from = seenURL;
    seenURL = location.href;
    if (restoreQuery(from, location.href)) brick._popHandled = location.href;
  });

  const ENGINE_KINDS = new Set(effectKinds.keys());

  // ---- events between components -------------------------------

  // EVENT_NAME is the event name grammar (5.1): lowercase, because the name
  // also appears in an attribute name (brick:on:<event>), which HTML
  // lowercases.
  const EVENT_NAME = /^[a-z0-9][a-z0-9._:-]{0,63}$/;
  // RESERVED are the names the runtime and the kit emit themselves, so no
  // action or page can fake a recovery, a navigation or a loop report.
  const RESERVED = new Set(["action-error", "action-cancelled", "action-retry", "recovered", "state-gone", "state-lost", "navigate", "navigated", "flash", "flash-clear", "effect", "toast", "event-loop", "island-loaded", "island-error", "errors-cleared", "upload-progress", "upload-done", "upload-error", "push-error", "refreshed", "dirty", "clean"]);
  // DEPTH_MAX is how many events may follow from one another (an event
  // dispatched by a listener action is one hop deeper); FANOUT_MAX is how
  // many listener actions one event may start. Page behaviour, not
  // policy: they join the brick-config meta when it exists (5.7).
  const DEPTH_MAX = 4;
  const FANOUT_MAX = 50;

  const nameError = (name) => {
    if (typeof name !== "string" || !EVENT_NAME.test(name)) return "brick: bad event name " + JSON.stringify(name);
    if (name.startsWith("brick:") || RESERVED.has(name)) return "brick: event name " + JSON.stringify(name) + " is reserved";
    return "";
  };
  const callsOf = (el, attr) => {
    try {
      const m = JSON.parse(el.getAttribute(attr) || "{}");
      return m && typeof m === "object" ? m : {};
    } catch { return {}; }
  };
  const before = (a, b) => (a === b ? 0 : a.compareDocumentPosition(b) & a.DOCUMENT_POSITION_FOLLOWING ? -1 : 1);

  // deliver applies 5.6: emit the event on brick.on, then enqueue each
  // listening component's call on its own queue. origin is the element the
  // event came from (an action's root, a clicked element, or null); its
  // stateful root is the source, which on: listeners never include unless
  // to is "self".
  function deliver(name, data, origin, to, ctx) {
    const err = nameError(name);
    if (err) { console.warn(err); return; }
    const payload = data && typeof data === "object" && !Array.isArray(data) ? data : {};
    const source = origin ? origin.closest(STATEFUL) : null;
    brick.emit(name, { ...payload, $source: source ? source.getAttribute("data-brick") : null });
    if (ctx.leaving) return;
    const chain = [...ctx.chain, name];
    if (ctx.chain.includes(name) || chain.length > DEPTH_MAX) {
      console.warn("brick: event " + JSON.stringify(name) + " not delivered: " + (ctx.chain.includes(name) ? "cycle " : "too deep ") + chain.join(" > "));
      brick.emit("event-loop", { name, chain });
      return;
    }
    const calls = [];
    if (to === "self") {
      if (source) { const c = callsOf(source, "data-brick-on")[name]; if (c) calls.push([source, c]); }
    } else {
      document.querySelectorAll("[data-brick-on]").forEach((el) => {
        if (el === source || !el.matches(STATEFUL)) return;
        if (to && el.getAttribute("data-brick-name") !== to) return;
        const c = callsOf(el, "data-brick-on")[name];
        if (c) calls.push([el, c]);
      });
      // A call-site listener (brick:on:<event> on the child's tag) hears
      // only that child: the source itself, or a presentational child
      // inside it.
      const up = origin && origin.closest("[data-brick-up]");
      if (up && source && (up === source || source.contains(up))) {
        const parent = up.parentElement && up.parentElement.closest(STATEFUL);
        const c = callsOf(up, "data-brick-up")[name];
        if (parent && c) calls.push([parent, c]);
      }
    }
    calls.sort((a, b) => before(a[0], b[0]));
    const seen = new Set();
    let started = 0;
    for (const [target, call] of calls) {
      if (seen.has(target)) continue;
      seen.add(target);
      if (started === FANOUT_MAX) {
        console.warn("brick: event " + JSON.stringify(name) + " reached more than " + FANOUT_MAX + " listeners; the rest were skipped");
        break;
      }
      let action, lit;
      try { [action, lit] = parseCall(call); } catch (e) { console.error(e); continue; }
      started++;
      send(target, action, action === "$refresh" ? [] : [...lit, payload], undefined, { chain, collapse: true });
    }
  }

  // clientDispatch is brick.dispatch and $dispatch: a dispatch that starts
  // in the browser and sends nothing itself.
  function clientDispatch(el, name, data, to) {
    const err = nameError(name);
    if (err) throw new Error(err);
    deliver(name, data, el, to || "", { chain: [], leaving: false });
  }

  // target resolves where an action call runs: the stateful root around
  // el, or for "$parent.<name>" the stateful root around that one.
  function target(el, action) {
    let root = el && el.closest(STATEFUL);
    if (root && action.startsWith("$parent.")) {
      root = root.parentElement && root.parentElement.closest(STATEFUL);
      action = action.slice("$parent.".length);
      if (!root) throw new Error("brick: " + action + ": no stateful component around this one");
    }
    return [root, action];
  }

  // fieldsOf reads what the browser holds for a root: each brick:model
  // field's value (a radio group's checked one, a checkbox group's checked
  // values), in DOM order. With server set it reads what the server last
  // rendered instead (the default value, checked and selected state), which
  // is what brick:dirty compares against and what a reset restores.
  function fieldsOf(root, server = false) {
    const arrays = arraysOf(root);
    const out = new Map();
    root.querySelectorAll("*").forEach((el) => {
      const m = modelOf(el);
      if (!m) return;
      const f = m.field;
      const checked = server ? el.defaultChecked : el.checked;
      if (el.type === "radio") { if (checked) out.set(f, el.value); else if (!out.has(f)) out.set(f, null); return; }
      if (el.type === "checkbox" && arrays.has(f)) {
        if (!out.has(f)) out.set(f, []);
        if (checked) out.get(f).push(el.value);
        return;
      }
      out.set(f, valueOf(el, server));
    });
    return out;
  }

  const recoveries = new WeakMap(); // root -> recovery times

  // recover rebuilds a root whose state the server lost:
  // a fresh render of this page, the same root found by its data-brick id
  // and morphed in, the browser's field values replayed, then the failed
  // action run once more. Anything unexpected falls back to a reload.
  async function recover(root, job) {
    const id = root.getAttribute("data-brick") || "";
    const mode = (job.form && job.form.getAttribute("brick:recover")) || root.getAttribute("brick:recover") || "";
    let cancelled = false;
    brick.emit("state-gone", { id, action: job.action, preventDefault: () => { cancelled = true; } });
    if (cancelled) return;
    const now = Date.now();
    const times = (recoveries.get(root) || []).filter((t) => now - t < RECOVER_WINDOW_MS);
    if (!id || mode === "off" || times.length >= RECOVER_MAX) { lastResort(id); return; }
    times.push(now);
    recoveries.set(root, times);
    let doc;
    try {
      const res = await fetch(location.href, { credentials: "same-origin", headers: { Accept: "text/html", "Brick-Recover": id } });
      if (!res.ok) throw new Error("recover: " + res.status);
      doc = new DOMParser().parseFromString(await res.text(), "text/html");
    } catch {
      lastResort(id);
      return;
    }
    // From here on a failure is an ordinary action error: once a replayed
    // action has been sent it may have run, so it is never sent again.
    const fresh = [...doc.querySelectorAll("[data-brick]")].find((el) => el.getAttribute("data-brick") === id && el.hasAttribute("data-brick-state"));
    if (!fresh) { lastResort(id); return; } // the deploy removed it
    const freshValues = fieldsOf(fresh);
    const held = fieldsOf(root);
    // What the user typed, kept across the morph (named fields of a
    // brick:submit form included): the fresh page's defaults must not
    // overwrite it.
    const typed = [...root.querySelectorAll("input, textarea, select")].map((el) => [el, el.value, el.checked]);
    morphElement(root, fresh);
    typed.forEach(([el, value, checked]) => { if (root.contains(el)) { el.value = value; el.checked = checked; } });
    bind(root);
    showBusy(root);
    // Without a recover action the job is replayed as ONE request that
    // carries every held value the fresh render lacks, so the
    // values and the action land together or not at all.
    const replay = [...(job.sets || [])];
    if (mode) {
      if (await post(root, mode, [Object.fromEntries(held)]) === GONE) { lastResort(id); return; }
    } else {
      held.forEach((value, field) => { if (!same(value, freshValues.get(field))) replay.push([field, value]); });
    }
    if (await post(root, job.action, job.args, job.chain, replay, null, job) === GONE) { lastResort(id); return; }
    brick.emit("recovered", { id });
  }

  // refetch renders root fresh: this page fetched again,
  // the same root found by its data-brick id and morphed in, with nothing
  // replayed, so a component whose action failed with a 500 starts over
  // from a new state instead of the one that broke it. brick:ignore
  // regions keep what they hold, as in any morph. It throws when the page
  // cannot be fetched or no longer has the component.
  async function refetch(root) {
    const id = root.getAttribute("data-brick") || "";
    if (!id) throw new Error("brick.reloadComponent: the component has no data-brick id");
    const wait = lag();
    if (wait) await wait;
    debugLog("→ reload", id);
    const res = await fetch(location.href, { credentials: "same-origin", headers: { Accept: "text/html", "Brick-Recover": id } });
    if (!res.ok) throw Object.assign(new Error("brick.reloadComponent: " + res.status), { status: res.status });
    const doc = new DOMParser().parseFromString(await res.text(), "text/html");
    const fresh = [...doc.querySelectorAll("[data-brick]")].find((el) => el.getAttribute("data-brick") === id && el.hasAttribute("data-brick-state"));
    if (!fresh) throw new Error("brick.reloadComponent: the page no longer has component " + JSON.stringify(id));
    deferred.delete(root);
    morphElement(root, fresh);
    bind(root);
    showBusy(root);
  }

  // lastResort reloads the page, at most RELOAD_MAX times a minute;
  // past that the page stays as it is and "state-lost" is emitted.
  function lastResort(id) {
    const now = Date.now();
    let list = [];
    try { list = JSON.parse(sessionStorage.getItem(RELOADS_KEY) || "[]").filter((t) => now - t < RELOAD_WINDOW_MS); } catch { list = []; }
    if (list.length >= RELOAD_MAX) { brick.emit("state-lost", { id }); return; }
    list.push(now);
    try { sessionStorage.setItem(RELOADS_KEY, JSON.stringify(list)); } catch { /* storage blocked: reload anyway */ }
    brick._reload();
  }

  // flush sends every brick:model value of root still waiting on its
  // debounce, so an action that follows (a submit) sees what was typed.
  function flush(root) {
    root.querySelectorAll("*").forEach((el) => {
      const push = pending.get(el);
      if (!push) return;
      clearTimeout(timers.get(el));
      pending.delete(el);
      push();
    });
  }

  // DISPATCH_CALL matches a $dispatch(...) call, which runs in the browser
  // only and so works outside any stateful component.
  const DISPATCH_CALL = /^\s*\$dispatch\s*\(/;

  // act runs one event attribute's call: a client $dispatch, or an action
  // queued on the stateful root around el, which becomes its trigger.
  function act(el, call, extra, form, mods = {}, answer) {
    let root, action, args;
    try {
      [action, args] = parseCall(call);
      if (action === "$dispatch") {
        const [name, data, to] = args;
        clientDispatch(el, name, data, to);
        return;
      }
      [root, action] = target(el, action);
      if (action === "$cancelUpload") {
        if (root) cancelUpload(root, args[0]);
        return;
      }
    } catch (err) {
      // A malformed call is a bug in the page; say so rather than letting
      // the button silently do nothing (the engine rejects literal ones).
      console.error(err);
      brick.emit("action-error", { action: call, message: String(err && err.message || err) });
      return;
    }
    if (!root) return;
    if (extra !== undefined && !(mods.optimistic && OPTIMISTIC.has(action))) args.push(extra);
    if (answer !== undefined) args.push(answer);
    flush(root);
    const onFail = mods.optimistic && OPTIMISTIC.has(action) ? optimistic(root, action, args) : null;
    send(root, action, args, form, { trigger: el, lock: Boolean(mods.lock), onFail });
  }

  // ---- optimistic writes ----------------------------------------

  // OPTIMISTIC are the built-ins .optimistic may apply in the browser: the
  // only ones whose result needs no server code (D8).
  const OPTIMISTIC = new Set(["$set", "$toggle"]);

  // signalNow is a path's current client value: its signal, or what a
  // brick:show / brick:text of that path shows when no input binds it.
  function signalNow(root, path) {
    const sig = signals.get(root);
    if (sig && sig.has(path)) return sig.get(path);
    for (const el of root.querySelectorAll("[brick\\:show]")) {
      const f = el.getAttribute("brick:show");
      if (f === path) return !el.hasAttribute("hidden");
      if (f === "!" + path) return el.hasAttribute("hidden");
    }
    const t = [...root.querySelectorAll("[brick\\:text]")].find((el) => el.getAttribute("brick:text") === path);
    return t ? t.textContent : undefined;
  }

  // optimistic writes a $set / $toggle into the root's signals at once, so
  // brick:text, brick:show and brick:dirty update before the server
  // answers, and returns the function that puts the old value back when
  // the action fails. On success the morph decides, as always.
  function optimistic(root, action, args) {
    const path = String(args[0]);
    const old = signalNow(root, path);
    const value = action === "$toggle" ? !old : args[1];
    if (!signals.has(root)) bind(root);
    signals.get(root).set(path, value);
    render(root);
    return () => {
      if (!root.isConnected) return;
      signals.get(root).set(path, old);
      render(root);
    };
  }

  // ---- brick:confirm: one replaceable confirmer, a function
  // (message, el) => boolean or Promise<boolean>. The default is the
  // browser's own dialog (CSP-safe); the kit or an app registers a better
  // one with brick.confirmWith.
  const defaultConfirm = (message) => window.confirm(message);
  let confirmer = defaultConfirm;
  // The prompter brick:confirm.prompt asks, (message, el) => string, null
  // for a cancel, or a promise of either; brick.promptWith replaces it.
  const defaultPrompt = (message) => window.prompt(message);
  let prompter = defaultPrompt;

  // confirmThen asks the confirmer when el has brick:confirm and acts only
  // on yes; without one it acts at once, in this task. The message is
  // passed as plain text, never HTML.
  function confirmThen(el, a, extra, form) {
    const c = attrsOf(el, "confirm")[0];
    if (!c) { act(el, a.value, extra, form, a.mods); return; }
    const message = c.value;
    if (c.mods.prompt) {
      // .prompt: the answer is the call's last argument; a
      // cancel (null or undefined) sends nothing.
      Promise.resolve()
        .then(() => prompter(message, el))
        .then((answer) => { if (answer !== null && answer !== undefined) act(el, a.value, extra, form, a.mods, String(answer)); }, (err) => console.error(err));
      return;
    }
    Promise.resolve()
      .then(() => confirmer(message, el))
      .then((ok) => { if (ok) act(el, a.value, extra, form, a.mods); }, (err) => console.error(err));
  }

  // ---- brick:method links (v23) --------------------------------------------
  // <a href brick:method="delete"> sends that verb instead of a GET: a
  // plain left click (after brick:confirm, when there is one) builds a
  // hidden form to the href and submits it natively, so the browser shows
  // whatever the server answers, as for any form post. A form can only
  // GET or POST, so another verb rides in METHOD_FIELD, which
  // brick.MethodOverride turns back into the verb on the server. A
  // modified click (new tab, new window) and a visitor without JS get the
  // link's ordinary GET. The engine carries the same table (MethodJS).
  const METHOD_FIELD = "_method"; const METHOD_VERBS = ["post","put","patch","delete"];
  // methodForm is the host's seam for the hidden form, (form, link) =>
  // void: a host with its own CSRF token appends it here
  // (brick.methodFormWith). brick has no generic token for plain forms.
  let methodForm = () => {};
  const methodBusy = new Set(); // links whose brick:disable-with look is on
  let methodBusyWatch = false; // the pageshow listener is registered
  function submitMethodLink(a, verb) {
    const form = document.createElement("form");
    form.method = "post";
    form.action = a.href;
    form.hidden = true;
    if (a.target) form.target = a.target;
    if (verb !== "post") {
      const f = document.createElement("input");
      f.type = "hidden";
      f.name = METHOD_FIELD;
      f.value = verb;
      form.appendChild(f);
    }
    methodForm(form, a);
    if (a.hasAttribute("brick:disable-with")) {
      // Back to a page kept in the back/forward cache: its links are idle.
      // Registered on first use, so a page without a busy link adds none.
      if (!methodBusyWatch) {
        methodBusyWatch = true;
        window.addEventListener("pageshow", (e) => { if (e.persisted) restoreMethodLinks(); });
      }
      swapLabel(a, true);
      a.setAttribute("aria-disabled", "true");
      methodBusy.add(a);
    }
    document.body.appendChild(form);
    // submit(), not requestSubmit(): no submit event, so navigation never
    // turns the post into a fetch whose failure would fall back to a GET.
    HTMLFormElement.prototype.submit.call(form);
    // The page is leaving, unless the answer opens elsewhere (target).
    setTimeout(() => form.remove(), 0);
    if (a.target) restoreMethodLinks();
  }
  function restoreMethodLinks() {
    methodBusy.forEach((a) => { swapLabel(a, false); a.removeAttribute("aria-disabled"); });
    methodBusy.clear();
  }
  // Registered before nav.js, so a method link is never also a navigation
  // (nav skips a click whose default was prevented).
  document.addEventListener("click", (e) => {
    if (e.defaultPrevented || e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return;
    const a = e.target.closest && e.target.closest("a[href][brick\\:method]");
    if (!a) return;
    const verb = (a.getAttribute("brick:method") || "").trim().toLowerCase();
    // An empty verb (brick:method="{{ if .x }}delete{{ end }}") is an
    // ordinary link.
    if (!verb) return;
    e.preventDefault();
    if (!METHOD_VERBS.includes(verb)) { console.error("brick: brick:method=" + JSON.stringify(verb) + " is not one of " + METHOD_VERBS.join(", ")); return; }
    if (methodBusy.has(a)) return;
    const c = a.getAttribute("brick:confirm");
    if (c === null) { submitMethodLink(a, verb); return; }
    Promise.resolve()
      .then(() => confirmer(c, a))
      .then((ok) => { if (ok) submitMethodLink(a, verb); }, (err) => console.error(err));
  });

  // ---- event modifiers: .once, .debounce.<n>ms, .throttle.<n>ms
  const fired = new WeakMap(); // el -> Set(attribute names) a .once already ran
  const eventTimers = new WeakMap(); // el -> Map(attribute name -> {timer, last})

  // fire applies an event attribute's modifiers, then confirms and acts.
  // .once is per rendered element: a morph that keeps the node keeps it.
  function fire(el, a, extra, form) {
    const o = a.mods;
    if (o.once) {
      let names = fired.get(el);
      if (!names) { names = new Set(); fired.set(el, names); }
      if (names.has(a.name)) return;
      names.add(a.name);
    }
    let byName = eventTimers.get(el);
    if (!byName) { byName = new Map(); eventTimers.set(el, byName); }
    let st = byName.get(a.name);
    if (!st) { st = { timer: 0, last: 0 }; byName.set(a.name, st); }
    if (o.debounce !== undefined) {
      clearTimeout(st.timer);
      st.timer = setTimeout(() => confirmThen(el, a, extra, form), o.debounce);
      return;
    }
    if (o.throttle !== undefined) {
      const now = Date.now();
      if (st.last && now - st.last < o.throttle) return;
      st.last = now;
    }
    confirmThen(el, a, extra, form);
  }

  // outsideClicks holds the elements with a brick:click.outside
  // attribute, registered when their root is bound; a removed one is
  // dropped on the next click.
  const outsideClicks = new Set();
  // shown reports whether el is displayed: a closed dropdown or dialog
  // (hidden, or display:none) has nothing to close, so a click outside it
  // does nothing.
  const shown = (el) => !el.closest("[hidden]") && (typeof getComputedStyle !== "function" || getComputedStyle(el).display !== "none");
  // clickDetails is what brick:click.details passes as the call's last
  // argument (v22): the modifier keys held, the button and click count
  // (detail: 0 for a click made with the keyboard), and the pointer
  // position, in the viewport (clientX/clientY) and relative to the
  // element carrying the attribute (x/y), rounded to whole pixels. Only
  // an attribute with .details sends it.
  function clickDetails(e, el) {
    const r = el.getBoundingClientRect ? el.getBoundingClientRect() : { left: 0, top: 0 };
    const cx = Math.round(Number(e.clientX) || 0), cy = Math.round(Number(e.clientY) || 0);
    return {
      alt: Boolean(e.altKey), ctrl: Boolean(e.ctrlKey), meta: Boolean(e.metaKey), shift: Boolean(e.shiftKey),
      button: Number(e.button) || 0, count: Number(e.detail) || 0,
      x: cx - Math.round(r.left), y: cy - Math.round(r.top), clientX: cx, clientY: cy,
    };
  }
  document.addEventListener("click", (e) => {
    const [el, a] = closestWith(e.target, "click", (x) => !x.mods.outside);
    if (el && (el.closest(STATEFUL) || DISPATCH_CALL.test(a.value))) {
      e.preventDefault();
      fire(el, a, a.mods.details ? clickDetails(e, el) : undefined);
    }
    // brick:click.outside runs for a click that lands outside the element
    // (and on a target still in the page, so a click whose own action
    // removed it is not "outside" everything); it never prevents the
    // click's default, which belongs to whatever was clicked.
    if (!e.target || !e.target.isConnected) return;
    outsideClicks.forEach((o) => {
      if (!o.isConnected) { outsideClicks.delete(o); return; }
      if (o.contains(e.target) || !shown(o)) return;
      attrsOf(o, "click").forEach((x) => { if (x.mods.outside) fire(o, x, x.mods.details ? clickDetails(e, o) : undefined); });
    });
  });
  // submit listens in the bubble phase so a listener on the form (or
  // anywhere below document) can cancel the action with preventDefault;
  // it is registered before nav.js, so stopImmediatePropagation still
  // keeps navigation from also submitting the form.
  document.addEventListener("submit", (e) => {
    const form = e.target;
    const a = attrsOf(form, "submit")[0];
    if (!a || !form.closest(STATEFUL)) return;
    if (e.defaultPrevented) { e.stopImmediatePropagation(); return; }
    e.preventDefault();
    e.stopImmediatePropagation();
    // The fields are passed only when the form has named fields; a form
    // bound entirely with brick:model has none, and its action takes
    // (state, props) only. Names starting with "brick:" are the engine's
    // no-JavaScript fallback (the handle, token and call, and the names it
    // gives brick:model controls): never an action's field.
    const fields = Object.fromEntries([...new FormData(form, e.submitter)].filter(([k]) => !k.startsWith("brick:")));
    fire(form, a, Object.keys(fields).length ? fields : undefined, form);
  });
  // eventValue is what a change, input or blur passes as the action's last
  // argument: a checkbox's checked state, another control's value, and
  // nothing for an element that is not a control.
  const eventValue = (el) => (el.type === "checkbox" ? el.checked : el.value);
  document.addEventListener("change", (e) => {
    const [el, a] = closestWith(e.target, "change");
    if (!el || !el.closest(STATEFUL)) return;
    fire(el, a, eventValue(el));
  });
  // brick:input runs on every edit, like brick:change but without waiting
  // for the control to commit; an IME composition sends once it ends.
  document.addEventListener("input", (e) => {
    if (e.isComposing) return;
    const [el, a] = closestWith(e.target, "input");
    if (!el || !el.closest(STATEFUL)) return;
    fire(el, a, eventValue(el));
  });
  document.addEventListener("compositionend", (e) => {
    const [el, a] = closestWith(e.target, "input");
    if (el && el.closest(STATEFUL)) fire(el, a, eventValue(el));
  });
  // brick:focus and brick:blur run when the element itself gains or loses
  // focus; focus and blur do not bubble, so a capturing listener sees
  // them, and an ancestor's attribute is not used (a form with
  // brick:blur would otherwise fire on every field it contains).
  ["focus", "blur"].forEach((kind) => document.addEventListener(kind, (e) => {
    const el = e.target;
    if (!el || el.nodeType !== 1 || !el.closest(STATEFUL)) return;
    attrsOf(el, kind).forEach((a) => { if (!a.mods.window) fire(el, a, kind === "blur" ? eventValue(el) : undefined); });
  }, true));
  // windowFocus holds the elements with brick:focus.window or
  // brick:blur.window, registered when their root is bound; they run when
  // the browser window itself gains or loses focus (switching tabs or
  // apps), with no value. An element's own focus never reaches here: the
  // window's listener sees only events whose target is the window.
  const windowFocus = new Set();
  if (typeof window.addEventListener === "function") ["focus", "blur"].forEach((kind) => window.addEventListener(kind, (e) => {
    if (e.target !== window) return;
    windowFocus.forEach((w) => {
      if (!w.isConnected) { windowFocus.delete(w); return; }
      attrsOf(w, kind).forEach((a) => { if (a.mods.window) fire(w, a); });
    });
    jsWindow.forEach((w) => {
      if (!w.isConnected) { jsWindow.delete(w); return; }
      attrsOf(w, "js").forEach((x) => { if (x.mods[kind] && x.mods.window) runJS(w, x); });
    });
  }));

  // windowKeys holds the elements with a brick:keydown.window or
  // brick:keyup.window attribute, registered when their root is bound; a
  // removed one is dropped on the next key.
  const windowKeys = new Set();
  // KEY_MODIFIERS are the combo modifiers and the KeyboardEvent flag each
  // one reads.
  const KEY_MODIFIERS = { ctrl: "ctrlKey", meta: "metaKey", shift: "shiftKey", alt: "altKey" };
  // keyMatches reports whether key event e matches attribute a. An
  // attribute without a key matches every key; one with a key matches it
  // only when exactly its modifier keys are held. A letter or digit is
  // also matched by its physical key (e.code), because shift and alt
  // change e.key (shift+k is "K", alt+k on a Mac is "˚").
  const keyMatches = (a, e) => {
    const k = a.mods.key;
    if (!k) return true;
    if (Object.entries(KEY_MODIFIERS).some(([m, flag]) => Boolean(a.mods[m]) !== Boolean(e[flag]))) return false;
    if (k.length !== 1) return k === e.key;
    return k === String(e.key).toLowerCase() || e.code === (/\d/.test(k) ? "Digit" : "Key") + k.toUpperCase();
  };
  // brick:keydown[.<key>] runs on a key pressed inside the element (the
  // innermost element whose key matches) and prevents the key's default,
  // so Enter in a form's input does not also submit the form;
  // brick:keydown.<key>.window runs for a key pressed anywhere.
  // brick:keyup is the same on release, without preventing anything: a
  // key's default has already happened when it comes up.
  ["keydown", "keyup"].forEach((kind) => document.addEventListener(kind, (e) => {
    if (e.isComposing) return;
    // .norepeat (v22) skips the auto-repeats of a held key: the key still
    // matches (so its default stays prevented and no outer binding takes
    // it), but only its first keydown fires.
    const repeat = (x) => Boolean(e.repeat && x.mods.norepeat);
    const [el, a] = closestWith(e.target, kind, (x) => !x.mods.window && keyMatches(x, e));
    if (el && el.closest(STATEFUL)) {
      if (kind === "keydown") e.preventDefault();
      if (!repeat(a)) fire(el, a);
    }
    windowKeys.forEach((w) => {
      if (!w.isConnected) { windowKeys.delete(w); return; }
      attrsOf(w, kind).forEach((x) => { if (x.mods.window && keyMatches(x, e) && !repeat(x)) fire(w, x); });
    });
  }));

  // ---- JS commands ---------------------------------------------

  // brick:js.<event>="cmd(args); cmd(args)" runs a fixed list of DOM
  // commands when the event fires, on any page, with no request. They only
  // change the DOM, the way a CSS class toggle would, and never read or
  // write component state, so they are not the declarative action DSL
  // brick deliberately does not have. GRAMMAR.js lists the commands and their argument
  // counts, the target first: a CSS selector (every match), "@name" (a
  // brick:ref in the element's own scope), "this", or "closest <selector>".
  const JS_CALL = /^\s*([A-Za-z]+)\s*\(([\s\S]*)\)\s*$/;
  // JS_URL_ATTRS hold URLs: a command may not point them at a script.
  const JS_URL_ATTRS = new Set(["href", "src", "action", "formaction", "xlink:href", "poster"]);
  const JS_BAD_URL = /^\s*(javascript|vbscript|data):/i;
  // JS_DEPTH_MAX stops a dispatch whose listener runs commands that
  // dispatch again from looping forever.
  const JS_DEPTH_MAX = 8;
  let jsDepth = 0;

  // splitJS splits a command list on semicolons outside JSON strings
  // (grammar.go splitJSCommands).
  function splitJS(v) {
    const out = [];
    let start = 0, inStr = false;
    for (let i = 0; i < v.length; i++) {
      const c = v[i];
      if (inStr && c === "\\") i++;
      else if (c === '"') inStr = !inStr;
      else if (!inStr && c === ";") { out.push(v.slice(start, i)); start = i + 1; }
    }
    out.push(v.slice(start));
    return out.filter((x) => x.trim());
  }

  // jsAttrRefused is grammar.go jsAttrRefused: the runtime checks again
  // because {{ }} can fill a name at render time.
  function jsAttrRefused(name) {
    const n = String(name).toLowerCase();
    if (!/^[A-Za-z_][\w.:-]*$/.test(name)) return "is not an attribute name";
    if (n.startsWith("on")) return "is an event handler";
    if (n === "srcdoc") return "would load a document";
    if (n.startsWith("brick:") || n.startsWith("data-brick")) return "belongs to the engine";
    return "";
  }

  // parseJS turns a command list into [[name, args]], or throws on the
  // first bad command, so a list runs whole or not at all.
  function parseJS(v) {
    const cmds = splitJS(String(v || "")).map((c) => {
      const m = JS_CALL.exec(c);
      if (!m) throw new Error("brick:js: " + JSON.stringify(c.trim()) + " is not a command");
      const n = GRAMMAR.js[m[1]];
      if (!n) throw new Error("brick:js: unknown command " + JSON.stringify(m[1]));
      const args = m[2].trim() ? JSON.parse("[" + m[2] + "]") : [];
      if (args.length < n[0] || args.length > n[1]) throw new Error("brick:js: " + m[1] + " takes " + n[0] + (n[1] !== n[0] ? " to " + n[1] : "") + " arguments, got " + args.length);
      if (typeof args[0] !== "string" || !args[0].trim()) throw new Error("brick:js: " + m[1] + ": the target must be a non-empty string");
      if (/Attr$/.test(m[1])) {
        const why = jsAttrRefused(args[1]);
        if (why) throw new Error("brick:js: " + m[1] + ": " + JSON.stringify(args[1]) + " " + why);
        if (JS_URL_ATTRS.has(String(args[1]).toLowerCase()) && args.slice(2).some((x) => JS_BAD_URL.test(String(x)))) throw new Error("brick:js: " + m[1] + ": a script URL is refused");
      }
      if (m[1] === "dispatch" && JS_EVENTS.includes(args[1])) throw new Error("brick:js: dispatch: " + JSON.stringify(args[1]) + " would re-trigger brick's own listeners");
      return [m[1], args];
    });
    if (!cmds.length) throw new Error("brick:js: no commands");
    return cmds;
  }

  // jsTargets resolves a command's target from the element that carries
  // the attribute.
  function jsTargets(el, t) {
    if (t === "this") return [el];
    if (t.startsWith("@")) { const r = refIn(el, t.slice(1)); return r ? [r] : []; }
    if (t.startsWith("closest ")) { const c = el.closest(t.slice(8).trim()); return c ? [c] : []; }
    return [...document.querySelectorAll(t)];
  }

  // sticky records, per element, the net class and attribute changes
  // commands made, as {cls: Map(class -> on), attrs: Map(name -> value or
  // null)}. syncAttrs replays them after a morph, so a menu a command
  // opened stays open when an action re-renders its component
  // (the commands are sticky); a replaced element starts clean.
  const sticky = new WeakMap();
  function stick(el, kind, key, value) {
    let s = sticky.get(el);
    if (!s) { s = { cls: new Map(), attrs: new Map() }; sticky.set(el, s); }
    s[kind].set(key, value);
  }
  function restick(el) {
    const s = sticky.get(el);
    if (!s) return;
    s.cls.forEach((on, c) => el.classList.toggle(c, on));
    s.attrs.forEach((v, n) => { if (v === null) el.removeAttribute(n); else if (el.getAttribute(n) !== v) el.setAttribute(n, v); });
  }
  const setA = (t, n, v) => { stick(t, "attrs", n, v); if (v === null) t.removeAttribute(n); else t.setAttribute(n, v); };
  const classesOf = (v) => String(v).split(/\s+/).filter(Boolean);

  // showEl and hideEl are show and hide: the hidden attribute, as
  // brick:show uses, with hide playing brick:leave first.
  function showEl(t) {
    const cancel = leaving.get(t);
    if (cancel) cancel();
    const was = t.hasAttribute("hidden");
    setA(t, "hidden", null);
    if (was) enter(t);
  }
  function hideEl(t) {
    if (t.hasAttribute("hidden") || leaving.has(t)) return;
    stick(t, "attrs", "hidden", "");
    leave(t, (restore) => { t.setAttribute("hidden", ""); restore(); });
  }

  const JS_RUN = {
    show: showEl,
    hide: hideEl,
    toggle: (t) => (t.hasAttribute("hidden") || leaving.has(t) ? showEl(t) : hideEl(t)),
    focus: (t) => t.focus(),
    addClass: (t, c) => classesOf(c).forEach((x) => { t.classList.add(x); stick(t, "cls", x, true); }),
    removeClass: (t, c) => classesOf(c).forEach((x) => { t.classList.remove(x); stick(t, "cls", x, false); }),
    toggleClass: (t, c) => classesOf(c).forEach((x) => stick(t, "cls", x, t.classList.toggle(x))),
    setAttr: (t, n, v) => setA(t, n, String(v)),
    removeAttr: (t, n) => setA(t, n, null),
    // toggleAttr(t, name) toggles presence; with on (and off) it swaps
    // between the two values, off absent meaning removed.
    toggleAttr: (t, n, on, off) => {
      if (on === undefined) { setA(t, n, t.hasAttribute(n) ? null : ""); return; }
      setA(t, n, t.getAttribute(n) === String(on) ? (off === undefined ? null : String(off)) : String(on));
    },
    dispatch: (t, name, detail) => t.dispatchEvent(new CustomEvent(name, { bubbles: true, detail: detail === undefined ? null : detail })),
  };

  // runJS runs one brick:js attribute of el: .once is per rendered
  // element, as for the action events.
  function runJS(el, a) {
    if (a.mods.once) {
      let names = fired.get(el);
      if (!names) { names = new Set(); fired.set(el, names); }
      if (names.has(a.name)) return;
      names.add(a.name);
    }
    let cmds;
    try { cmds = parseJS(a.value); } catch (err) {
      console.error(err);
      brick.emit("action-error", { action: a.value, message: String(err && err.message || err) });
      return;
    }
    try { execJS(el, cmds, a.value); } catch (err) { console.error(err); }
  }

  // execJS runs parsed commands with el as "this", for an attribute and
  // for brick.js alike; a command that throws stops the rest and the
  // error reaches the caller.
  function execJS(el, cmds, source) {
    if (jsDepth >= JS_DEPTH_MAX) { console.warn("brick:js: commands nested too deep; stopped at " + JSON.stringify(source)); return; }
    jsDepth++;
    try {
      for (const [name, args] of cmds) jsTargets(el, args[0]).forEach((t) => JS_RUN[name](t, ...args.slice(1)));
    } finally {
      jsDepth--;
    }
  }

  // The events brick:js listens for (grammar.go jsEvents). The bubbling
  // ones take the innermost element whose attribute names the event; focus,
  // blur, mouseenter and mouseleave do not bubble and look only at their
  // target. Every listener captures at the document, so a component that
  // stops an event's propagation (or brick:submit's own listener) cannot
  // keep a command from running; a command never prevents the default.
  const JS_EVENTS = ["click", "dblclick", "input", "change", "submit", "focus", "blur", "keydown", "keyup", "mouseenter", "mouseleave"];
  const JS_SELF = new Set(["focus", "blur", "mouseenter", "mouseleave"]);
  // jsOutside and jsWindow hold the elements with brick:js.click.outside
  // and brick:js.<key event>.window, registered by scanJS.
  const jsOutside = new Set();
  const jsWindow = new Set();
  function scanJS(scope) {
    elementsWith(scope, "js", true).forEach(([el, a]) => {
      if (a.mods.outside) jsOutside.add(el);
      if (a.mods.window) jsWindow.add(el);
    });
  }
  const KEY_EVENTS = new Set(["keydown", "keyup"]);
  JS_EVENTS.forEach((kind) => document.addEventListener(kind, (e) => {
    if (e.isComposing) return;
    const isKey = KEY_EVENTS.has(kind);
    const own = (x) => x.mods[kind] && !x.mods.outside && !x.mods.window && (!isKey || keyMatches(x, e));
    if (JS_SELF.has(kind)) {
      if (e.target && e.target.nodeType === 1) attrsOf(e.target, "js").filter(own).forEach((x) => runJS(e.target, x));
      return;
    }
    const [el] = closestWith(e.target, "js", own);
    if (el) attrsOf(el, "js").filter(own).forEach((x) => runJS(el, x));
    if (kind === "click" && e.target && e.target.isConnected) {
      jsOutside.forEach((o) => {
        if (!o.isConnected) { jsOutside.delete(o); return; }
        if (o.contains(e.target) || !shown(o)) return;
        attrsOf(o, "js").forEach((x) => { if (x.mods.click && x.mods.outside) runJS(o, x); });
      });
    }
    if (isKey) {
      jsWindow.forEach((w) => {
        if (!w.isConnected) { jsWindow.delete(w); return; }
        attrsOf(w, "js").forEach((x) => { if (x.mods[kind] && x.mods.window && keyMatches(x, e)) runJS(w, x); });
      });
    }
  }, true));
  if (document.readyState === "loading") document.addEventListener("DOMContentLoaded", () => scanJS(document.documentElement)); else scanJS(document.documentElement);

  // ---- bindings -------------------------------

  // POLL_MIN_MS floors brick:poll, so a typo like "10ms" cannot hammer the
  // server. brick-config pollMin lowers or raises it (H5), but never below
  // POLL_FLOOR_MS, the safety floor no page can remove.
  const POLL_FLOOR_MS = 250;
  const POLL_MIN_MS = Math.max(POLL_FLOOR_MS, configInt("pollMin", 1000));
  const signals = new WeakMap(); // root -> Map(field -> value)
  const synced = new WeakMap(); // root -> Map(field -> value the server last rendered)
  const dirtyRoots = new WeakSet(); // roots that last reported "dirty"
  const timers = new WeakMap(); // input -> timeout
  const pending = new WeakMap(); // input -> its debounced send, until it runs
  const lastSent = new WeakMap(); // input -> when its .throttle last sent
  const composing = new WeakSet(); // inputs inside an IME composition
  const polled = new WeakMap(); // element -> Set(brick:poll attribute names) running

  // modelOf reads brick:model[.lazy|.defer][.debounce.<n>ms|.throttle.<n>ms]
  // -> {field, lazy, defer, wait, throttle}.
  const modelOf = (el) => {
    const a = attrsOf(el, "model")[0];
    if (!a) return null;
    const o = a.mods;
    return { field: a.value, lazy: Boolean(o.lazy), defer: Boolean(o.defer), wait: o.debounce !== undefined ? o.debounce : DEBOUNCE_MS, throttle: o.throttle };
  };
  // arraysOf is the set of state paths whose value is a list, stamped on
  // the root by the engine (data-brick-arrays): a checkbox bound to one
  // contributes its value to a list rather than a boolean, even when the
  // group has a single box.
  const arraysOf = (root) => new Set((root.getAttribute("data-brick-arrays") || "").split(/\s+/).filter(Boolean));
  // valueOf is one control's value, or with server the value the server
  // rendered into it. A <select multiple> is always a list.
  const valueOf = (el, server = false) => {
    if (el.type === "checkbox") return server ? el.defaultChecked : el.checked;
    if (el.tagName === "SELECT") {
      const opts = [...el.options];
      const picked = opts.filter((o) => (server ? o.defaultSelected : o.selected));
      if (el.multiple) return picked.map((o) => o.value);
      return (picked[0] || opts[0] || { value: "" }).value;
    }
    const v = server ? el.defaultValue : el.value;
    if (el.type === "number" || el.type === "range") return v === "" ? null : Number(v);
    return v;
  };
  // fieldValue is a bound field's current value: for a checkbox in a
  // group bound to a list, the checked values of the whole group in DOM
  // order.
  function fieldValue(root, el, field) {
    if (el.type === "checkbox" && arraysOf(root).has(field)) {
      const out = [];
      root.querySelectorAll("input[type=checkbox]").forEach((c) => { const m = modelOf(c); if (m && m.field === field && c.checked) out.push(c.value); });
      return out;
    }
    return valueOf(el);
  }

  // render applies a root's signals to its brick:text and brick:show, then
  // its dirty state.
  function render(root) {
    const sig = signals.get(root);
    if (!sig) return;
    root.querySelectorAll("[brick\\:text]").forEach((el) => {
      const f = el.getAttribute("brick:text");
      if (sig.has(f)) el.textContent = sig.get(f) == null ? "" : String(sig.get(f));
    });
    root.querySelectorAll("[brick\\:show]").forEach((el) => {
      let f = el.getAttribute("brick:show");
      const not = f.startsWith("!");
      if (not) f = f.slice(1);
      if (!sig.has(f)) return;
      const on = Boolean(sig.get(f)) !== not;
      // A change plays brick:enter / brick:leave as a JS show or hide does;
      // an element already in the right state is left alone.
      if (on) {
        const cancel = leaving.get(el);
        if (cancel) cancel();
        if (el.hasAttribute("hidden")) { el.removeAttribute("hidden"); enter(el); }
      } else if (!el.hasAttribute("hidden") && !leaving.has(el)) {
        leave(el, (restore) => { el.setAttribute("hidden", ""); restore(); });
      }
    });
    showDirty(root);
    notifyWatchers(root);
  }

  // ---- brick.watch: root -> Set({path, fn, last}). A watcher
  // sees a signal: a bound field, or a value an optimistic write set. It is
  // called only when the value changes, however it changed.
  const watchers = new WeakMap();
  function notifyWatchers(root) {
    const ws = watchers.get(root);
    const sig = signals.get(root);
    if (!ws || !sig) return;
    ws.forEach((w) => {
      if (!sig.has(w.path)) return;
      const v = sig.get(w.path);
      if (same(v, w.last)) return;
      const old = w.last;
      w.last = v;
      try { w.fn(v, old); } catch (err) { console.error(err); }
    });
  }

  // ---- brick:ref -------------------------------------------------

  // refIn finds the brick:ref named name that belongs to el's stateful root
  // (not one inside a nested stateful child), or to the page outside every
  // root when el is outside one.
  function refIn(el, name) {
    const root = el && el.closest ? el.closest(STATEFUL) : null;
    const scope = root || document;
    // querySelectorAll never matches the scope itself, so a ref on the
    // stateful root element (a component whose root is the <form>) is
    // checked first.
    if (root && root.getAttribute("brick:ref") === name) return root;
    for (const r of scope.querySelectorAll("[brick\\:ref]")) {
      if (r.getAttribute("brick:ref") === name && r.closest(STATEFUL) === root) return r;
    }
    return null;
  }

  // ---- brick:offline ---------------------------------------------

  // showOffline shows brick:offline elements (hides brick:offline.remove
  // ones) while the browser reports no network, and marks <html
  // data-offline> for CSS.
  const OFFLINE = "[brick\\:offline], [brick\\:offline\\.remove]";
  let wasOffline = false;
  function showOffline(scope = document) {
    const off = typeof navigator !== "undefined" && navigator.onLine === false;
    if (off !== wasOffline) {
      wasOffline = off;
      if (off) document.documentElement.setAttribute("data-offline", ""); else document.documentElement.removeAttribute("data-offline");
    }
    if (!scope.querySelectorAll) return;
    const apply = (el, invert) => { if (off !== invert) el.removeAttribute("hidden"); else el.setAttribute("hidden", ""); };
    const self = scope.nodeType === 1 && scope.matches(OFFLINE) ? [scope] : [];
    [...self, ...scope.querySelectorAll(OFFLINE)].forEach((el) => {
      if (el.hasAttribute("brick:offline")) apply(el, false);
      if (el.hasAttribute("brick:offline.remove")) apply(el, true);
    });
  }
  if (typeof window.addEventListener === "function") {
    window.addEventListener("online", () => showOffline());
    window.addEventListener("offline", () => showOffline());
  }

  // showDirty: a field is dirty while its signal differs from
  // what the server last rendered. brick:dirty shows while any field (or
  // any listed one) is dirty, .remove hides instead; the root carries
  // data-dirty, and "dirty" / "clean" are emitted when that flips.
  function showDirty(root) {
    const sig = signals.get(root);
    const base = synced.get(root);
    if (!sig || !base) return;
    const dirty = new Set();
    sig.forEach((v, f) => { if (!same(v, base.get(f))) dirty.add(f); });
    elementsWith(root, "dirty").forEach(([el, a]) => {
      const fields = a.value.trim().split(/\s+/).filter(Boolean);
      const on = fields.length ? fields.some((f) => dirty.has(f)) : dirty.size > 0;
      if (a.mods.remove ? !on : on) el.removeAttribute("hidden"); else el.setAttribute("hidden", "");
    });
    const now = dirty.size > 0;
    if (now) root.setAttribute("data-dirty", ""); else root.removeAttribute("data-dirty");
    if (now !== dirtyRoots.has(root)) {
      if (now) dirtyRoots.add(root); else dirtyRoots.delete(root);
      brick.emit(now ? "dirty" : "clean", { id: root.getAttribute("data-brick"), fields: [...dirty] });
    }
  }

  // startPolls starts each brick:poll under root that is not running yet
  //: brick:poll="5s" refreshes, brick:poll.5s="call" runs a
  // call, .visible pauses while the element is off screen. Every poll
  // skips its ticks while the tab is hidden, unless .keep-alive (a chat
  // or a job that must keep its session warm); .visible still holds then,
  // judged by the element's last reported position. A poll is quiet (no
  // loading state) and stops once its element is removed.
  function startPolls(root) {
    elementsWith(root, "poll", true).forEach(([el, a]) => {
      if (el.closest(STATEFUL) !== root) return; // the child's own bind starts it
      let names = polled.get(el);
      if (!names) { names = new Set(); polled.set(el, names); }
      if (names.has(a.name)) return;
      const interval = a.mods.interval !== undefined ? a.mods.interval : msOf(a.value.trim());
      const call = a.mods.interval !== undefined ? a.value : "$refresh";
      if (!Number.isFinite(interval)) return;
      names.add(a.name);
      let visible = true;
      let io = null;
      if (a.mods.visible && "IntersectionObserver" in window) {
        visible = false;
        io = new IntersectionObserver((entries) => { visible = entries[entries.length - 1].isIntersecting; });
        io.observe(el);
      }
      const id = setInterval(() => {
        if (!el.isConnected) { clearInterval(id); if (io) io.disconnect(); return; }
        if ((document.hidden && !a.mods["keep-alive"]) || !visible) return;
        let r, action, args;
        try { [action, args] = parseCall(call); [r, action] = target(el, action); } catch (err) { console.error(err); return; }
        if (r) send(r, action, args, undefined, { quiet: true, collapse: true });
      }, Math.max(POLL_MIN_MS, interval));
    });
  }

  // ---- brick:intersect ------------------------------------------

  // THRESHOLD maps .half / .full to the observer's threshold; the default
  // is any visible pixel.
  const THRESHOLD = { half: 0.5, full: 1 };
  const watchedViews = new WeakMap(); // element -> Map(attribute name -> {io, busy, fired, seen, dwell, dwelt, inView})

  // observe re-arms one brick:intersect. A fresh observe makes the browser
  // report the element's current state again, which is what loads the next
  // page when an infinite-scroll sentinel is still visible after a load
  // (D4).
  function rearm(rec, el) {
    if (!el.isConnected) { rec.io.disconnect(); return; }
    rec.io.unobserve(el);
    rec.io.observe(el);
  }

  // startIntersects observes every brick:intersect of root, or re-arms one
  // already observed; bind calls it after every morph. While a job the
  // element started is queued or in flight, new reports are ignored.
  function startIntersects(root) {
    if (!("IntersectionObserver" in window)) return;
    elementsWith(root, "intersect", true).forEach(([el, a]) => {
      if (el.closest(STATEFUL) !== root) return; // the child's own bind
      let byName = watchedViews.get(el);
      if (!byName) { byName = new Map(); watchedViews.set(el, byName); }
      const old = byName.get(a.name);
      if (old) { if (!old.busy) rearm(old, el); return; }
      // .dwell: dwell is the pending timer of the current stay in view,
      // dwelt marks a stay that already fired (one call per stay, so a
      // row read for a minute counts once), inView the last report.
      const rec = { io: null, busy: false, fired: false, seen: false, dwell: 0, dwelt: false, inView: false };
      const opts = {};
      if (a.mods.half) opts.threshold = THRESHOLD.half;
      if (a.mods.full) opts.threshold = THRESHOLD.full;
      if (a.mods.margin !== undefined) opts.rootMargin = a.mods.margin + "px";
      const fire = () => {
        if (rec.busy || (a.mods.once && rec.fired)) return;
        const r = el.closest(STATEFUL);
        let action, args;
        try { [action, args] = parseCall(el.getAttribute(a.name)); } catch (err) { console.error(err); return; }
        let target2;
        try { [target2, action] = target(el, action); } catch (err) { console.error(err); return; }
        if (!r || !target2) return;
        rec.fired = true;
        rec.busy = true;
        send(target2, action, args, undefined, { trigger: el }).finally(() => {
          rec.busy = false;
          // A dwell fires once per stay, so a fresh report must not
          // start the next one while the element never left.
          if (!a.mods.once && a.mods.dwell === undefined) rearm(rec, el);
        });
      };
      // stopDwell cancels the stay's timer: the element left, the tab was
      // hidden or the element was removed before the dwell completed.
      const stopDwell = () => { clearTimeout(rec.dwell); rec.dwell = 0; };
      const startDwell = () => {
        if (rec.dwell || rec.dwelt || (a.mods.once && rec.fired)) return;
        rec.dwell = setTimeout(() => {
          rec.dwell = 0;
          if (!el.isConnected) { rec.io.disconnect(); return; }
          if (!rec.inView || document.hidden) return;
          rec.dwelt = true;
          fire();
        }, a.mods.dwell);
      };
      if (a.mods.dwell !== undefined && typeof document.addEventListener === "function") {
        // A hidden tab is not being looked at: the stay ends, and a new
        // one starts when the tab returns with the element still in view.
        const onVisibility = () => {
          if (!el.isConnected) { stopDwell(); document.removeEventListener("visibilitychange", onVisibility); return; }
          if (document.hidden) stopDwell(); else if (rec.inView) startDwell();
        };
        document.addEventListener("visibilitychange", onVisibility);
      }
      rec.io = new IntersectionObserver((entries) => {
        const en = entries[entries.length - 1];
        if (!el.isConnected) { stopDwell(); rec.io.disconnect(); return; }
        if (a.mods.dwell !== undefined) {
          rec.inView = en.isIntersecting;
          if (!en.isIntersecting) { stopDwell(); rec.dwelt = false; return; }
          if (!document.hidden) startDwell();
          return;
        }
        // .leave fires on leaving only after the element was seen, so the
        // first report of an element that starts off screen is not a leave.
        const hit = a.mods.leave ? (!en.isIntersecting && rec.seen) : en.isIntersecting;
        if (en.isIntersecting) rec.seen = true;
        if (hit) fire();
      }, opts);
      byName.set(a.name, rec);
      rec.io.observe(el);
    });
  }

  // bind reads the signals from a root's bound inputs and what the server
  // rendered into them, and starts its polls, window keys and viewport
  // actions.
  function bind(root) {
    signals.set(root, fieldsOf(root));
    synced.set(root, fieldsOf(root, true));
    liveSent.delete(root);
    render(root);
    startPolls(root);
    startIntersects(root);
    syncPush();
    ["keydown", "keyup"].forEach((kind) => elementsWith(root, kind, true).forEach(([el, a]) => { if (a.mods.window) windowKeys.add(el); }));
    ["focus", "blur"].forEach((kind) => elementsWith(root, kind, true).forEach(([el, a]) => { if (a.mods.window) windowFocus.add(el); }));
    elementsWith(root, "click", true).forEach(([el, a]) => { if (a.mods.outside) outsideClicks.add(el); });
    scanJS(root);
  }

  function modelChanged(el, isChange) {
    const m = modelOf(el);
    const root = m && el.closest(STATEFUL);
    if (!root) return;
    if (m.lazy && !isChange) return;
    if (el.type === "radio" && !el.checked) return;
    if (!signals.has(root)) bind(root);
    const value = fieldValue(root, el, m.field);
    signals.get(root).set(m.field, value);
    render(root);
    clearTimeout(timers.get(el));
    if (m.defer) {
      // Sent with the root's next request, whatever it is.
      pending.delete(el);
      deferredOf(root).set(m.field, value);
      return;
    }
    const push = () => { pending.delete(el); lastSent.set(el, Date.now()); send(root, "$set", [m.field, value]); };
    if (isChange || m.lazy) { pending.delete(el); push(); return; }
    if (m.throttle !== undefined) {
      // At most one send per window, and a trailing one with the last value.
      const since = Date.now() - (lastSent.get(el) || 0);
      if (since >= m.throttle) { pending.delete(el); push(); return; }
      pending.set(el, push);
      timers.set(el, setTimeout(push, m.throttle - since));
      return;
    }
    pending.set(el, push);
    timers.set(el, setTimeout(push, m.wait));
  }
  // An IME composition (Japanese, Chinese, Korean input) fires input
  // events for half-built characters; the value is sent once it ends.
  document.addEventListener("compositionstart", (e) => composing.add(e.target));
  document.addEventListener("compositionend", (e) => { composing.delete(e.target); modelChanged(e.target, false); });
  document.addEventListener("input", (e) => {
    const el = e.target;
    if (e.isComposing || composing.has(el)) return;
    if (el.type !== "checkbox" && el.tagName !== "SELECT") modelChanged(el, false);
  });
  document.addEventListener("change", (e) => {
    const el = e.target;
    if (el.type === "checkbox" || el.tagName === "SELECT" || (modelOf(el) || {}).lazy) modelChanged(el, true);
  });

  // A reset button restores a form's inputs to what the server last
  // rendered; the runtime then drops the form's pending and
  // deferred values and re-reads the signals. A $set already sent left the
  // server ahead of the inputs, so those fields go back as one $refresh.
  document.addEventListener("reset", (e) => {
    const form = e.target;
    const root = form.closest && form.closest(STATEFUL);
    if (!root) return;
    setTimeout(() => {
      if (e.defaultPrevented) return;
      const fields = new Set();
      form.querySelectorAll("*").forEach((el) => {
        const m = modelOf(el);
        if (!m) return;
        fields.add(m.field);
        clearTimeout(timers.get(el));
        pending.delete(el);
      });
      const d = deferred.get(root);
      const q = queues.get(root);
      fields.forEach((f) => { if (d) d.delete(f); });
      if (q) q.waiting.forEach((j) => { if (j.action === "$set" && fields.has(j.args[0])) j.skip = true; });
      const now = fieldsOf(root);
      signals.set(root, now);
      render(root);
      const live = liveSent.get(root);
      const drift = [];
      fields.forEach((f) => { if (live && live.has(f) && !same(live.get(f), now.get(f))) drift.push([f, now.get(f)]); });
      if (drift.length) send(root, "$refresh", [], undefined, { sets: drift });
    });
  });

  // ---- server push ------------------------------------------------

  // A stateful root rendered with brick: {listen:} carries its topics
  // (data-brick-listen, space separated) and a listen token the server
  // sealed for them. The page opens ONE EventSource for every topic on it,
  // at the events endpoint beside the action URL, sending the tokens: the
  // browser never names a topic, so it can only hear what the server
  // rendered for it. Each "refresh" event names a topic, and every root
  // listening to it runs a quiet $refresh: at most one per root per tick
  // (a burst coalesces), and while the tab is hidden only the keep-alive
  // roots (data-brick-listen-keep, like brick:poll.keep-alive) refresh,
  // the others once it is shown. The stream reopens only when the set of
  // topics changes, not when an action re-renders a root with a fresh
  // token. EventSource reconnects by itself after a dropped connection;
  // a refused one (403, 410, 503) closes it, and the runtime retries with
  // backoff (PUSH_RETRY_MS doubling to PUSH_RETRY_MAX_MS, PUSH_RETRIES
  // times). Every reconnect refreshes every listener once, because what
  // was published while the stream was down is not replayed.
  const LISTEN = "[data-brick-listen-token]";
  const PUSH_RETRY_MS = 1000;
  const PUSH_RETRY_MAX_MS = 30000;
  const PUSH_RETRIES = 8;
  const eventsURL = () => actionURL().replace(/action$/, "events");
  let pushSource = null; // the open EventSource
  let pushKey = ""; // its sorted topics
  let pushFailures = 0; // refused opens in a row
  let pushRetry = 0; // the pending retry timer
  let pushScheduled = false;
  let pushDropped = false; // the stream dropped since it last opened
  let pushCatchUp = false; // the next sync reopens after the page was away
  const pushDue = new Set(); // roots to refresh at the next tick
  const pushDeferred = new Set(); // roots waiting for the tab to show
  let pushTick = 0;

  const listeners = () => [...document.querySelectorAll(LISTEN)];
  const topicsOf = (el) => (el.getAttribute("data-brick-listen") || "").split(" ").filter(Boolean);

  // refreshPushed queues one quiet $refresh for root, coalesced per tick.
  function refreshPushed(root) {
    if (!root.isConnected) return;
    if (document.hidden && !root.hasAttribute("data-brick-listen-keep")) { pushDeferred.add(root); return; }
    pushDue.add(root);
    if (pushTick) return;
    pushTick = setTimeout(() => {
      pushTick = 0;
      const due = [...pushDue];
      pushDue.clear();
      due.forEach((r) => { if (r.isConnected) send(r, "$refresh", [], undefined, { quiet: true, collapse: true }); });
    }, 0);
  }

  function closePush() {
    clearTimeout(pushRetry);
    pushRetry = 0;
    if (pushSource) { pushSource.close(); pushSource = null; }
  }

  // openPush opens the stream for the listeners on the page now.
  function openPush() {
    closePush();
    const els = listeners();
    if (!els.length || typeof window.EventSource !== "function") return;
    const tokens = [...new Set(els.map((el) => el.getAttribute("data-brick-listen-token")))];
    const url = eventsURL() + "?" + tokens.map((t) => "l=" + encodeURIComponent(t)).join("&");
    const es = new window.EventSource(url, { withCredentials: crossActions });
    pushSource = es;
    es.addEventListener("open", () => {
      if (es !== pushSource) return;
      pushFailures = 0;
      if (pushDropped) { pushDropped = false; listeners().forEach(refreshPushed); }
    });
    es.addEventListener("refresh", (e) => {
      if (es !== pushSource) return;
      const topic = String(e.data);
      listeners().forEach((el) => { if (topicsOf(el).includes(topic)) refreshPushed(el); });
    });
    es.addEventListener("error", () => {
      if (es !== pushSource) return;
      pushDropped = true;
      // CONNECTING: the browser is reconnecting by itself. CLOSED: the
      // server refused, so the runtime retries, a bounded number of times.
      if (es.readyState !== 2) return;
      pushSource = null;
      if (++pushFailures > PUSH_RETRIES) { brick.emit("push-error", { failures: pushFailures - 1 }); return; }
      const wait = Math.min(PUSH_RETRY_MAX_MS, PUSH_RETRY_MS * 2 ** (pushFailures - 1));
      pushRetry = setTimeout(() => { pushRetry = 0; openPush(); }, wait);
    });
  }

  // syncPush reopens the stream when the page's set of topics changed
  // (a navigation, a component added or removed), once per tick.
  function syncPush() {
    // A page that never listened costs nothing: no timer, no scan later.
    if (pushScheduled || (!pushKey && !document.querySelector(LISTEN))) return;
    pushScheduled = true;
    setTimeout(() => {
      pushScheduled = false;
      const key = [...new Set(listeners().flatMap(topicsOf))].sort().join(" ");
      if (key === pushKey && (pushSource || pushRetry || !key)) return;
      pushKey = key;
      pushFailures = 0;
      pushDropped = pushCatchUp;
      pushCatchUp = false;
      if (key) openPush(); else closePush();
    }, 0);
  }

  document.addEventListener("visibilitychange", () => {
    if (document.hidden) return;
    const waiting = [...pushDeferred];
    pushDeferred.clear();
    waiting.forEach(refreshPushed);
  });
  // A page kept in the back/forward cache must not hold its stream open
  // (it would use up the browser's connections per host); it reopens, and
  // catches up, when the page comes back.
  if (typeof window.addEventListener === "function") {
    window.addEventListener("pagehide", () => { closePush(); pushKey = ""; });
    window.addEventListener("pageshow", (e) => { if (e.persisted) { pushCatchUp = true; syncPush(); } });
  }

  // ---- lazy islands --------------------------------------------

  // A <brick-lazy data-brick-lazy=TICKET data-when=…> stands where an
  // island will be: it shows the boundary's loading block until the
  // runtime redeems the ticket at the island endpoint (beside the action
  // endpoint) and replaces the placeholder with the answer. The ticket is
  // all the request names: the server decides what renders.
  const LAZY = "brick-lazy[data-brick-lazy]";
  const ISLAND = "application/vnd.brick.island+json";
  const ISLAND_VERSION = 1;
  const islandURL = () => actionURL().replace(/action$/, "island");
  const lazyStarted = new WeakSet();
  const lazyWatch = new WeakMap(); // placeholder -> its IntersectionObserver
  // whenDue calls go once the placeholder is due. "visible" observes the
  // placeholder's children (a display: contents element has no box of its
  // own), or a 1px sentinel when the boundary gave no loading block.
  function whenDue(el, go) {
    const when = el.getAttribute("data-when");
    if (when === "visible" && "IntersectionObserver" in window) {
      let targets = [...el.children];
      if (!targets.length) {
        const s = document.createElement("span");
        s.setAttribute("aria-hidden", "true");
        s.style.cssText = "display:inline-block;width:1px;height:1px";
        el.appendChild(s);
        targets = [s];
      }
      const io = new IntersectionObserver((entries) => {
        if (entries.some((e) => e.isIntersecting)) { io.disconnect(); lazyWatch.delete(el); go(); }
      });
      lazyWatch.set(el, io);
      targets.forEach((t) => io.observe(t));
    } else if (when === "idle") {
      if ("requestIdleCallback" in window) requestIdleCallback(go); else setTimeout(go, 1);
    } else {
      go();
    }
  }
  function startLazy(scope) {
    if (scope.nodeType !== 1) return;
    const found = scope.matches(LAZY) ? [scope] : [...scope.querySelectorAll(LAZY)];
    found.forEach((el) => {
      if (lazyStarted.has(el)) return;
      lazyStarted.add(el);
      el.style.display = "contents";
      whenDue(el, () => loadLazy(el));
    });
  }
  // appendAssets links the island's stylesheets and modules the page does
  // not have yet, inside <brick-assets> (as a navigation does).
  function appendAssets(list) {
    const have = new Set([...document.querySelectorAll("script[src], link[rel=stylesheet][href]")].map((e) => e.getAttribute("src") || e.getAttribute("href")));
    let target = document.querySelector("brick-assets");
    const nonce = (document.querySelector("brick-assets [nonce]") || {}).nonce || "";
    (list || []).forEach((a) => {
      if (!a || typeof a.url !== "string" || have.has(a.url)) return;
      have.add(a.url);
      if (!target) { target = document.createElement("brick-assets"); document.body.appendChild(target); }
      const el = document.createElement(a.css ? "link" : "script");
      if (a.css) { el.rel = "stylesheet"; el.setAttribute("href", a.url); } else { el.type = "module"; el.setAttribute("src", a.url); }
      if (a.integrity) el.setAttribute("integrity", a.integrity);
      if (nonce) el.setAttribute("nonce", nonce);
      target.appendChild(el);
    });
  }
  async function loadLazy(el) {
    if (!el.isConnected) return;
    const ticket = el.getAttribute("data-brick-lazy");
    const headers = { Accept: ISLAND };
    // The page's query, so an island with brick: {query:} reads the URL
    // the visitor sees, not the endpoint's.
    const q = typeof location === "undefined" ? "" : location.search.replace(/^\?/, "");
    if (q) headers["Brick-Query"] = q;
    try {
      if (hooks.request.length && await requestHooks({ root: null, action: "", args: [], trigger: el, island: ticket }, headers)) return;
      const res = await fetch(islandURL() + "?t=" + encodeURIComponent(ticket), { credentials: CREDENTIALS, headers });
      // An expired or foreign ticket: the page is stale, so reload it once
      // (the same guard as a lost state).
      if (res.status === GONE) { lastResort(""); return; }
      if (!res.ok) throw new Error("brick: island: " + res.status);
      const ans = JSON.parse(await res.text());
      if (!ans || ans.v !== ISLAND_VERSION) throw new Error("brick: unsupported island response version " + JSON.stringify(ans && ans.v));
      if (!el.isConnected) return;
      appendAssets(ans.assets);
      const tpl = document.createElement("template");
      tpl.innerHTML = typeof ans.html === "string" ? ans.html : "";
      const io = lazyWatch.get(el);
      if (io) { io.disconnect(); lazyWatch.delete(el); }
      const parent = el.parentNode;
      el.replaceWith(tpl.content);
      if (parent && parent.nodeType === 1) startLazy(parent);
      brick.emit("island-loaded", { failed: res.headers && typeof res.headers.get === "function" && res.headers.get("Brick-Island-Error") === "1" });
    } catch (err) {
      brick.emit("island-error", { message: String(err && err.message || err) });
    }
  }

  // ---- streamed islands ----------------------------------------

  // A streamed page writes each slow island at the end of the same
  // response as a hidden <brick-fill data-brick-fill=ID>, and an inline
  // script moves it into <brick-lazy data-brick-stream=ID> as it arrives.
  // The runtime moves whatever that script could not: every fill when a
  // CSP blocked the script (they then land when the page finishes), and a
  // fill carrying data-brick-assets, whose stylesheets and modules are
  // linked first.
  const FILL = "brick-fill[data-brick-fill]";
  function applyFills(scope) {
    if (scope.nodeType !== 1) return;
    const found = scope.matches(FILL) ? [scope] : [...scope.querySelectorAll(FILL)];
    found.forEach((f) => {
      // Ids are engine-made ("s1"), so one outside that alphabet is not
      // ours and never reaches a selector.
      const id = f.getAttribute("data-brick-fill") || "";
      if (!/^[a-z0-9]+$/.test(id)) return;
      const place = document.querySelector('brick-lazy[data-brick-stream="' + id + '"]');
      if (!place) return;
      const assets = f.getAttribute("data-brick-assets");
      if (assets) {
        try { appendAssets(JSON.parse(assets)); } catch (err) { console.error("brick: stream assets", err); }
      }
      const frag = document.createDocumentFragment();
      while (f.firstChild) frag.appendChild(f.firstChild);
      place.replaceWith(frag);
      f.remove();
      brick.emit("island-loaded", { failed: false, stream: true });
    });
  }

  const bindAll = (root) => {
    if (root.nodeType !== 1) return;
    if (root.matches(STATEFUL)) bind(root);
    root.querySelectorAll(STATEFUL).forEach(bind);
  };
  new MutationObserver((records) => { records.forEach((r) => r.addedNodes.forEach((n) => { applyFills(n); bindAll(n); startLazy(n); if (wasOffline && n.nodeType === 1) showOffline(n); })); syncPush(); }).observe(document.documentElement, { childList: true, subtree: true });
  // boot binds the page, then runs the ready hooks once; a
  // ready hook registered later runs at once.
  const boot = () => {
    applyFills(document.documentElement);
    bindAll(document.documentElement);
    startLazy(document.documentElement);
    showOffline();
    ready = true;
    notify("ready", {});
  };
  if (document.readyState === "loading") document.addEventListener("DOMContentLoaded", boot);
  else boot();

  window.brick = {
    mount(type, fn) {
      types.set(type, fn);
      document.querySelectorAll(sel).forEach((el) => {
        if (el.getAttribute("data-brick-type") === type) schedule(el);
      });
    },
    morph: (parent, next) => morph(parent, next),
    // action calls a server action of the stateful component around el, as
    // a brick:click would: for code that is not a click — a drag and drop,
    // a keyboard shortcut, a command palette. It resolves when the patch
    // is applied, and rejects outside a stateful component.
    action(el, name, args = []) {
      let root, action;
      try { [root, action] = target(el, name); } catch (err) { return Promise.reject(err); }
      if (!root) return Promise.reject(new Error(`brick.action: ${name}: not inside a stateful component`));
      return send(root, action, args);
    },
    // hook registers a hook: "request" (add headers, cancel),
    // "response", "morph" (return false to leave an element as it is),
    // "morphed" and "ready". It returns the unregister function.
    hook(name, fn) {
      if (!Object.prototype.hasOwnProperty.call(hooks, name)) throw new Error("brick.hook: unknown hook " + JSON.stringify(name) + " (" + Object.keys(hooks).join(", ") + ")");
      if (typeof fn !== "function") throw new Error("brick.hook: not a function");
      hooks[name].push(fn);
      if (name === "ready" && ready) { try { fn({}); } catch (err) { console.error(err); } }
      return () => { const i = hooks[name].indexOf(fn); if (i >= 0) hooks[name].splice(i, 1); };
    },
    // watch calls fn(value, old) whenever the signal path of el's stateful
    // root changes: from a bound input, an optimistic write or a morph.
    watch(el, path, fn) {
      const root = el && el.closest && el.closest(STATEFUL);
      if (!root) throw new Error("brick.watch: " + path + ": not inside a stateful component");
      let ws = watchers.get(root);
      if (!ws) { ws = new Set(); watchers.set(root, ws); }
      const sig = signals.get(root);
      const w = { path, fn, last: sig && sig.has(path) ? sig.get(path) : signalNow(root, path) };
      ws.add(w);
      return () => ws.delete(w);
    },
    // errors reads the validation errors of el's stateful root as its last
    // render left them (data-brick-errors, the template's .Errors), with no
    // request: has(path) (no path: any error), get(path), first(path) (no
    // path: the first by path order), all() (a copy), and clear(path) (no
    // path: all), which drops them from this view and fires
    // "errors-cleared"; the messages the server rendered change with the
    // next render. Each call reads the root, so it is never stale after an
    // action.
    errors(el) {
      const root = el && el.closest ? el.closest(STATEFUL) : null;
      if (!root) throw new Error("brick.errors: not inside a stateful component");
      const read = () => {
        try {
          const v = JSON.parse(root.getAttribute("data-brick-errors") || "{}");
          return v && typeof v === "object" && !Array.isArray(v) ? v : {};
        } catch (err) { return {}; }
      };
      const msg = (v) => (v == null ? "" : String(v));
      const own = (all, path) => Object.prototype.hasOwnProperty.call(all, path);
      return {
        has(path) { const all = read(); return path === undefined ? Object.keys(all).length > 0 : own(all, path); },
        get(path) { const all = read(); return own(all, path) ? msg(all[path]) : ""; },
        first(path) {
          const all = read();
          if (path !== undefined) return own(all, path) ? msg(all[path]) : "";
          const keys = Object.keys(all).sort();
          return keys.length ? msg(all[keys[0]]) : "";
        },
        all() { return read(); },
        clear(path) {
          const all = read();
          if (path === undefined) root.removeAttribute("data-brick-errors");
          else {
            delete all[path];
            if (Object.keys(all).length) root.setAttribute("data-brick-errors", JSON.stringify(all));
            else root.removeAttribute("data-brick-errors");
          }
          brick.emit("errors-cleared", { root, id: root.getAttribute("data-brick"), path: path === undefined ? "" : path });
        },
      };
    },
    // js runs a brick:js command list from code: a brick.mount
    // hook, an effect handler, a stored attribute
    // (brick.js(el, el.getAttribute("data-on-close"))). The commands are
    // v18's closed list with the same targets, el standing for "this" and
    // scoping "@ref" and "closest"; a bad list throws instead of logging,
    // because the caller is code that can handle it.
    js(el, cmds) {
      if (!el || el.nodeType !== 1) throw new Error("brick.js: not an element");
      execJS(el, parseJS(cmds), cmds);
    },
    // all returns the stateful components of a type, in document order,
    // or every stateful component when type is omitted; find returns the
    // first or null. The type is the
    // component file's stem, which every stateful root carries as
    // data-brick-name (the same value data-brick-type has on a mounted
    // one), never an importer's alias. The root returned is what
    // brick.action, brick.watch and brick.errors take. The type is
    // compared as a string, never put into a selector.
    all(type) {
      const roots = [...document.querySelectorAll(STATEFUL)];
      return type === undefined ? roots : roots.filter((r) => r.getAttribute("data-brick-name") === String(type));
    },
    find(type) {
      for (const r of document.querySelectorAll(STATEFUL)) if (type === undefined || r.getAttribute("data-brick-name") === String(type)) return r;
      return null;
    },
    // ref finds the element named by brick:ref in el's stateful root (or on
    // the page outside every root), or null.
    ref(el, name) { return refIn(el, name); },
    // dispatch sends an event from el's component to its listeners, as an
    // action's brick.dispatch would; it sends no request of
    // its own. A bad or reserved name throws.
    dispatch(el, name, data = {}, { to = "" } = {}) { clientDispatch(el, name, data, to); },
    // upload sends files to an upload field of el's stateful component, as
    // a brick:upload input does: for a drop zone, a paste or
    // a camera capture. It resolves once every accepted file is done.
    upload(el, field, files) { return upload(el, field, files); },
    // effect registers the client handler of an effect kind and returns
    // its unregister function. A site's own kinds start with "x-"; the
    // engine's kinds may be replaced (brick.effect("flash", …) takes over
    // every flash).
    effect(kind, fn) {
      if (typeof kind !== "string" || (!kind.startsWith("x-") && !ENGINE_KINDS.has(kind))) throw new Error("brick.effect: custom kinds start with \"x-\": " + JSON.stringify(kind));
      const prev = effectKinds.get(kind);
      effectKinds.set(kind, fn);
      return () => {
        if (effectKinds.get(kind) !== fn) return;
        if (prev) effectKinds.set(kind, prev); else effectKinds.delete(kind);
      };
    },
    // confirmWith replaces the confirmer brick:confirm asks:
    // fn(message, el) returns a boolean or a promise of one. It returns
    // the function that puts the previous confirmer back.
    confirmWith(fn) {
      if (typeof fn !== "function") throw new Error("brick.confirmWith: not a function");
      const prev = confirmer;
      confirmer = fn;
      return () => { if (confirmer === fn) confirmer = prev; };
    },
    // methodFormWith replaces the hook a brick:method link's hidden form
    // passes through before it is submitted (v23): fn(form, link) may
    // append fields, such as the host's CSRF token. It returns the
    // function that puts the previous hook back.
    methodFormWith(fn) {
      if (typeof fn !== "function") throw new Error("brick.methodFormWith: not a function");
      const prev = methodForm;
      methodForm = fn;
      return () => { if (methodForm === fn) methodForm = prev; };
    },
    // promptWith replaces the prompter brick:confirm.prompt asks
    //: fn(message, el) returns the answer, null for a cancel,
    // or a promise of either. It returns the function that puts the
    // previous prompter back.
    promptWith(fn) {
      if (typeof fn !== "function") throw new Error("brick.promptWith: not a function");
      const prev = prompter;
      prompter = fn;
      return () => { if (prompter === fn) prompter = prev; };
    },
    emit(name, detail) { bus.dispatchEvent(new CustomEvent(name, { detail })); },
    on(name, fn) {
      const h = (e) => fn(e.detail);
      bus.addEventListener(name, h);
      listening.set(name, (listening.get(name) || 0) + 1);
      let on = true;
      return () => {
        if (!on) return;
        on = false;
        bus.removeEventListener(name, h);
        listening.set(name, listening.get(name) - 1);
      };
    },
    // reloadComponent renders el's stateful component fresh from the
    // server, after any action already queued on it: the
    // way back from an action that failed with a 500, from an
    // "action-error" listener (d.root, d.status). It resolves once the
    // fresh render is in and rejects when it cannot be had; it never fires
    // "action-error" itself, so a listener that calls it cannot loop.
    reloadComponent(el) {
      const root = el && el.closest ? el.closest(STATEFUL) : null;
      if (!root) return Promise.reject(new Error("brick.reloadComponent: not inside a stateful component"));
      return new Promise((resolve, reject) => { send(root, "$reload", [], undefined, { reload: { resolve, reject } }); });
    },
    // debug(on) logs each request and answer to the console; latency(ms)
    // delays each request by ms. Both are for a developer
    // at the console and start off.
    debug(on = true) { debugOn = Boolean(on); },
    latency(ms = 0) { latencyMs = Number.isFinite(ms) && ms > 0 ? ms : 0; },
    _reload: () => location.reload(),
    _submit: (form) => HTMLFormElement.prototype.submit.call(form), // a submit effect; replaceable in tests
    _assign: (url) => location.assign(url), // a redirect effect; replaceable in tests
    _morph: (parent, next) => morph(parent, next), // exposed for tests
    // nav.js asks first whether a link or brick.navigate changes only
    // parameters components on this page own (a patch):
    // those components get $params and nothing is fetched.
    _patchQuery: (to) => restoreQuery(location.href, to),
  };

  // A flash carried across a full page load is shown once the page's
  // scripts have run, so a Toaster that mounts on load is listening.
  const carried = takeCarriedFlashes();
  if (carried.length) {
    const showCarried = () => setTimeout(() => carried.forEach((f) => runEffect(f, null, { chain: [], leaving: false })));
    if (document.readyState === "loading") document.addEventListener("DOMContentLoaded", showCarried); else showCarried();
  }
})();
