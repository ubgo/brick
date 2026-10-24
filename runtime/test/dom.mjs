// Event helpers shared by every jsdom test: the runtime's own tests
// (*_test.mjs) and the kit drivers (kit_*.mjs, through kit_harness.mjs).
// Each dispatches what a browser would for the interaction, bubbling and
// cancelable, so delegated listeners on document see it.

// TICK_MS is the default pause for tick: long enough for a mocked fetch's
// promise chain and the runtime's microtasks (patch, bind, remount) to
// settle in jsdom, short enough that a suite of a hundred ticks stays fast.
export const TICK_MS = 20;

// tick waits ms (TICK_MS by default) for pending async work to settle.
export const tick = (ms = TICK_MS) => new Promise((r) => setTimeout(r, ms));

// popped runs go (a history.back()/forward()) and waits for the popstate it
// fires. jsdom dispatches popstate on a later task, so a fixed tick after
// back() races it under load; wait for the event, then tick for the swap.
export const popped = (w, go) => new Promise((res) => { w.addEventListener("popstate", () => setTimeout(res, 0), { once: true }); go(); });

// click dispatches a primary-button click and returns the event, so a
// test can read defaultPrevented; opts adds modifiers or another button.
export const click = (w, el, opts = {}) => {
  const ev = new w.MouseEvent("click", { bubbles: true, cancelable: true, button: 0, ...opts });
  el.dispatchEvent(ev);
  return ev;
};

// key dispatches a keydown of k (a KeyboardEvent key, e.g. "ArrowDown").
export const key = (w, el, k, opts = {}) => el.dispatchEvent(new w.KeyboardEvent("keydown", { key: k, bubbles: true, cancelable: true, ...opts }));

// fill sets a field's value and dispatches event ("input" by default,
// "change" for a committed value), as typing or picking would.
export const fill = (w, el, value, event = "input") => {
  el.value = value;
  el.dispatchEvent(new w.Event(event, { bubbles: true }));
};
