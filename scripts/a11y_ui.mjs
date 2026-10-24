// Accessibility audit of the kit showcase (examples/ui) with axe-core in a
// real headless Chrome: every page linked from the home page (all component
// docs and demo apps) plus a few that no link reaches, in light and dark.
//
// Usage: node scripts/a11y_ui.mjs http://127.0.0.1:18300 [report.json]
// A11Y_ONLY=<regexp> limits the run to matching page paths:
//   A11Y_ONLY='^/apps/(landing|auth)/$' task ui:a11y
// (scripts/a11y_ui.sh serves the showcase and runs it.)
//
// axe-core is fetched from the CDN on each run (AXE_URL), so the repository
// has no npm dependency for it; set AXE_FILE to use a local copy offline.
// Prints the violations grouped by rule and impact, writes every node to the
// report when a path is given, and exits non-zero when any serious or
// critical violation is left that the ALLOW list below does not explain.
import { readFileSync, writeFileSync } from "node:fs";
import { openChrome, sleep } from "../runtime/test/cdp.mjs";

const base = (process.argv[2] || "http://127.0.0.1:18300").replace(/\/$/, "");
const reportPath = process.argv[3] || "";
const AXE_URL = process.env.AXE_URL || "https://cdn.jsdelivr.net/npm/axe-core@4/axe.min.js";
// Pages no link on the home page reaches.
const EXTRA = ["/", "/apps/cms/blog/", "/apps/cms/blog/post/?slug=islands-explained", "/apps/cms/blog/post/?slug=no-such-post"];
// FAILING are the impacts that fail the run; moderate and minor are reported.
const FAILING = new Set(["serious", "critical"]);
// ALLOW lists findings judged false positives, each with its reason. A
// match needs the rule, a page prefix and a substring of the node's target.
const ALLOW = [
  { rule: "color-contrast", page: "/apps/landing/", target: ".land-logos", why: "customer logotypes, drawn faded on purpose; WCAG 1.4.3 exempts logotypes" },
  { rule: "aria-allowed-role", page: "/", target: ".bk-dropdown__trigger", why: "Menubar gives its DropdownMenus' <summary> role=menuitem: the WAI-ARIA menubar needs menuitems, and <details>/<summary> is what makes each menu work without JavaScript; browsers and screen readers honour the role, only ARIA-in-HTML's element table objects" },
];
// SETTLE_MS is how long a page gets after navigation before axe runs: its
// client modules mount and enhance the markup (hidden controls appear, roles
// change), and the audit must see the page a person sees.
const SETTLE_MS = 800;

const axe = process.env.AXE_FILE ? readFileSync(process.env.AXE_FILE, "utf8") : await (await fetch(AXE_URL)).text();
if (!axe.includes("axe")) throw new Error(`could not load axe-core from ${AXE_URL}`);
const home = await (await fetch(base + "/")).text();
// A11Y_ONLY=<regexp> audits only the pages whose path matches, for a quick
// check of the pages a change touched (a full run takes minutes).
const only = process.env.A11Y_ONLY ? new RegExp(process.env.A11Y_ONLY) : null;
const paths = [...new Set([...home.matchAll(/href="(\/[^"#?]*)"/g)].map((m) => m[1]).filter((p) => !p.startsWith("/_brick/")).concat(EXTRA))].filter((p) => !only || only.test(p)).sort();

const cdp = await openChrome({ profilePrefix: "brick-a11y-" });
const { send } = cdp;
await send("Emulation.setDeviceMetricsOverride", { width: 1280, height: 900, deviceScaleFactor: 1, mobile: false });

// A page is audited once it has loaded and its client modules have run.
const run = `(async () => { ${axe}
  ; const res = await axe.run(document, { resultTypes: ["violations"] });
  return res.violations.map((v) => ({ id: v.id, impact: v.impact, help: v.help, nodes: v.nodes.map((n) => ({ target: n.target.join(" "), summary: (n.failureSummary || "").slice(0, 400), html: n.html.slice(0, 200) })) })); })()`;
const found = [];
for (const theme of ["light", "dark"]) {
  await send("Emulation.setEmulatedMedia", { features: [{ name: "prefers-color-scheme", value: theme }, { name: "prefers-reduced-motion", value: "reduce" }] });
  for (const p of paths) {
    await send("Page.navigate", { url: base + p });
    await sleep(SETTLE_MS);
    const r = await send("Runtime.evaluate", { expression: run, awaitPromise: true, returnByValue: true });
    const v = r.result && r.result.value;
    if (!Array.isArray(v)) { found.push({ page: p, theme, id: "audit-error", impact: "critical", nodes: [{ target: "", summary: JSON.stringify(r.exceptionDetails).slice(0, 300), html: "" }] }); continue; }
    for (const x of v) found.push({ page: p, theme, ...x });
  }
}
cdp.close();

const allowed = (f, n) => ALLOW.find((a) => a.rule === f.id && f.page.startsWith(a.page) && n.target.includes(a.target));
const byRule = new Map();
let failing = 0;
for (const f of found) {
  for (const n of f.nodes) {
    const a = allowed(f, n);
    const key = `${f.impact.padEnd(8)} ${f.id}${a ? " (allowed)" : ""}`;
    const e = byRule.get(key) || { nodes: 0, pages: new Set(), example: `${f.page} [${f.theme}] ${n.target}` };
    e.nodes++;
    e.pages.add(`${f.page} ${f.theme}`);
    byRule.set(key, e);
    if (!a && FAILING.has(f.impact)) failing++;
  }
}
if (reportPath) writeFileSync(reportPath, JSON.stringify(found, null, 1));
console.log(`${axe.match(/axe v[\d.]+/)?.[0] || "axe"}: ${paths.length} pages × 2 themes`);
for (const [k, e] of [...byRule].sort()) console.log(`${k}  ${e.nodes} nodes on ${e.pages.size} page-themes  e.g. ${e.example}`);
for (const a of ALLOW) console.log(`allowed: ${a.rule} on ${a.page} ${a.target} — ${a.why}`);
if (failing) { console.log(`FAIL: ${failing} serious or critical nodes`); process.exit(1); }
console.log("a11y ui ok");
process.exit(0);
