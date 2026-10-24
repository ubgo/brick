// Drives the kit's Alert and Banner in jsdom (see kit_overlays_test.go):
// the close buttons appear with JavaScript and dismiss; a Banner's
// dismissal is remembered by id across page loads.
import assert from "node:assert/strict";
import { load, tick, click } from "./kit_harness.mjs";

const first = load();
{
  const { w, doc, mount } = first;
  const events = [];
  mount("Alert");
  mount("Banner");
  await tick();
  w.brick.on("alert:dismiss", (e) => events.push(e.id));
  const alert = doc.getElementById("al");
  const x = alert.querySelector(".bk-alert__close");
  assert.equal(x.hidden, false, "the close button shows with JavaScript");
  click(w, x);
  assert.equal(doc.getElementById("al"), null, "dismissing removes the alert");
  assert.deepEqual(events, ["al"], "and emits alert:dismiss");
  assert.equal(doc.getElementById("plain").querySelector(".bk-alert__close"), null, "a plain alert has no button");

  const banner = doc.getElementById("launch");
  assert.equal(banner.hidden, false);
  click(w, banner.querySelector(".bk-banner__close"));
  assert.ok(banner.hidden, "the banner hides");
  assert.equal(w.localStorage.getItem("bk-banner:launch"), "1", "and remembers it");
  // A second visit on the same origin: the banner stays hidden.
  const again = load();
  again.w.localStorage.setItem("bk-banner:launch", "1");
  again.mount("Banner");
  await tick();
  assert.ok(again.doc.getElementById("launch").hidden, "a dismissed banner stays gone on the next visit");
  assert.equal(again.doc.getElementById("other").hidden, false, "another id still shows");
  again.w.close();
  w.close();
}
console.log("kit_alert ok");
