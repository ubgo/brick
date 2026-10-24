#!/usr/bin/env python3
"""Check that every relative link in the public docs resolves.

Why: the public docs are read on GitHub, where a renamed heading or a
moved example silently becomes a dead link. This checks every relative
link and #anchor in docs/**/*.md and README.md (or the files given as
arguments): the file must exist, and an anchor into a markdown file must
match one of its headings as GitHub slugs them, including the -1, -2
suffixes of repeated headings and explicit <a id/name> anchors. Links
inside code are not links, and absolute URLs are not checked (no network).

    python3 scripts/check_doc_links.py            # exit 1 when anything is broken
    python3 scripts/check_doc_links.py docs/x.md  # only these files
"""
import re
import sys
from pathlib import Path
from urllib.parse import unquote

ROOT = Path(__file__).resolve().parent.parent
DEFAULT = ["README.md", "docs/**/*.md"]
VENDORED = re.compile(r"/(brick_modules|node_modules)/")

FENCE_RE = re.compile(r"^\s*(```|~~~)")
# [text](target) and ![alt](target); the target ends at whitespace or ')'.
INLINE_RE = re.compile(r"!?\[(?:[^\[\]]|\\[\[\]]|\[[^\]]*\])*\]\(\s*<?([^)\s>]+)>?(?:\s+\"[^\"]*\")?\s*\)")
REF_RE = re.compile(r"^\s{0,3}\[[^\]]+\]:\s*<?(\S+?)>?(?:\s+.*)?$")
CODE_SPAN_RE = re.compile(r"(`+)(?:(?!\1).)+?\1")
HTML_ANCHOR_RE = re.compile(r"<a\s[^>]*(?:id|name)=\"([^\"]+)\"", re.I)
HTML_HREF_RE = re.compile(r"<a\s[^>]*href=\"([^\"]+)\"", re.I)


def slug(heading: str) -> str:
    """GitHub's anchor for a heading: the rendered text, lowercased, with
    everything but letters, digits, '-', '_' and spaces dropped, and
    spaces turned into hyphens (so 'a — b' becomes 'a--b')."""
    h = re.sub(r"!?\[([^\]]*)\]\([^)]*\)", r"\1", heading)  # links keep their text
    # Inline html is dropped, but a code span's text is kept: `<brick:prop>`
    # slugs as "brickprop".
    parts = re.split(r"(`+[^`]*`+)", h)
    h = "".join(x.strip("`") if x.startswith("`") else re.sub(r"<[^>]+>", "", x) for x in parts)
    h = h.strip().lower()
    h = re.sub(r"[^\w\- ]", "", h)
    return h.replace(" ", "-")


def outside_fences(lines):
    """Yield (line number, text) for the lines outside fenced code."""
    in_fence = False
    for n, line in enumerate(lines, 1):
        if FENCE_RE.match(line):
            in_fence = not in_fence
            continue
        if not in_fence:
            yield n, line


def strip_code(lines):
    """Like outside_fences, with code spans blanked: a link in code is not a link."""
    for n, line in outside_fences(lines):
        yield n, CODE_SPAN_RE.sub("", line)


_anchor_cache: dict[Path, set[str]] = {}


def anchors(path: Path) -> set[str]:
    if path not in _anchor_cache:
        seen: dict[str, int] = {}
        out: set[str] = set()
        for _, line in outside_fences(path.read_text(errors="replace").split("\n")):
            m = re.match(r"^\s{0,3}(#{1,6})\s+(.*?)\s*#*\s*$", line)
            if m:
                s = slug(m.group(2))
                k = seen.get(s, 0)
                seen[s] = k + 1
                out.add(s if k == 0 else f"{s}-{k}")
            for a in HTML_ANCHOR_RE.findall(line):
                out.add(a)
        _anchor_cache[path] = out
    return _anchor_cache[path]


def targets(path: Path):
    for n, line in strip_code(path.read_text(errors="replace").split("\n")):
        for m in INLINE_RE.finditer(line):
            yield n, m.group(1)
        m = REF_RE.match(line)
        if m:
            yield n, m.group(1)
        for href in HTML_HREF_RE.findall(line):
            yield n, href


def check(path: Path) -> list[str]:
    problems = []
    for n, target in targets(path):
        if re.match(r"^[a-z][a-z0-9+.-]*:", target, re.I):  # http:, mailto:, …
            continue
        file_part, _, anchor = target.partition("#")
        dest = path if not file_part else (path.parent / unquote(file_part)).resolve()
        where = f"{path.relative_to(ROOT)}:{n}"
        if not dest.exists():
            problems.append(f"{where}: {target}: no such file")
            continue
        if anchor and dest.suffix == ".md" and dest.is_file():
            if unquote(anchor).lower() not in anchors(dest) and unquote(anchor) not in anchors(dest):
                problems.append(f"{where}: {target}: no heading #{anchor} in {dest.relative_to(ROOT) if dest.is_relative_to(ROOT) else dest}")
    return problems


def main(argv: list[str]) -> int:
    files: list[Path] = []
    for pattern in argv or DEFAULT:
        # Vendored copies (brick_modules/, node_modules/) are someone else's docs.
        files += sorted(p for p in ROOT.glob(pattern) if p.is_file() and not VENDORED.search(p.as_posix()))
    problems = [p for f in files for p in check(f)]
    for p in problems:
        print(p)
    print(f"{len(files)} files, {len(problems)} broken links")
    return 1 if problems else 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
