// Drives the kit's Dropzone in jsdom (see kit_forms_test.go): the chosen
// files are listed with their sizes, images with a blob: thumbnail whose URL
// is revoked when they leave, the area highlights while dragging, and with
// brick:upload the files show until their upload ends (G8).
// jsdom has no DataTransfer, so the remove buttons stay hidden there.
import assert from "node:assert/strict";
import { load, tick } from "./kit_harness.mjs";

const { w, doc, mount } = load();
// jsdom has no blob: URLs; these record what the component creates and
// revokes, which is what is under test.
let made = 0;
const live = new Set();
w.URL.createObjectURL = () => { const u = "blob:t/" + (++made); live.add(u); return u; };
w.URL.revokeObjectURL = (u) => { live.delete(u); };
// A second Dropzone in live mode (brick:upload): outside a stateful
// component the runtime does not send, so the upload events are emitted
// by hand below.
const first = doc.querySelector(".bk-dropzone");
const second = first.cloneNode(true);
second.querySelector("input").setAttribute("brick:upload", "pics");
first.after(second);
mount("Dropzone");
await tick();
const el = first;
const input = el.querySelector("input[type=file]");
const list = el.querySelector(".bk-dropzone__list");
assert.equal(list.hidden, true, "no files, no list");
input.dispatchEvent(new w.Event("dragenter", { bubbles: true }));
assert.ok(el.classList.contains("bk-dropzone--over"), "dragging highlights");
input.dispatchEvent(new w.Event("drop", { bubbles: true }));
assert.equal(el.classList.contains("bk-dropzone--over"), false, "dropping ends it");
const files = [new w.File(["x".repeat(2048)], "report.pdf"), new w.File(["hi"], "notes.txt")];
Object.defineProperty(input, "files", { value: files, configurable: true });
input.dispatchEvent(new w.Event("change", { bubbles: true }));
assert.equal(list.hidden, false);
const rows = [...list.querySelectorAll(".bk-dropzone__file")];
assert.deepEqual(rows.map((r) => r.querySelector(".bk-dropzone__name").textContent), ["report.pdf", "notes.txt"]);
assert.deepEqual(rows.map((r) => r.querySelector(".bk-dropzone__size").textContent), ["2.0 KB", "2 B"]);
assert.equal(rows[0].querySelector(".bk-dropzone__remove").hidden, typeof w.DataTransfer !== "function", "remove needs DataTransfer");

// Images get a thumbnail from a blob: URL; other types keep the icon.
const png = new w.File(["png"], "cat.png", { type: "image/png" });
const pdf = new w.File(["pdf"], "a.pdf", { type: "application/pdf" });
Object.defineProperty(input, "files", { value: [png, pdf], configurable: true });
input.dispatchEvent(new w.Event("change", { bubbles: true }));
const [imgRow, pdfRow] = [...list.querySelectorAll(".bk-dropzone__file")];
const thumb = imgRow.querySelector("img.bk-dropzone__thumb");
assert.ok(thumb && thumb.getAttribute("src").startsWith("blob:"), "an image row shows a blob: thumbnail");
assert.equal(pdfRow.querySelector("img"), null, "a pdf keeps its icon");
assert.ok(pdfRow.querySelector(".bk-dropzone__kind svg, .bk-dropzone__kind .bk-icon"), "the pdf row still has the file icon");
assert.equal(live.size, 1, "one URL, for the one image");
// The same image chosen again reuses its URL; a different choice revokes it.
input.dispatchEvent(new w.Event("change", { bubbles: true }));
assert.equal(made, 1, "re-rendering the same file makes no new URL");
Object.defineProperty(input, "files", { value: [pdf], configurable: true });
input.dispatchEvent(new w.Event("change", { bubbles: true }));
assert.equal(live.size, 0, "the image left the list, so its URL is revoked");

// Live mode: chosen files show as uploading rows until their upload ends.
const liveInput = second.querySelector("input");
const liveList = second.querySelector(".bk-dropzone__list");
const a = new w.File(["aa"], "a.png", { type: "image/png" });
const b = new w.File(["bbb"], "b.txt", { type: "text/plain" });
Object.defineProperty(liveInput, "files", { value: [a, b], configurable: true });
liveInput.dispatchEvent(new w.Event("change", { bubbles: true }));
const pend = () => [...liveList.querySelectorAll(".bk-dropzone__file--uploading .bk-dropzone__name")].map((n) => n.textContent);
assert.deepEqual(pend(), ["a.png", "b.txt"], "both files show while they upload");
assert.equal(liveList.hidden, false);
assert.ok(liveList.querySelector("img.bk-dropzone__thumb"), "the image uploading shows its thumbnail");
assert.equal(live.size, 1);
w.brick.emit("upload-done", { field: "other", name: "a.png", size: 2 });
assert.deepEqual(pend(), ["a.png", "b.txt"], "another field's upload is not ours");
w.brick.emit("upload-done", { field: "pics", name: "a.png", size: 2 });
assert.deepEqual(pend(), ["b.txt"], "a finished upload drops its row");
assert.equal(live.size, 0, "and revokes its URL");
// A re-render that empties the list (the component's morph) puts the rows
// still uploading back.
liveList.replaceChildren();
w.brick.emit("upload-error", { field: "pics", name: "nope", size: 1 });
assert.deepEqual(pend(), ["b.txt"], "a row wiped by a morph comes back");
w.brick.emit("upload-error", { field: "pics", name: "b.txt", size: 3, message: "too big" });
assert.deepEqual(pend(), [], "a failed upload drops its row too");
assert.equal(liveList.hidden, true, "nothing uploading, no list");
w.close();
console.log("kit_dropzone ok");
