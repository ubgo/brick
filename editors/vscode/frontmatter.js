// Live checking of a brick file's YAML frontmatter against the schema
// `brick schema` prints (brick.FrontmatterSchema). It needs no VS Code, so
// its tests run in plain Node: extension.js only feeds it documents and
// shows what it returns.
//
// Why here and not only in `brick check`: check runs on save and renders
// the whole site, while a wrong key or value in the frontmatter is
// visible from the file alone, so it can be flagged as it is typed. The
// schema is the same one the language server completes keys from, so
// completion and checking never disagree.
const Ajv2020 = require("ajv/dist/2020").default;
const YAML = require("yaml");

// The frontmatter fence (internal/frontmatter's grammar): "---" alone on
// a line, trailing spaces and tabs allowed, on the file's first line.
const FENCE = /^---[ \t]*\r?$/;
const BOM = "﻿";

// The source every diagnostic carries, so a reader can tell these from
// `brick check`'s.
const SOURCE = "brick frontmatter";

/**
 * The frontmatter block of text: the YAML between the fences and the
 * offset where it starts. null when the file has none, or when the block
 * is never closed (brick check reports that).
 * @param {string} text
 * @returns {{yaml: string, offset: number} | null}
 */
function extract(text) {
  let i = text.startsWith(BOM) ? BOM.length : 0;
  const first = text.indexOf("\n", i);
  if (first < 0 || !FENCE.test(text.slice(i, first))) return null;
  const start = first + 1;
  for (let at = start; at <= text.length; ) {
    let end = text.indexOf("\n", at);
    if (end < 0) end = text.length;
    if (FENCE.test(text.slice(at, end))) return { yaml: text.slice(start, at), offset: start };
    if (end === text.length) return null;
    at = end + 1;
  }
  return null;
}

/**
 * A checker for one schema: compile once, check many documents.
 * @param {object} schema the JSON Schema `brick schema` prints
 */
function checker(schema) {
  // allErrors: report every wrong key at once, not one per keystroke.
  // allowUnionTypes: the schema states "a scalar of any kind" as
  // type: [string, number, boolean], which is plain JSON Schema; Ajv's
  // strict mode only asks that such unions be allowed explicitly.
  const ajv = new Ajv2020({ allErrors: true, allowUnionTypes: true });
  const validate = ajv.compile(schema);
  return (text) => check(text, validate);
}

/**
 * The diagnostics for one document's frontmatter, as 0-based positions.
 * @param {string} text
 * @param {Function} validate a compiled Ajv validator
 * @returns {{start: {line: number, character: number}, end: {line: number, character: number}, message: string, source: string}[]}
 */
function check(text, validate) {
  const block = extract(text);
  if (!block) return [];
  const pos = positions(text);
  const at = (a, b) => ({ start: pos(block.offset + a), end: pos(block.offset + Math.max(a, b)) });
  const doc = YAML.parseDocument(block.yaml, { prettyErrors: false });
  const out = [];
  if (doc.errors.length > 0) {
    // A YAML syntax error: the engine refuses the file, and nothing else
    // can be checked until it is fixed.
    for (const e of doc.errors) out.push({ ...at(e.pos[0], e.pos[1]), message: e.message, source: SOURCE });
    return out;
  }
  const data = doc.toJS();
  if (data === null || data === undefined) return [];
  if (validate(data)) return [];
  for (const e of pick(validate.errors)) {
    const r = rangeOf(doc, e);
    out.push({ ...at(r[0], r[1]), message: describe(e), source: SOURCE });
  }
  return out;
}

// pick keeps one error per value: Ajv explains a failed anyOf with every
// branch's complaint, and a reader wants the one that names the value. A
// value with plain errors (an unknown key, a wrong type) keeps those.
function pick(errors) {
  const byPath = new Map();
  for (const e of errors) {
    const key = e.instancePath + (e.keyword === "additionalProperties" ? "/" + e.params.additionalProperty : "");
    const list = byPath.get(key) || [];
    list.push(e);
    byPath.set(key, list);
  }
  const out = [];
  for (const list of byPath.values()) {
    const plain = list.filter((e) => !e.schemaPath.includes("/anyOf/") && e.keyword !== "anyOf");
    out.push(...(plain.length > 0 ? plain : [list.find((e) => e.keyword === "anyOf") || list[0]]));
  }
  return out;
}

// describe is an error in the frontmatter's own words: the key path, then
// what is wrong.
function describe(e) {
  const path = e.instancePath.split("/").slice(1).map(unescape).join(".");
  const where = path ? path + ": " : "";
  switch (e.keyword) {
    case "additionalProperties":
      return `${path ? path + "." : ""}${e.params.additionalProperty} is not a frontmatter key here`;
    case "enum":
      return `${where}must be one of ${e.params.allowedValues.map((v) => JSON.stringify(v)).join(", ")}`;
    case "anyOf":
      return `${where}is not a value this key takes`;
    default:
      return `${where}${e.message}`;
  }
}

// unescape reads one JSON Pointer segment.
function unescape(seg) {
  return seg.replace(/~1/g, "/").replace(/~0/g, "~");
}

// rangeOf is the offsets, in the YAML text, of what an error is about:
// the unknown key itself, else the value at its path, else the closest
// parent that exists.
function rangeOf(doc, e) {
  const path = e.instancePath.split("/").slice(1).map(unescape).map((s) => (/^\d+$/.test(s) ? Number(s) : s));
  if (e.keyword === "additionalProperties") {
    const map = path.length ? doc.getIn(path, true) : doc.contents;
    const pair = map && map.items && map.items.find((p) => p.key && p.key.value === e.params.additionalProperty);
    if (pair && pair.key.range) return pair.key.range;
  }
  for (let n = path.length; n >= 0; n--) {
    const node = n ? doc.getIn(path.slice(0, n), true) : doc.contents;
    if (node && node.range) {
      // A key whose value is the problem: point at the key and value,
      // which a block value would otherwise stretch over many lines.
      return [node.range[0], node.range[1]];
    }
  }
  return [0, 0];
}

// positions maps an offset in text to a 0-based line and character.
function positions(text) {
  const starts = [0];
  for (let i = 0; i < text.length; i++) if (text[i] === "\n") starts.push(i + 1);
  return (off) => {
    let lo = 0;
    let hi = starts.length - 1;
    while (lo < hi) {
      const mid = (lo + hi + 1) >> 1;
      if (starts[mid] <= off) lo = mid;
      else hi = mid - 1;
    }
    return { line: lo, character: off - starts[lo] };
  };
}

module.exports = { extract, checker, check, SOURCE };
