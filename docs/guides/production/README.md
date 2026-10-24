# Production guides

What to decide before a brick site faces real traffic: where state lives, who the user is, what runs around every action, and what the engine refuses.

| Guide | What it covers |
|---|---|
| [State stores](state-stores.md) | Memory, named stores, `brick.Bytes` over SQL or Redis, and your own |
| [Sealed state and key rings](sealed-state.md) | Keeping state in the page, encrypted, with rotating keys |
| [Sessions and sign-out](sessions.md) | Binding state to a session and revoking it |
| [Action middleware](action-middleware.md) | Code that runs around every action: auth, logging, metrics |
| [Host API](host-api.md) | Calling the engine from your own handlers |
| [Limits and determinism](limits-determinism.md) | What the engine bounds and why the same input gives the same bytes |

Test these with [bricktest](../tooling/bricktest.md) and [statetest](../tooling/statetest.md).
