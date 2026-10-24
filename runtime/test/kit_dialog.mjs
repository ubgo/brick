// Drives the kit's Dialog, ConfirmDialog and Drawer in jsdom (see
// kit_overlays_test.go): opening by trigger block, data-bk-open and the
// open prop, closing by Escape, backdrop, close button and data-bk-close,
// focus return, and a server-set open attribute becoming modal. Run twice:
// with a showModal stub (the browser path) and without (the fallback).
import assert from "node:assert/strict";
import { load, tick, click, key } from "./kit_harness.mjs";
import { loadESM } from "./kit_esm.mjs";

async function run(withModal) {
  const { w, doc } = load();
  const modals = [], closes = [];
  if (withModal) {
    // Like Chrome: a modal dialog stays :modal (in the top layer) until
    // close(), even when only its open attribute is removed.
    w.HTMLDialogElement.prototype.showModal = function () { this.setAttribute("open", ""); this._modal = true; modals.push(this.id); };
    w.HTMLDialogElement.prototype.close = function () { if (!this.hasAttribute("open")) return; closes.push(this.id); this._modal = false; this.removeAttribute("open"); this.dispatchEvent(new w.Event("close")); };
    const matches = w.Element.prototype.matches;
    w.Element.prototype.matches = function (sel) { return sel === ":modal" ? Boolean(this._modal) : matches.call(this, sel); };
  } else {
    delete w.HTMLDialogElement.prototype.showModal;
    delete w.HTMLDialogElement.prototype.close;
  }
  loadESM(w, "Dialog", "ConfirmDialog", "Drawer");
  await tick();
  const d1 = doc.getElementById("d1"), d2 = doc.getElementById("d2");
  const opener = doc.querySelector("#t1");

  // The open prop: server-opened, made modal once JavaScript runs.
  assert.ok(d2.open, "open prop renders open");
  if (withModal) assert.deepEqual(modals, ["d2"], "an open dialog becomes modal on mount");
  key(w, d2, "Escape");
  assert.equal(d2.open, false, "Escape closes");

  // Trigger block: a click anywhere in it opens; Escape closes; focus returns.
  opener.focus();
  click(w, opener);
  assert.ok(d1.open, "trigger block opens");
  assert.equal(doc.activeElement.closest("dialog"), withModal ? doc.activeElement.closest("dialog") : d1, "focus moves inside (fallback path)");
  key(w, d1, "Escape");
  await tick();
  assert.equal(d1.open, false);
  assert.equal(doc.activeElement, opener, "focus returns to the opener");

  // Backdrop: a press and release on the dialog box itself.
  click(w, opener);
  d1.dispatchEvent(new w.MouseEvent("mousedown", { bubbles: true }));
  click(w, d1);
  assert.equal(d1.open, false, "backdrop click closes");
  // A drag that starts inside and ends on the backdrop does not close.
  click(w, opener);
  d1.querySelector("p").dispatchEvent(new w.MouseEvent("mousedown", { bubbles: true }));
  click(w, d1);
  assert.ok(d1.open, "a drag out of the panel keeps it open");
  click(w, d1.querySelector(".bk-dialog__close"));
  assert.equal(d1.open, false, "close button closes");

  // data-bk-open anywhere; data-bk-close inside.
  click(w, doc.getElementById("open-d2"));
  assert.ok(d2.open, "data-bk-open opens");
  click(w, d2.querySelector("[data-bk-close]"));
  assert.equal(d2.open, false, "data-bk-close closes");
  // An action patch that sets open again makes it modal again.
  d2.setAttribute("open", "");
  await tick();
  if (withModal) assert.equal(modals.filter((m) => m === "d2").length, 3, "a patched open attribute becomes modal");
  key(w, d2, "Escape");

  // A patch that removes open from a modal dialog: browsers keep it in the
  // top layer, so it is really closed (close() runs) — also when the patch
  // remounts the component and the removal arrives before the observer.
  if (withModal) {
    click(w, doc.getElementById("open-d2"));
    closes.length = 0;
    d2.removeAttribute("open");
    await tick();
    assert.deepEqual(closes, ["d2"], "a removed open attribute closes the modal properly");
    assert.equal(d2.matches(":modal"), false);
    const host = d2.parentElement, parent = host.parentElement;
    click(w, doc.getElementById("open-d2"));
    closes.length = 0;
    host.remove();
    await tick();
    d2.removeAttribute("open");
    parent.append(host);
    await tick();
    assert.deepEqual(closes, ["d2"], "and when it arrives with a remount");
    assert.equal(d2.matches(":modal"), false);
  }

  // dismissible false: the backdrop does not close; Escape still does.
  const d3 = doc.getElementById("d3");
  click(w, doc.getElementById("open-d3"));
  d3.dispatchEvent(new w.MouseEvent("mousedown", { bubbles: true }));
  click(w, d3);
  assert.ok(d3.open, "not dismissible by backdrop");
  key(w, d3, "Escape");
  assert.equal(d3.open, false);

  // ConfirmDialog: an alertdialog; the confirm button closes it.
  const c = doc.getElementById("c1");
  click(w, doc.querySelector("#t-confirm"));
  assert.ok(c.open, "confirm opens from its trigger");
  assert.equal(c.getAttribute("role"), "alertdialog");
  c.dispatchEvent(new w.MouseEvent("mousedown", { bubbles: true }));
  click(w, c);
  assert.ok(c.open, "the backdrop does not dismiss a confirm");
  const confirm = c.querySelector("[brick\\:click]");
  assert.equal(confirm.getAttribute("brick:click"), "remove(3)");
  click(w, confirm);
  assert.equal(c.open, false, "confirm closes");

  // Drawer: the same code through import from brick:Dialog.
  const dr = doc.getElementById("dr");
  click(w, doc.getElementById("open-dr"));
  assert.ok(dr.open, "drawer opens");
  key(w, dr, "Escape");
  assert.equal(dr.open, false, "drawer closes on Escape");

  // closeAction: the visitor closing a server-owned dialog runs the action;
  // a re-render closing it does not.
  const calls = [];
  w.brick.action = (el, name) => { calls.push(name); return Promise.resolve(); };
  const d4 = doc.getElementById("d4");
  d4.setAttribute("open", "");
  await tick();
  assert.ok(d4.open, "a server-set open opens it");
  key(w, d4, "Escape");
  await tick();
  assert.deepEqual(calls, ["close_form"], "the visitor closing it tells the server");
  if (withModal) {
    d4.setAttribute("open", "");
    await tick();
    d4.removeAttribute("open");
    await tick();
    assert.equal(d4.matches(":modal"), false);
    assert.deepEqual(calls, ["close_form"], "a re-render closing it does not");
  }

  // commandfor fallback where invoker commands are missing.
  click(w, doc.getElementById("cmd-d2"));
  assert.ok(d2.open, "commandfor/show-modal opens without native support");
  key(w, d2, "Escape");
  w.close();
}

await run(true);
await run(false);
console.log("kit_dialog ok");
