<!-- Thanks for contributing to brick! -->

## What & why

<!-- What does this change and why? Link the issue it closes. -->
Closes #

## Type of change

- [ ] Bug fix
- [ ] New feature
- [ ] Kit component (new or changed)
- [ ] Refactor / internal
- [ ] Docs
- [ ] Other:

## Checklist

- [ ] `task check` is green: gofmt, vet and staticcheck in every module, the cross-builds, and every test suite with the race detector.
- [ ] New behaviour has a test that would fail without it: an assertion on the exact output for a rendering change, a jsdom test for a client module.
- [ ] Browser-only behaviour is covered by the matching suite, and it passes: `task e2e`, `task ui:e2e` or `task e2e:both`.
- [ ] For a kit change: `task ui:test` passes, the component follows the authoring rules in `kit/README.md`, it works without JavaScript, and its showcase page shows it. `task ui:a11y` passes, and `task kit:catalog` was run if a component was added or renamed.
- [ ] Output stays deterministic: the same input renders the same bytes.
- [ ] No suppressed diagnostics. No `//nolint`, no skipped test, no loosened assertion.
- [ ] README and CHANGELOG updated for anything user-facing.
- [ ] No unrelated changes or formatting churn.
