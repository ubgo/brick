// Loads kit component modules that import each other (`import { enhance }
// from "brick:Dialog"`, rewritten by brick to the module's hashed URL) or
// export functions, which kit_harness.mjs's plain eval cannot run: each
// module becomes a function whose exports land on window.__bkmods[Name] and
// whose imports read from there. Load a dependency before its importers.
import { readFileSync } from "node:fs";
import { moduleFile } from "./kit_harness.mjs";

// brick writes an import of another component as a relative specifier,
// "./Dialog.<hash>.js" (both modules sit in the same asset directory).
const importRe = /import\s*\{([^}]*)\}\s*from\s*["']\.\/([A-Za-z][\w-]*)\.[0-9a-f]+\.js["'];?/g;
const bareImportRe = /import\s*["']\.\/[A-Za-z][\w-]*\.[0-9a-f]+\.js["'];?/g;
const exportRe = /export\s+(function|const|let)\s+(\w+)/g;

// source returns the transformed module text of a component.
export function source(name) {
  const names = [];
  let src = readFileSync(moduleFile(process.argv[2], name), "utf8")
    .replace(importRe, (_, list, from) => `const {${list.replace(/\s+as\s+/g, ": ")}} = window.__bkmods[${JSON.stringify(from)}];`)
    .replace(bareImportRe, "")
    .replace(exportRe, (_, kind, n) => { names.push(n); return `${kind} ${n}`; });
  return `(() => {\n${src}\nwindow.__bkmods = window.__bkmods || {};\nwindow.__bkmods[${JSON.stringify(name)}] = { ${names.join(", ")} };\n})();`;
}

// loadESM evaluates each named module in the window, in order.
export function loadESM(w, ...names) {
  for (const n of names) w.eval(source(n));
}

// pointer dispatches a pointer event where jsdom has no PointerEvent: a
// MouseEvent carrying pointerType and pointerId.
export function pointer(w, el, type, { x = 0, y = 0, pointerType = "mouse", button = 0 } = {}) {
  const e = new w.MouseEvent(type, { bubbles: true, cancelable: true, clientX: x, clientY: y, button });
  Object.defineProperty(e, "pointerType", { value: pointerType });
  Object.defineProperty(e, "pointerId", { value: 1 });
  el.dispatchEvent(e);
  return e;
}

// rect stubs an element's getBoundingClientRect.
export function rect(el, { left = 0, top = 0, width = 0, height = 0 }) {
  el.getBoundingClientRect = () => ({ left, top, width, height, right: left + width, bottom: top + height, x: left, y: top });
}
