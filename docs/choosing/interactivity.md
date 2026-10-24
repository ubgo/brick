# Choosing how to add interactivity

brick offers several ways to make a page respond, from none at all to a live stream of changes pushed from the server. They differ in what they need from the visitor's browser and from your server, so start with the cheapest one that does the job. This page ranks them; each row links to the guide that builds it.

## The decision

| You want to… | Use | Needs the runtime (JavaScript) | Needs state and a store | Guide | Example |
|---|---|---|---|---|---|
| Show a page, link between pages | plain pages and links | no | no | [Pages and routes](../guides/site/pages-and-routes.md) | [site-blog](../../examples/site-blog/) |
| Submit a form and see the result | a form with `brick:submit` posted as plain HTML | no, it rewrites to `method="post"` | yes, a stateful component | [Forms without JavaScript](../guides/forms/without-javascript.md) | [forms-nojs](../../examples/forms-nojs/) |
| Send DELETE or PUT from a link | `brick:method` on an `<a href>` | no, the link falls back to a plain GET | no | [Method links](../guides/forms/method-links.md) | [methodlinks](../../examples/methodlinks/) |
| Personalise part of a cached page ("Hi, Sara") | an island (`brick: {island: true}`) | no | no | [Islands, lazy and streaming](../guides/interactivity/islands-lazy-and-streaming.md) | [interactivity-lazy-stream](../../examples/interactivity-lazy-stream/) |
| Load a slow part after the page | a lazy island (`brick:lazy`) | yes | no | [Islands, lazy and streaming](../guides/interactivity/islands-lazy-and-streaming.md) | [interactivity-lazy-stream](../../examples/interactivity-lazy-stream/) |
| Send a slow part in the same response | a streamed island (`brick:lazy="stream"`) | no (without it the fill lands at the end of the body) | no | [Islands, lazy and streaming](../guides/interactivity/islands-lazy-and-streaming.md) | [interactivity-lazy-stream](../../examples/interactivity-lazy-stream/) |
| Change something on the server and update one component | a stateful component with an action (`state:`, `brick:click`) | yes | yes | [Stateful components](../guides/interactivity/stateful-components.md) | [interactivity-counter](../../examples/interactivity-counter/) |
| Keep a filter or page number in the URL | `query:` and `params:` fields | yes | yes | [State in the URL](../guides/interactivity/url-and-server-state.md) | [interactivity-url-state](../../examples/interactivity-url-state/) |
| Move between pages without a flicker | navigation (`navigation: true`) | yes | no | [Navigation without reloads](../guides/interactivity/navigation.md) | [interactivity-navigation](../../examples/interactivity-navigation/) |
| Open a menu, toggle a class, animate a change | client commands (`brick:js.<event>`), morph controls, transitions | yes | no | [Morph controls, transitions and client commands](../guides/interactivity/morph-transitions-client-commands.md) | [interactivity-client-commands](../../examples/interactivity-client-commands/) |
| Run your own browser code for one component | `<brick:client>`, `brick.mount` | yes | no | [Styles and client code](../guides/components/styles-and-client.md) | [components-styles-client](../../examples/components-styles-client/) |
| Make an action flash a message, redirect, or download | effects | yes | yes | [Effects](../guides/live/effects.md) | [live-effects-events](../../examples/live-effects-events/) |
| Tell another component on the page that something changed | events (`brick: {on:}`) | yes | yes | [Events](../guides/live/events.md) | [live-effects-events](../../examples/live-effects-events/) |
| Update every open page when data changes elsewhere | server push (`brick: {listen:}`) | yes | yes, plus `Options.Push` | [Push and presence](../guides/live/push-and-presence.md) | [live-push-presence](../../examples/live-push-presence/) |
| Run slow work and refresh when it ends | background jobs | yes | yes, plus `Options.Jobs` | [Background jobs](../guides/live/background-jobs.md) | [live-jobs](../../examples/live-jobs/) |

## If X then Y

- **The page does not change after load:** plain pages. Nothing here is needed, and the page works with scripts off.
- **Only the visitor's name or cart count differs:** an island. The page around it stays cached and shared; only the island renders per request. A stateful component is an island automatically.
- **The change has to be saved on the server:** a stateful component. If it has to survive a restart or a second server, pair it with a [state store](state-store.md).
- **The change is purely visual and the server need not know:** client commands or morph controls. They send no request, and the command list is closed, so a template value cannot make them run code.
- **A form must work when scripts fail or are blocked:** use `brick:submit` and let the engine rewrite it to a plain post. See [choosing a form approach](forms.md).
- **Two components show the same number:** one component dispatches an event and the other listens, instead of sharing state. Events stay on one page; for other visitors' pages use push.
- **Other people change the data:** push. A webhook, another user and a background job all publish a topic, and listening components re-render. Several servers need `contrib/push-redis`.
- **A multi-page site feels slow to navigate:** navigation, then `brick:prefetch` on the links people actually click.

## Pitfalls

- **Forgetting `{{ assets }}`.** The runtime is linked there. Leave it out and stateful components render, but no button does anything.
- **A stateful component or lazy island makes the page private.** The page is rendered per visitor because its handles and tickets belong to one session, so the page cannot be shared from a CDN. Render the shared page once and fill islands per request, or let the site layer do both.
- **`brick.yaml` is read only by the `brick` command.** A Go host that wants navigation sets `Options.Navigation` or loads the config itself, or links do full loads.
- **A server-marked active link defeats fragment navigation.** Mark the current link with `brick:current` instead, so the layout bytes stay equal between pages.
- **Search engines see the placeholder of a lazy island.** Use lazy islands below the fold; `brick build` renders them in place for an exported site.
- **Client commands cannot run code.** `setAttr` refuses event handlers and script URLs, and `unknown command` is a compile error. If you need logic, write an action or a `brick.mount` module.

Back to [choosing guides](README.md). The full feature list is in the [feature index](../reference/features.md).
