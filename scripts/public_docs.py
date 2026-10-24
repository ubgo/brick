#!/usr/bin/env python3
"""Generate the public docs (docs/guide.md, docs/cookbook.md) from the
design site's sources (BRICK_DESIGN_DIR/site/src, set by the Taskfile).

Why: the guide and cookbook are written once, for the private design site,
where they cite spec rule ids and link to private pages. The public copies
drop those and use plain GitHub markdown, so the two never drift: edit the
site source, then run `task docs:public`.
"""
import os
import re
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
SRC = ROOT / os.environ.get("BRICK_DESIGN_DIR", "design") / "site" / "src"
OUT = ROOT / "docs"

# design-site callout kinds -> GitHub alert kinds
ALERTS = {"note": "NOTE", "tip": "TIP", "info": "NOTE", "warn": "WARNING", "danger": "CAUTION"}

HEADER = "<!-- Generated from the design site by `task docs:public`; edit the design site source {name}, not this file. -->\n\n"


def convert(text: str) -> str:
    out = []
    for line in text.split("\n"):
        if re.fullmatch(r"::: ?[a-z]*|:::", line.strip()):
            continue
        if re.fullmatch(r"<!-- group:.*-->", line.strip()):
            continue
        m = re.match(r"^```(\w+) title=\"([^\"]*)\"\s*$", line)
        if m:
            out.append(f"`{m.group(2)}`")
            out.append("")
            out.append("```" + m.group(1))
            continue
        m = re.match(r"^> \[!(\w+)\]\s*(.*)$", line)
        if m:
            out.append(f"> [!{ALERTS.get(m.group(1), 'NOTE')}]")
            if m.group(2):
                out.append(f"> **{m.group(2)}**")
                out.append(">")
            continue
        out.append(line)
    s = "\n".join(out)
    # Rule citations belong to the private design notes.
    s = re.sub(r"\s*\*\*\(([A-Z]\d+(, ?[A-Z]\d+)*)\)\*\*", "", s)
    s = re.sub(r"\s*\(\[[A-Z0-9, ]+\]\(\.\./[A-Z]+\.md[^)]*\)\)", "", s)
    s = re.sub(r" \((?:rule )?[A-Z]\d+(?:[, ]+[A-Z]\d+)*(?:, (?:confirmed|decided by you|added by you)[^)]*)?\)", "", s)
    s = re.sub(r"\((?:rule )?[A-Z]\d+, (?:confirmed|decided by you|added by you)[^)]*\)", "", s)
    s = re.sub(r"\[([^\]]+)\]\(\.\./[A-Z]+\.md[^)]*\)", r"\1", s)
    s = re.sub(r"\[([^\]]+)\]\((?:decisions|rules|why|picture|how-it-works|index)\.html[^)]*\)", r"\1", s)
    s = re.sub(r"\((guide|cookbook)\.html(#[^)]*)?\)", lambda m: f"({m.group(1)}.md{m.group(2) or ''})", s)
    return s


def github_slug(heading: str) -> str:
    """GitHub's anchor for a heading: code spans keep their text, html tags
    go, then everything but letters, digits, '-', '_' and spaces is dropped
    and each space becomes a hyphen, so "a — `b`" is "a--b"."""
    parts = re.split(r"(`+[^`]*`+)", heading)
    h = "".join(x.strip("`") if x.startswith("`") else re.sub(r"<[^>]+>", "", x) for x in parts)
    return re.sub(r"[^\w\- ]", "", h.strip().lower()).replace(" ", "-")


def headings(text: str) -> list[str]:
    out, fence = [], False
    for line in text.split("\n"):
        if line.lstrip().startswith("```"):
            fence = not fence
        elif not fence and re.match(r"^#{1,6} ", line):
            out.append(re.sub(r"^#+ ", "", line).strip())
    return out


def fix_anchors(pages: dict[str, str]) -> dict[str, str]:
    """The design site collapses repeated hyphens in its anchors
    ("passing-values-props"); GitHub does not ("passing-values--props").
    Rewrite links into the guide and the cookbook to GitHub's anchors, so
    the public copies link correctly when read on GitHub."""
    to_github = {}
    for name, text in pages.items():
        for h in headings(text):
            g = github_slug(h)
            to_github[(name, re.sub(r"-+", "-", g).strip("-"))] = g

    def rewrite(page: str, text: str) -> str:
        def sub(m):
            target, anchor = m.group(1) or page, m.group(2)
            return f"]({m.group(1) or ''}#{to_github.get((target, anchor), anchor)})"
        return re.sub(r"\]\((guide\.md|cookbook\.md)?#([\w-]+)\)", sub, text)

    return {name: rewrite(name, text) for name, text in pages.items()}


def main() -> int:
    OUT.mkdir(exist_ok=True)
    pages = {}
    for name in ("guide.md", "cookbook.md"):
        text = (SRC / name).read_text()
        text = re.sub(r"\A---\n.*?\n---\n", "", text, flags=re.S)
        pages[name] = convert(text).strip()
    for name, text in fix_anchors(pages).items():
        (OUT / name).write_text(HEADER.format(name=name) + text + "\n")
        print("wrote", OUT / name)
    return 0


if __name__ == "__main__":
    sys.exit(main())
