// The brick VS Code client: it starts `brick lsp` and connects it to .html
// files. Every language answer (diagnostics, completion, hover, definition,
// formatting, references, rename) comes from the server. The one thing
// done here is checking frontmatter as it is typed (frontmatter.js),
// against the schema the same binary prints, since the server's own
// diagnostics come from `brick check` on save.
const vscode = require("vscode");
const { execFile } = require("node:child_process");
const { LanguageClient } = require("vscode-languageclient/node");
const frontmatter = require("./frontmatter");

// How long typing must pause before the frontmatter is checked again.
const CHECK_DELAY_MS = 200;
// How long `brick schema` may take before live checking is given up.
const SCHEMA_TIMEOUT_MS = 10000;
// SCHEMA_MAX_BYTES caps brick schema's output; the schema is tens of KiB,
// so 16 MiB only guards against a wrong brick.path printing without end.
const SCHEMA_MAX_BYTES = 16 << 20;

/** @type {LanguageClient | undefined} */
let client;
/** @type {vscode.OutputChannel} */
let output;
/** @type {vscode.DiagnosticCollection} */
let diagnostics;
/** @type {((text: string) => object[]) | undefined} */
let check;
const timers = new Map();

function brickPath() {
  return vscode.workspace.getConfiguration("brick").get("path") || "brick";
}

function start() {
  client = new LanguageClient(
    "brick",
    "brick",
    { command: brickPath(), args: ["lsp"] },
    { documentSelector: [{ scheme: "file", language: "html" }], outputChannel: output },
  );
  return client.start();
}

// loadSchema compiles the checker from `brick schema`. A binary that
// cannot print it (missing, or older than the schema command) turns live
// checking off and says why; the server still works.
function loadSchema() {
  return new Promise((resolve) => {
    execFile(brickPath(), ["schema"], { timeout: SCHEMA_TIMEOUT_MS, maxBuffer: SCHEMA_MAX_BYTES }, (err, stdout) => {
      check = undefined;
      if (err) {
        output.appendLine(`brick frontmatter: live checking is off: ${brickPath()} schema: ${err.message}`);
      } else {
        try {
          check = frontmatter.checker(JSON.parse(stdout));
        } catch (e) {
          output.appendLine(`brick frontmatter: live checking is off: ${e.message}`);
        }
      }
      resolve();
    });
  });
}

function isBrickDocument(doc) {
  return doc.uri.scheme === "file" && doc.languageId === "html";
}

function checkNow(doc) {
  if (!isBrickDocument(doc)) return;
  if (!check) {
    diagnostics.delete(doc.uri);
    return;
  }
  const found = check(doc.getText()).map((d) => {
    const range = new vscode.Range(d.start.line, d.start.character, d.end.line, d.end.character);
    const diag = new vscode.Diagnostic(range, d.message, vscode.DiagnosticSeverity.Error);
    diag.source = d.source;
    return diag;
  });
  diagnostics.set(doc.uri, found);
}

function checkSoon(doc) {
  const key = doc.uri.toString();
  clearTimeout(timers.get(key));
  timers.set(
    key,
    setTimeout(() => {
      timers.delete(key);
      checkNow(doc);
    }, CHECK_DELAY_MS),
  );
}

function checkOpen() {
  for (const doc of vscode.workspace.textDocuments) checkNow(doc);
}

async function activate(context) {
  output = vscode.window.createOutputChannel("brick");
  diagnostics = vscode.languages.createDiagnosticCollection("brick-frontmatter");
  context.subscriptions.push(
    output,
    diagnostics,
    vscode.workspace.onDidOpenTextDocument(checkNow),
    vscode.workspace.onDidChangeTextDocument((e) => checkSoon(e.document)),
    vscode.workspace.onDidCloseTextDocument((doc) => diagnostics.delete(doc.uri)),
    vscode.workspace.onDidChangeConfiguration(async (e) => {
      if (!e.affectsConfiguration("brick.path")) return;
      if (client) await client.stop();
      await loadSchema();
      checkOpen();
      await start();
    }),
  );
  await loadSchema();
  checkOpen();
  await start();
}

function deactivate() {
  for (const t of timers.values()) clearTimeout(t);
  return client ? client.stop() : undefined;
}

module.exports = { activate, deactivate };
