// Drives the kit's Toaster and Toast in jsdom (see kit_overlays_test.go):
// a server-rendered Toast adopted on load and one inserted later (as an
// action's patch would), brick.emit("toast") and the exported toast(),
// de-duplication by id, auto-dismiss, pausing on hover, the close button,
// the max, the region's name only while it holds a toast, and flash-clear.
import assert from "node:assert/strict";
import { load, tick, click } from "./kit_harness.mjs";
import { loadESM } from "./kit_esm.mjs";

const { w, doc } = load();
loadESM(w, "Toast", "Toaster");
await tick();
const list = doc.querySelector(".bk-toaster__list");
const titles = () => [...list.children].map((t) => t.querySelector(".bk-toast__title").textContent);
const find = (title) => [...list.children].find((t) => t.querySelector(".bk-toast__title").textContent === title);

assert.deepEqual(titles(), ["Saved"], "a Toast rendered on the page is moved into the Toaster");
assert.equal(doc.getElementById("slot").children.length, 0);
assert.equal(find("Saved").querySelector(".bk-toast__close").hidden, false, "its close button shows");
assert.equal(list.getAttribute("aria-live"), "polite");
assert.ok(doc.getElementById("second").hidden, "a second Toaster stays hidden");
assert.equal(doc.getElementById("second").hasAttribute("aria-label"), false, "an inactive Toaster is not a named region");
assert.equal(list.parentElement.getAttribute("aria-label"), "Notifications", "a Toaster holding a toast is a named region");
assert.equal(doc.getElementById("second").querySelector(".bk-toaster__list").children.length, 0);
assert.equal(doc.getElementById("preview").parentElement, doc.body, "an inline Toast stays where it was rendered");

w.brick.emit("toast", { title: "Hello", description: "World", variant: "info", duration: 0 });
const hello = find("Hello");
assert.ok(hello, "brick.emit(\"toast\") shows one");
assert.ok(hello.classList.contains("bk-toast--info"), "in its variant");
assert.equal(hello.querySelector(".bk-toast__desc").textContent, "World");
assert.ok(hello.querySelector(".bk-toast__icon"), "with the variant's icon, from the server template");

w.__bkmods.Toaster.toast("Copied");
assert.ok(find("Copied"), "the exported toast() shows one");
assert.equal(find("Copied").dataset.duration, "5000", "with the default duration");

// A Toast inserted later, as an action's patch inserts one.
const tpl = doc.querySelector("template[data-variant=success]").content.firstElementChild.cloneNode(true);
tpl.querySelector(".bk-toast__title").textContent = "Contact saved";
doc.getElementById("slot").append(tpl);
await tick();
assert.ok(find("Contact saved"), "a Toast inserted after load is picked up");

// The same flash rendered again while on screen is not shown twice.
const again = doc.querySelector("template[data-variant=success]").content.firstElementChild.cloneNode(true);
again.id = "flash";
again.querySelector(".bk-toast__title").textContent = "Saved";
doc.getElementById("slot").append(again);
await tick();
assert.equal(titles().filter((t) => t === "Saved").length, 1, "an id already on screen is dropped");
assert.equal(doc.getElementById("slot").children.length, 0);

// Auto-dismiss, paused while the pointer is over the list.
w.brick.emit("toast", { title: "Brief", duration: 60 });
list.dispatchEvent(new w.MouseEvent("mouseenter"));
await tick(120);
assert.ok(find("Brief"), "hovering pauses the timer");
list.dispatchEvent(new w.MouseEvent("mouseleave"));
await tick(120);
assert.equal(find("Brief"), undefined, "dismissed after its duration once the pointer leaves");
await tick(250);
assert.equal(find("Saved"), undefined, "the flash (duration 300) dismissed itself");
const later = doc.querySelector("template[data-variant=success]").content.firstElementChild.cloneNode(true);
later.id = "flash";
later.querySelector(".bk-toast__title").textContent = "Saved";
doc.getElementById("slot").append(later);
await tick();
assert.equal(find("Saved"), undefined, "an id already shown is not shown again after it was dismissed");

click(w, find("Copied").querySelector(".bk-toast__close"));
assert.equal(find("Copied"), undefined, "the close button dismisses");
assert.ok(find("Hello"), "duration 0 stays");

for (let i = 0; i < 8; i++) w.brick.emit("toast", { title: "n" + i, duration: 0 });
assert.equal(list.children.length, 5, "at most max are shown");
assert.deepEqual(titles().slice(-2), ["n6", "n7"], "the oldest go first");

// Once the last toast is gone the empty corner stops being a landmark.
for (const t of [...list.children]) click(w, t.querySelector(".bk-toast__close"));
await tick();
assert.equal(list.children.length, 0);
assert.equal(list.parentElement.hasAttribute("aria-label"), false, "an empty Toaster is not a named region");

// flash-clear dismisses the toasts of one variant, then all.
w.brick.emit("flash", { message: "i", level: "info" });
w.brick.emit("flash", { message: "warn", level: "warning" });
w.brick.emit("toast", { title: "plain", duration: 0 });
await tick();
w.brick.emit("flash-clear", { level: "warning" });
await tick();
assert.deepEqual(titles(), ["i", "plain"], "only the warning is dismissed");
w.brick.emit("flash-clear", { level: "" });
await tick();
assert.equal(list.children.length, 0, "every toast is dismissed");
w.close();
console.log("kit_toaster ok");
