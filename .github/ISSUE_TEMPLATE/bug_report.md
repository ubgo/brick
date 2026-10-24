---
name: Bug report
about: A page rendered the wrong HTML, a component misbehaved, or the site layer or CLI failed
title: "[bug] "
labels: bug
---

**What happened**

A clear description of the bug.

**The smallest input that shows it**

The page and components, reduced as far as they still reproduce. Files are easier to act on than a description:

```html
<!-- content/a.html -->
<Card title="Hi">body</Card>
```

```html
<!-- components/Card.html -->
<div class="card">{{ .Props.title }}{{ children }}</div>
```

**What you got, and what you expected**

The rendered HTML, the error, or what the browser did. For a kit component, say whether JavaScript was on.

**Environment**

- Module and version: `github.com/ubgo/brick vX.Y.Z`, and any contrib module
- Go version, from `go version`
- For browser behaviour: the browser and its version
- OS and architecture

**Does `brick check` report anything?**

Paste its output; it names the file and line for most problems.
