#!/usr/bin/env python3
"""Keep whole-file snippets in the GitHub docs identical to the tested files.

docsync (`ds:block`) embeds Go declarations, but it cannot copy a brick
component's YAML frontmatter, and a component without its frontmatter is not
the code a reader needs. So a page that shows a whole file marks the fenced
block with the file it was copied from:

    <!-- snippet: examples/forms-signup/theme/components/Signup.html -->
    ```html
    ...the exact file contents...
    ```

An optional `lines=A-B` after the path shows only those lines (1-based,
inclusive). The check fails when a marked block differs from its source, so a
snippet can never drift from the example the test suite runs. `--fix` rewrites
every marked block from its source.

Usage: check_doc_snippets.py [--fix] [root]
"""
import pathlib
import re
import sys

# A marker line, then the fenced block that must follow it directly.
MARKER = re.compile(r"^<!-- snippet: (?P<path>\S+)(?: lines=(?P<a>\d+)-(?P<b>\d+))? -->$")
FENCE = re.compile(r"^(?P<fence>`{3,}|~{3,})(?P<lang>\S*)\s*$")
# Pages that carry snippets; the generated guide and cookbook have their own executed tests.
DOC_GLOBS = ["docs/**/*.md", "README.md", "examples/**/README.md", "contrib/*/README.md"]
SKIP_PARTS = {"node_modules", "brick_modules", "testdata"}


def source_text(root, rel, a, b):
    """The text a marked block must hold, or an error message."""
    path = (root / rel).resolve()
    if root.resolve() not in path.parents:
        return None, f"source {rel} is outside the repository"
    if not path.is_file():
        return None, f"source {rel} does not exist"
    lines = path.read_text(encoding="utf-8").splitlines()
    if a is not None:
        a, b = int(a), int(b)
        if not 1 <= a <= b <= len(lines):
            return None, f"lines={a}-{b} is outside {rel} ({len(lines)} lines)"
        lines = lines[a - 1 : b]
    return lines, None


def check_page(root, page, fix):
    """Problems in one page; rewrites it in place when fix is set."""
    lines = page.read_text(encoding="utf-8").splitlines()
    out, problems, i, changed = [], [], 0, False
    rel_page = page.relative_to(root)
    while i < len(lines):
        out.append(lines[i])
        m = MARKER.match(lines[i])
        if not m:
            i += 1
            continue
        where = f"{rel_page}:{i + 1}"
        if i + 1 >= len(lines) or not FENCE.match(lines[i + 1]):
            problems.append(f"{where}: snippet marker is not followed directly by a fenced code block")
            i += 1
            continue
        fence = FENCE.match(lines[i + 1]).group("fence")
        end = next((j for j in range(i + 2, len(lines)) if lines[j].strip() == fence), None)
        if end is None:
            problems.append(f"{where}: the fenced block after the snippet marker is never closed")
            i += 1
            continue
        want, err = source_text(root, m.group("path"), m.group("a"), m.group("b"))
        have = lines[i + 2 : end]
        if err:
            problems.append(f"{where}: {err}")
        elif have != want:
            if fix:
                changed = True
            else:
                problems.append(f"{where}: block differs from {m.group('path')} (run with --fix to copy the source in)")
        out.append(lines[i + 1])
        out.extend(want if (fix and not err) else have)
        out.append(lines[end])
        i = end + 1
    if changed:
        page.write_text("\n".join(out) + "\n", encoding="utf-8")
    return problems, changed


def main(argv):
    fix = "--fix" in argv
    args = [a for a in argv if a != "--fix"]
    root = pathlib.Path(args[0] if args else ".").resolve()
    pages = sorted({p for g in DOC_GLOBS for p in root.glob(g) if not SKIP_PARTS & set(p.parts)})
    problems, fixed, blocks = [], 0, 0
    for page in pages:
        blocks += sum(1 for l in page.read_text(encoding="utf-8").splitlines() if MARKER.match(l))
        p, changed = check_page(root, page, fix)
        problems += p
        fixed += changed
    for p in problems:
        print(p)
    print(f"{len(pages)} pages, {blocks} snippets, {len(problems)} problems" + (f", {fixed} pages rewritten" if fix else ""))
    return 1 if problems else 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
