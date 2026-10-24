# Sealed state, key rotation and sign-out

A counter whose state is sealed into the page with the keys in `BRICK_SECRET` (`brick.KeysFromEnv`, `NewAESGCMSealer`, `NewSealedStates`). The server keeps nothing, so the count survives a restart. Each visitor's state is bound to a session cookie (`Options.Sessions`), and `POST /signout` refuses every blob that session was issued before (`SealedOptions.Revoked`). The tests cover a restart, a key rotation (`new,old` opens, `new` alone answers 410), another session replaying a blob, sign-out, and a missing or malformed secret.

Run it from `examples/`:

```sh
BRICK_SECRET=$(brick keygen) go run ./production-sealed-state
```

Then open http://localhost:8084/.

Test it:

```sh
go test ./production-sealed-state/
```

Guide: [Sealed state and key rings](../../docs/guides/production/sealed-state.md), [Sessions and sign-out](../../docs/guides/production/sessions.md).
