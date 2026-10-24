// Pins the highlighting grammar: it tokenizes brick markup through the
// injection the way VS Code does, on top of a stub HTML grammar, so the test
// needs no copy of VS Code's own HTML grammar.
const test = require("node:test");
const assert = require("node:assert");
const fs = require("node:fs");
const path = require("node:path");
const vt = require("vscode-textmate");
const onig = require("vscode-oniguruma");

const injectionPath = path.join(__dirname, "..", "syntaxes", "brick.injection.tmLanguage.json");

// A minimal text.html.basic: plain text plus HTML tags, enough for the
// injection's "L:text.html" selector to attach.
const stubHTML = {
  scopeName: "text.html.basic",
  patterns: [{ name: "meta.tag.html", begin: "</?[a-z][a-z0-9]*", end: ">" }],
};

async function grammar() {
  const wasm = fs.readFileSync(require.resolve("vscode-oniguruma/release/onig.wasm")).buffer;
  await onig.loadWASM(wasm);
  const registry = new vt.Registry({
    onigLib: Promise.resolve({
      createOnigScanner: (p) => new onig.OnigScanner(p),
      createOnigString: (s) => new onig.OnigString(s),
    }),
    loadGrammar: async (scope) => {
      if (scope === "text.html.basic") return vt.parseRawGrammar(JSON.stringify(stubHTML), "stub.json");
      if (scope === "brick.injection") return vt.parseRawGrammar(fs.readFileSync(injectionPath, "utf8"), injectionPath);
      return null;
    },
    getInjections: (scope) => (scope === "text.html.basic" ? ["brick.injection"] : []),
  });
  return registry.loadGrammar("text.html.basic");
}

// scopesOf returns, for each non-blank token text in line, its innermost scope.
function scopesOf(g, line) {
  const out = {};
  for (const t of g.tokenizeLine(line, vt.INITIAL).tokens) {
    const text = line.slice(t.startIndex, t.endIndex);
    if (text.trim()) out[text] = t.scopes[t.scopes.length - 1];
  }
  return out;
}

test("actions, component tags and brick: attributes are scoped", async () => {
  const g = await grammar();
  const s = scopesOf(g, `<div brick:click="save(1)">{{- range $i, $p := .Props.items }}<Card title="Hi"><x-card/><brick:prop name="b">{{ end -}}`);
  assert.equal(s["brick:click"], "entity.other.attribute-name.brick");
  assert.equal(s['"save(1)"'], "string.quoted.brick");
  assert.equal(s["{{-"], "punctuation.section.embedded.begin.brick");
  assert.equal(s["range"], "keyword.control.brick");
  assert.equal(s["$p"], "variable.other.brick");
  assert.equal(s[".items"], "variable.other.property.brick");
  assert.equal(s["Card"], "entity.name.tag.component.brick");
  assert.equal(s["x-card"], "entity.name.tag.component.brick");
  assert.equal(s["brick:prop"], "entity.name.tag.component.brick");
  assert.equal(s["-}}"], "punctuation.section.embedded.end.brick");
});

test("an action inside a component attribute value is highlighted", async () => {
  const g = await grammar();
  const s = scopesOf(g, `<Card title="{{ .Props.t }}">`);
  assert.equal(s[".t"], "variable.other.property.brick");
});

test("plain lowercase HTML tags are left to the HTML grammar", async () => {
  const g = await grammar();
  const s = scopesOf(g, `<div class="a">x</div>`);
  assert.ok(!Object.values(s).some((scope) => scope.endsWith(".brick")), JSON.stringify(s));
});

// The frontmatter injection, over the same stub HTML grammar plus a stub
// source.yaml that scopes a key, so the test sees which lines VS Code's
// YAML grammar would get.
const frontmatterPath = path.join(__dirname, "..", "syntaxes", "brick.frontmatter.tmLanguage.json");
const stubYAML = {
  scopeName: "source.yaml",
  patterns: [{ match: "^\\s*([A-Za-z_][\\w.-]*)(:)", captures: { 1: { name: "entity.name.tag.yaml" } } }],
};

async function frontmatterGrammar() {
  const wasm = fs.readFileSync(require.resolve("vscode-oniguruma/release/onig.wasm")).buffer;
  await onig.loadWASM(wasm).catch(() => {}); // already loaded by an earlier test
  const registry = new vt.Registry({
    onigLib: Promise.resolve({
      createOnigScanner: (p) => new onig.OnigScanner(p),
      createOnigString: (s) => new onig.OnigString(s),
    }),
    loadGrammar: async (scope) => {
      if (scope === "text.html.basic") return vt.parseRawGrammar(JSON.stringify(stubHTML), "stub.json");
      if (scope === "source.yaml") return vt.parseRawGrammar(JSON.stringify(stubYAML), "yaml.json");
      if (scope === "brick.frontmatter") return vt.parseRawGrammar(fs.readFileSync(frontmatterPath, "utf8"), frontmatterPath);
      return null;
    },
    getInjections: (scope) => (scope === "text.html.basic" ? ["brick.frontmatter"] : []),
  });
  return registry.loadGrammar("text.html.basic");
}

// tokenizeDoc returns, per line, each non-blank token's full scope list.
function tokenizeDoc(g, text) {
  let state = vt.INITIAL;
  return text.split("\n").map((line) => {
    const r = g.tokenizeLine(line, state);
    state = r.ruleStack;
    const out = {};
    for (const t of r.tokens) {
      const s = line.slice(t.startIndex, t.endIndex);
      if (s.trim()) out[s.trim()] = t.scopes;
    }
    return out;
  });
}

test("frontmatter on the first line is YAML, the body after it is not", async () => {
  const g = await frontmatterGrammar();
  const lines = tokenizeDoc(g, "---\ntitle: Home\nbrick:\n  island: true\n---\ntitle: body text\n<p>x</p>");
  assert.ok(lines[0]["---"].includes("punctuation.definition.frontmatter.begin.brick"), JSON.stringify(lines[0]));
  assert.ok(lines[1]["title"].includes("entity.name.tag.yaml"), JSON.stringify(lines[1]));
  assert.ok(lines[1]["title"].includes("meta.embedded.block.yaml"));
  assert.ok(lines[3]["island"].includes("entity.name.tag.yaml"));
  assert.ok(lines[4]["---"].includes("punctuation.definition.frontmatter.end.brick"), JSON.stringify(lines[4]));
  assert.ok(!JSON.stringify(lines[5]).includes("yaml"), "a key after the closing fence is HTML text: " + JSON.stringify(lines[5]));
});

test("a --- that is not on the first line starts no frontmatter", async () => {
  const g = await frontmatterGrammar();
  const lines = tokenizeDoc(g, "<p>intro</p>\n---\ntitle: x\n---");
  assert.ok(!JSON.stringify(lines).includes("frontmatter"), JSON.stringify(lines));
});
