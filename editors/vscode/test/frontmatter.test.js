// Pins live frontmatter checking against the schema the real brick CLI
// prints, so a schema change that the checker misreads fails here. The CLI
// is $BRICK_BIN when set, else built from this repository with go run.
const test = require("node:test");
const assert = require("node:assert");
const path = require("node:path");
const { execFileSync } = require("node:child_process");
const { extract, checker, SOURCE } = require("../frontmatter");

function schema() {
  const opts = { encoding: "utf8", maxBuffer: 16 << 20 };
  if (process.env.BRICK_BIN) return JSON.parse(execFileSync(process.env.BRICK_BIN, ["schema"], opts));
  const cli = path.join(__dirname, "..", "..", "..", "cmd", "brick");
  return JSON.parse(execFileSync("go", ["run", ".", "schema"], { ...opts, cwd: cli }));
}

const check = checker(schema());

test("extract finds the block only on the first line", () => {
  assert.deepStrictEqual(extract("---\ntitle: x\n---\n<h1>x</h1>\n"), { yaml: "title: x\n", offset: 4 });
  assert.deepStrictEqual(extract("\uFEFF---  \r\na: 1\r\n---\r\n"), { yaml: "a: 1\r\n", offset: 8 });
  assert.strictEqual(extract("<p>x</p>\n---\na: 1\n---\n"), null);
  assert.strictEqual(extract("---\na: 1\n"), null, "an unclosed block is brick check's to report");
  assert.strictEqual(extract("<h1>no frontmatter</h1>"), null);
});

test("valid frontmatter has no diagnostics", () => {
  const page = [
    "---",
    "title: Home",
    "layout: ~/layouts/base.html",
    "unknown: passthrough",
    "props:",
    "  title: {type: string, required: true}",
    "brick:",
    "  island: true",
    "  lazy: visible",
    "---",
    "<h1>{{ .Props.title }}</h1>",
  ].join("\n");
  assert.deepStrictEqual(check(page), []);
});

test("an unknown brick: key is flagged on the key itself", () => {
  const page = "---\ntitle: x\nbrick:\n  island: true\n  islnd: true\n---\n<p></p>\n";
  const d = check(page);
  assert.strictEqual(d.length, 1);
  assert.match(d[0].message, /brick\.islnd is not a frontmatter key/);
  assert.deepStrictEqual(d[0].start, { line: 4, character: 2 });
  assert.deepStrictEqual(d[0].end, { line: 4, character: 7 });
  assert.strictEqual(d[0].source, SOURCE);
});

test("a wrong type and a value outside an enum are each one diagnostic at the value", () => {
  const page = "---\nunknown: maybe\nbrick:\n  island: yes please\n---\n";
  const d = check(page).sort((a, b) => a.start.line - b.start.line);
  assert.strictEqual(d.length, 2, JSON.stringify(d));
  assert.match(d[0].message, /^unknown: must be one of "error", "passthrough"/);
  assert.deepStrictEqual(d[0].start, { line: 1, character: 9 });
  assert.match(d[1].message, /^brick\.island: must be boolean/);
  assert.deepStrictEqual(d[1].start, { line: 3, character: 10 });
});

test("several unknown keys are all reported at once", () => {
  const d = check("---\nbrick:\n  a: 1\n  b: 2\n---\n");
  assert.deepStrictEqual(d.map((x) => x.start.line), [2, 3]);
});

test("a YAML syntax error is reported where it is", () => {
  const d = check("---\ntitle: [unclosed\n---\n");
  assert.ok(d.length >= 1);
  assert.strictEqual(d[0].start.line >= 1, true);
});

test("the params of a page are free-form", () => {
  assert.deepStrictEqual(check("---\nauthor: Ada\ntags: [a, b]\nnested: {x: 1}\n---\n"), []);
});
