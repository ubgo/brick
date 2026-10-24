// Runs extension.js against a stand-in for the vscode module, so its
// wiring is checked without VS Code: activation loads the schema from the
// configured brick binary, an open document with a bad frontmatter key
// gets a diagnostic, and an edit that fixes it clears it. Colours and
// squiggles themselves need a real VS Code. The binary is $BRICK_BIN when
// set, else one built from this repository.
const test = require("node:test");
const assert = require("node:assert");
const path = require("node:path");
const os = require("node:os");
const fs = require("node:fs");
const Module = require("node:module");
const { execFileSync } = require("node:child_process");

function brickBin() {
  if (process.env.BRICK_BIN) return process.env.BRICK_BIN;
  const out = path.join(fs.mkdtempSync(path.join(os.tmpdir(), "brick-vscode-")), "brick");
  execFileSync("go", ["build", "-o", out, "."], { cwd: path.join(__dirname, "..", "..", "..", "cmd", "brick") });
  return out;
}

// fakeVSCode is the part of the vscode API extension.js touches.
function fakeVSCode(bin) {
  const handlers = { open: [], change: [], close: [], config: [] };
  const sets = new Map();
  class Range {
    constructor(sl, sc, el, ec) {
      Object.assign(this, { sl, sc, el, ec });
    }
  }
  class Diagnostic {
    constructor(range, message, severity) {
      Object.assign(this, { range, message, severity });
    }
  }
  const docs = [];
  const on = (list) => (fn) => (list.push(fn), { dispose() {} });
  return {
    sets,
    handlers,
    docs,
    api: {
      Range,
      Diagnostic,
      DiagnosticSeverity: { Error: 0 },
      window: { createOutputChannel: () => ({ appendLine: (l) => console.log(l), dispose() {} }) },
      languages: {
        createDiagnosticCollection: () => ({
          set: (uri, d) => sets.set(uri.toString(), d),
          delete: (uri) => sets.delete(uri.toString()),
          dispose() {},
        }),
      },
      workspace: {
        textDocuments: docs,
        getConfiguration: () => ({ get: (k) => (k === "path" ? bin : undefined) }),
        onDidOpenTextDocument: on(handlers.open),
        onDidChangeTextDocument: on(handlers.change),
        onDidCloseTextDocument: on(handlers.close),
        onDidChangeConfiguration: on(handlers.config),
      },
    },
  };
}

function doc(file, text) {
  return { uri: { scheme: "file", toString: () => "file://" + file }, languageId: "html", getText: () => text };
}

test("activation checks open documents and edits re-check them", async () => {
  const fake = fakeVSCode(brickBin());
  const resolve = Module._resolveFilename;
  const stubs = {
    vscode: fake.api,
    "vscode-languageclient/node": {
      LanguageClient: class {
        start() {
          return Promise.resolve();
        }
        stop() {
          return Promise.resolve();
        }
      },
    },
  };
  Module._resolveFilename = function (request, ...rest) {
    if (request in stubs) return request;
    return resolve.call(this, request, ...rest);
  };
  for (const [k, v] of Object.entries(stubs)) require.cache[k] = { id: k, filename: k, loaded: true, exports: v };
  try {
    const ext = require("../extension");
    const bad = doc("/site/components/Card.html", "---\nbrick:\n  islnd: true\n---\n<div></div>\n");
    fake.docs.push(bad);
    await ext.activate({ subscriptions: [] });
    const d = fake.sets.get("file:///site/components/Card.html");
    assert.ok(d && d.length === 1, JSON.stringify(d));
    assert.match(d[0].message, /brick\.islnd is not a frontmatter key/);
    assert.deepStrictEqual([d[0].range.sl, d[0].range.sc], [2, 2]);
    assert.strictEqual(d[0].source, "brick frontmatter");

    // The fix arrives as an edit; the check runs after the typing pause.
    const fixed = doc("/site/components/Card.html", "---\nbrick:\n  island: true\n---\n<div></div>\n");
    fake.handlers.change.forEach((fn) => fn({ document: fixed }));
    await new Promise((r) => setTimeout(r, 400));
    assert.deepStrictEqual(fake.sets.get("file:///site/components/Card.html"), []);

    fake.handlers.close.forEach((fn) => fn(fixed));
    assert.ok(!fake.sets.has("file:///site/components/Card.html"));
    await ext.deactivate();
  } finally {
    Module._resolveFilename = resolve;
  }
});
