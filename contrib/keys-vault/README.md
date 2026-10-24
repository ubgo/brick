# brick/contrib/keys-vault

A `brick.KeyRing` whose state keys are protected by HashiCorp Vault's transit engine, so the sealing keys no longer sit in a `BRICK_SECRET` environment variable in plain form.

**How it works: envelope encryption.** Each 32-byte state key (a data key) is stored *wrapped* by a transit key (`vault:v1:…`). The wrapped form is not a secret: it can live in config, an environment variable or a repo. At start, `New` asks Vault to unwrap each data key once and builds an ordinary `brick.StaticKeys` ring from the results. Sealing and opening then run locally on brick's AES-GCM sealer, so no request waits on Vault, and a Vault outage or an expired token after start changes nothing. What Vault adds over a plain secret: the wrapped form is useless without a token whose policy allows `transit/decrypt/<key>`, every unwrap is in Vault's audit log, and revoking the policy stops new servers from starting.

## Use it

```go
vc, err := api.NewClient(api.DefaultConfig()) // VAULT_ADDR; authenticate your way (token, AppRole, Kubernetes, …)
if err != nil { return err }

wrapped, err := keysvault.ParseWrapped(os.Getenv("BRICK_WRAPPED_KEYS")) // "vault:v2:…,vault:v1:…"
if err != nil { return err }
keys, err := keysvault.New(ctx, vc.Logical(), keysvault.Options{
    KeyName: "brick-state",
    Wrapped: wrapped,
})
if err != nil { return err } // never start without the key
sealer := brick.NewAESGCMSealer(keys, nil)
states, err := brick.NewSealedStates(brick.SealedOptions{Sealer: sealer})
if err != nil { return err }
eng, err := brick.New(brick.Options{FS: fsys, States: states})
```

Runnable version: `Example` builds the ring over an offline stand-in for `client.Logical()` (the `Writer` interface), seals a stateful component into a rendered page and opens it again; `ExampleNew_notCiphertext` shows a value that is not `vault:v…` refused before Vault is asked. Run it with `go test -run Example .` in this folder ([example_test.go](example_test.go)).

Guide: [sealed-state.md](../../docs/guides/production/sealed-state.md#keys-from-a-kms).

The host builds and authenticates the client; this module only calls `WriteWithContext` on `client.Logical()` (the `Writer` interface), so any auth method works and a test can stand in.

| Option | Default | Meaning |
|---|---|---|
| `Mount` | `transit` | the transit engine's mount path |
| `KeyName` | required | the transit key the data keys are wrapped with |
| `Wrapped` | required | the wrapped data keys, newest first: the first seals, every one opens |
| `Context` | none | the derivation context, for a transit key created with `derived=true`; must match the one used to wrap |

**Making a data key.** `NewWrappedKey(ctx, vc.Logical(), keysvault.Options{KeyName: "brick-state"})` calls `transit/datakey/wrapped/<key>` with 256 bits and returns the wrapped key: the plaintext never leaves Vault. The CLI does the same: `vault write -field=ciphertext transit/datakey/wrapped/brick-state bits=256`.

**Rotation.** Same as `StaticKeys`: add a new wrapped key second and deploy, promote it to first in a later deploy, and drop the old one a state TTL after that. Rotating the transit key itself (`vault write -f transit/keys/brick-state/rotate`) needs no change: Vault still unwraps keys wrapped by older versions until `min_decryption_version` moves past them (the live test checks this).

**Failure is a hard stop.** No client, no `KeyName`, no wrapped key, a value that is not a transit ciphertext, a Vault error (permission denied, unknown key, wrong context, sealed Vault), or a data key that does not unwrap to 32 bytes: each makes `New` return an error and no ring. Nothing falls back to running without a key. Errors name the data key's position (`data key 2`), never its bytes or its ciphertext.

**Memory.** The decoded plaintexts are zeroed once the ring holds its own copies, on every path out of `New`. The base64 string inside Vault's JSON response is an immutable Go string and is left to the garbage collector. The ring keeps the keys in process memory for its lifetime, exactly like a `BRICK_SECRET` ring: envelope encryption protects the keys at rest, not from the running process.

**Policy.** The server's token needs `update` on `transit/decrypt/brick-state`. Only the person or job that makes data keys needs `update` on `transit/datakey/wrapped/brick-state`.

## Tests

`go test ./...` needs no network: it runs against an in-process fake and checks rotation, the request paths, every refusal path and that errors carry no key material. `task test:keys:vault` starts a throwaway `vault server -dev` from the repo's `compose.yml` (profile `vault`, `127.0.0.1:18200`), runs the live round trip (wrap, rotate the transit key, wrap again, unwrap both, seal, open) and the live refusals (another transit key, wrong and missing derivation context, a token without permission, an unknown key), and removes the container (`BRICK_VAULT_TEST=1`; `BRICK_VAULT_ADDR` and `BRICK_VAULT_TOKEN` override the defaults).
