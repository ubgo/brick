# Security Policy

## Reporting a vulnerability

**Please do not report security vulnerabilities through public GitHub issues.**

Instead, report them privately:

- Use **[GitHub Security Advisories](https://github.com/ubgo/brick/security/advisories/new)** (preferred), or
- Email **khanakia@gmail.com** with the details.

Please include:

- A description of the vulnerability and its impact.
- Steps to reproduce (proof-of-concept if possible).
- Affected version(s) and environment.

We will acknowledge your report within a few days and keep you updated on remediation. We ask that you give us a reasonable window to release a fix before any public disclosure, and we're happy to credit you in the advisory.

## Supported versions

Security fixes are applied to the latest released version. Older versions may not receive patches; please upgrade to stay supported.

| Version | Supported |
|---|---|
| latest `v0.x` | ✅ |
| any earlier `v0.x` | ❌ |

This module is pre-GA. Under SemVer a `v0.x` release may break, so the only supported version is the newest one; fixes are not backported to an earlier `v0.x`.

## Scope

brick's security model is that **every file is trusted code and only data is untrusted.** Pages, layouts, components and scripts are like Go source; render props, frontmatter values, data files, loader results, request values and action arguments are data. These reports are in scope and treated as vulnerabilities:

- Data reaching the page as markup without passing through `{{ sanitize }}`, or escaping that `html/template` would have applied being bypassed.
- A path escaping `Options.FS`, or data choosing which file is read (`src=` must be literal, `<brick:render>` takes names only, scripts cannot open files).
- A script reaching the network, the clock, the file system or the database without the capability the host granted, or escaping its step and time limits.
- Tampering with an action's server-held state: forging or replaying a state handle or token, calling an action the component does not define, or passing a value the action did not receive from its arguments.
- A remote import (`brick vendor`) whose content can change without `brick.lock` detecting it, or rendering that reaches the network.
- A path in `site.Options.BasePath` hosting that serves files outside the site or rewrites a URL to another origin.

Out of scope: templates, components and scripts written by the site's own authors (they are code, and can do anything code can), capabilities a host deliberately grants (`db`, `http`, custom `ScriptGlobals`), and HTML a host explicitly marks safe.
