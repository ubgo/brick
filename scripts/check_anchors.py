#!/usr/bin/env python3
"""Fails when a link in the design site points at an anchor that does not
exist. The design-spec tool's own check (task docs:check) validates pages
and sections but not in-page fragments, so a guide link to #brick_secret
passed while the heading's id was brick-secret.

Checked: every href="#x" and href="page.html#x" in the design site's *.html against
the id= and name= attributes of the target page, and every href="../X.md#x"
against the heading slugs of the design notes' X.md (GitHub-style: lowercase, spaces to
'-', punctuation dropped), since the site links into the design notes that way.

  python3 scripts/check_anchors.py [site-dir]   exit 1 and list each broken link
"""

import html
import pathlib
import re
import sys

HREF = re.compile(r'href="([^"]*#[^"]*)"')
ID = re.compile(r'\s(?:id|name)="([^"]+)"')
HEADING = re.compile(r"^#{1,6}\s+(.*?)\s*#*\s*$")
FENCE = re.compile(r"^\s*(```|~~~)")
# An explicit {#id} at the end of a heading overrides its slug.
EXPLICIT = re.compile(r"\{#([^}]+)\}\s*$")


def slug(text):
    """GitHub's heading slug: what a ../X.md#x link resolves against."""
    text = re.sub(r"<[^>]+>", "", text)
    text = re.sub(r"`", "", text)
    text = text.strip().lower()
    text = re.sub(r"[^\w\- ]", "", text, flags=re.UNICODE)
    return text.replace(" ", "-")


def markdown_anchors(path):
    anchors, seen, fenced = set(), {}, False
    for line in path.read_text(encoding="utf-8").splitlines():
        if FENCE.match(line):
            fenced = not fenced
            continue
        if fenced:
            continue
        m = HEADING.match(line)
        if not m:
            anchors.update(ID.findall(" " + line))
            continue
        title = m.group(1)
        explicit = EXPLICIT.search(title)
        if explicit:
            anchors.add(explicit.group(1))
            title = EXPLICIT.sub("", title)
        base = slug(title)
        n = seen.get(base, 0)
        seen[base] = n + 1
        anchors.add(base if n == 0 else f"{base}-{n}")
    return anchors


def main():
    site = pathlib.Path(sys.argv[1] if len(sys.argv) > 1 else "design/site")
    cache = {}

    def anchors_of(target):
        if target not in cache:
            if not target.exists():
                cache[target] = None
            elif target.suffix == ".md":
                cache[target] = markdown_anchors(target)
            else:
                cache[target] = set(ID.findall(target.read_text(encoding="utf-8")))
        return cache[target]

    broken = []
    for page in sorted(site.glob("*.html")):
        for href in HREF.findall(page.read_text(encoding="utf-8")):
            href = html.unescape(href)
            path, _, frag = href.partition("#")
            if not frag or "://" in path:
                continue
            target = (page.parent / path).resolve() if path else page.resolve()
            found = anchors_of(target)
            if found is None:
                broken.append(f"{page.name}: {href}: {path} does not exist")
            elif frag not in found:
                broken.append(f"{page.name}: {href}: no anchor #{frag} in {target.name}")

    for line in broken:
        print(line)
    if broken:
        print(f"{len(broken)} broken anchor link(s)")
        return 1
    print("anchors: 0 problems")
    return 0


if __name__ == "__main__":
    sys.exit(main())
