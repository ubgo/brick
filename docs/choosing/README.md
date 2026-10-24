# Choosing guides

brick usually offers more than one way to do a thing, and the right one depends on your app. Each page here is a decision aid: a table that compares the options on the questions that matter, "if X then Y" rules, the pitfalls that surprise people, and a link to the guide and the runnable example for every option. Read the one that matches the decision you are facing, then follow its links to build.

| You are deciding… | Read | Options compared |
|---|---|---|
| Where a stateful component's data lives | [Choosing a state store](state-store.md) | memory, sealed in the page, SQL, Redis, your own store; key sources (environment, AWS KMS, Google Cloud KMS, Vault, your own) |
| How a page responds to the visitor | [Choosing how to add interactivity](interactivity.md) | plain pages, no-JavaScript forms, islands, stateful actions, URL state, navigation, client commands, effects and events, push, background jobs |
| How a form is built | [Choosing a form approach](forms.md) | live or deferred fields, no-JavaScript posts, validation rules, proxied or presigned uploads, method links |
| How and where the site is rendered and hosted | [Choosing a rendering and hosting mode](rendering-and-hosting.md) | static export, dev server, site in your binary, engine alone, under a path, per-visitor pages, the render cache |
| Where components come from | [Choosing where components come from](component-sources.md) | your project, a remote file, a library, the UI kit |

## Rules of thumb

- Start with the least machinery: a plain page, then a stateful component, then push. Each step needs more from the browser and the server.
- Production state is sealed or shared, never process memory behind a load balancer.
- A page that is the same for everyone should be cached or exported; use islands for the parts that are not.
- When two options both fit, pick the one with fewer moving parts and a test you can run: every example linked from these pages has a Go test that runs the real handler.

Where to go next: the [learning path](../README.md), the [feature index](../reference/features.md), or [getting started](../getting-started.md).
