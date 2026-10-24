// Shared loader for the kit's jsdom tests: the page brick rendered and the
// real runtime and component modules, from the directory runKitJS (in
// kit_test.go) writes and passes as argv[2].
import { readFileSync, readdirSync } from "node:fs";
import { join } from "node:path";
import { JSDOM, VirtualConsole } from "jsdom";

export { tick, click, key, fill } from "./dom.mjs";

// load builds the window and evaluates runtime.js, then each named
// component's module (Tabs → Tabs.<hash>.js), in the order given. A module
// that imports another ("brick:Modal") is not loadable this way; test it
// through the component it serves.
export function load(...components) {
  const dir = process.argv[2];
  const dom = new JSDOM(readFileSync(join(dir, "page.html"), "utf8"), { url: "https://site.test/", runScripts: "outside-only", pretendToBeVisual: true, virtualConsole: new VirtualConsole() });
  const w = dom.window;
  const file = (name) => readFileSync(moduleFile(dir, name), "utf8");
  w.eval(file("runtime"));
  return { w, doc: w.document, mount: (name) => w.eval(file(name)) };
}

// moduleFile is the path of a component's (or the runtime's) client
// module in dir: brick names it <Name>.<hash>.js, and the hash changes
// with every edit, so it is found by prefix.
export function moduleFile(dir, name) {
  const files = readdirSync(dir);
  const f = files.find((f) => f.startsWith(name + ".") && f.endsWith(".js"));
  if (!f) throw new Error(`no module for ${name} in ${files.join(" ")}`);
  return join(dir, f);
}
