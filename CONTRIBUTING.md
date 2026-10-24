# Contributing to brick

## The gate

```sh
task js:deps   # once: installs jsdom for the browser-runtime tests (they skip without it)
task check
```

`task check` runs gofmt, `go vet` and staticcheck over the root module and every other module (`contrib/*`, `examples`, `cmd/brick`), builds for Linux, macOS, Windows and FreeBSD, and runs every test suite with the race detector. Everything must be green before a change is proposed. `task --list` shows every command with a one-line description.

CI exists but does not run on push. This repository is private, where GitHub Actions minutes are metered, so the workflows are `workflow_dispatch` only and `task check` is the real gate. The exact trigger lines to restore automatic runs are in a comment at the top of each workflow.

## The browser suites

The Go tests render HTML and the jsdom tests drive the client modules, but some behaviour only exists in a real browser: morphing, navigation, focus, popovers, dialogs. These suites start a server, drive it in headless Google Chrome (set `CHROME=/path/to/chrome` if it is not in the default place) and stop it again. Run the ones that cover your change:

| Command | What it checks |
|---|---|
| `task e2e` | The engine demo site: actions, bindings, navigation |
| `task ui:e2e` | The kit showcase and every demo app |
| `task e2e:both` | Both examples mounted under `/ui/` and `/engine/` on one server, which exercises sub-path hosting |
| `task test:v21:chrome` | The v21 runtime features (window focus, loading targets, enter transitions, `brick.js`, `brick.find`, `flash-clear`) against a small fixture site |
| `task test:v22:chrome` | The v22 runtime features (Modelable, `brick.transition`, `brick:keydown.norepeat`, `brick:click.details`) against the kit showcase |
| `task ui:a11y` | An axe-core audit of the showcase; fails on serious or critical findings. `A11Y_ONLY=<regexp>` audits only the matching pages |

## Working on the kit

The showcase in `examples/ui` uses the repository's `kit/` directly, as the embedded library `brick-kit` (`kit.Library()`), so a change shows up with no copy step. After changing anything under `kit/`:

```sh
task ui:test      # the kit's lint and jsdom tests, then the showcase tests
task kit:catalog  # regenerate the component catalog in kit/README.md when you add or rename a component
```

`task ui:serve` serves the showcase with live reload on http://localhost:8002. Every component follows the authoring rules in [kit/README.md](kit/README.md); `kit_lint_test.go` enforces the ones a test can check.

## The rules a change is reviewed against

**A calling convention, never a new template language.** Everything inside `{{ }}` is Go's own `html/template`. brick adds tags, props, blocks and a few built-ins; a change that needs new syntax inside actions is the wrong change.

**Nothing required, everything overridable.** `<h1>Hello</h1>` with no frontmatter is a complete page. When two things disagree, one wins and brick reports what it shadowed; it does not forbid the combination.

**The no-escape invariant.** An attribute holds text or a reference. Anything with angle brackets, quotes or newlines goes in a `<brick:prop>` block. No value should ever need escaping.

**Every file is trusted code; only data is untrusted.** Paths start at the project root and never leave `Options.FS`. Data never chooses a file. Scripts never open a file while they run, and they reach the outside world only through capabilities the host grants.

**Same input, same bytes.** Output is deterministic, and an `*Engine` is safe for concurrent renders. A test that renders twice and compares is the usual proof.

**The root module names no vendor.** Scripting, markdown and sanitizing enter through interfaces; each implementation is its own module under `contrib/`.

**Progressive by default.** A kit component is complete, usable HTML without JavaScript. A client module enhances markup that is already there and never builds it.

## Tests

New behaviour needs a test that would fail without it. A change to rendering needs an assertion on the exact output. A change to a client module needs a jsdom test, and a change that only a real browser shows needs a case in the matching browser suite. `task golden:update` rewrites `bricktest` golden files after an intended output change; say in the pull request why the output changed.

Never suppress a diagnostic to make a gate pass. No `//nolint`, no loosened assertion, no skipped test. Fix the cause, or say why the tool is wrong and leave it failing for review.

## Docs

`docs/guide.md` and `docs/cookbook.md` are generated from the design sources the maintainers keep (`task docs:public`), so do not edit them by hand: describe the documentation change in the pull request and it is folded into the sources. A user-facing change updates the README and the CHANGELOG in the same pull request.

## Commits

Describe what changed and why. The repository history is read by people deciding whether a behaviour was deliberate.
