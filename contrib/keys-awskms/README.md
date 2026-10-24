# brick/contrib/keys-awskms

A `brick.KeyRing` whose state keys are protected by AWS KMS, so the sealing keys no longer sit in a `BRICK_SECRET` environment variable in plain form.

**How it works: envelope encryption.** Each 32-byte state key (a data key) is stored *encrypted* under a KMS key. The ciphertext is not a secret: it can live in config, an environment variable or a repo. At start, `New` asks KMS to decrypt each data key once and builds an ordinary `brick.StaticKeys` ring from the results. Sealing and opening then run locally on brick's AES-GCM sealer, so no request waits on KMS, no request costs a KMS call, and a KMS outage after start changes nothing. What KMS adds over a plain secret: the stored form is useless without IAM permission to `kms:Decrypt`, every decrypt shows in CloudTrail, and revoking the permission stops new servers from starting.

## Use it

```go
cfg, err := config.LoadDefaultConfig(ctx) // your AWS config, your way
if err != nil { return err }
client := kms.NewFromConfig(cfg)

wrapped, err := keysawskms.ParseWrapped(os.Getenv("BRICK_WRAPPED_KEYS")) // "new,old" base64 ciphertexts
if err != nil { return err }
keys, err := keysawskms.New(ctx, client, keysawskms.Options{
    KeyID:             "alias/brick-state",
    Wrapped:           wrapped,
    EncryptionContext: map[string]string{"app": "shop"},
})
if err != nil { return err } // never start without the key
sealer := brick.NewAESGCMSealer(keys, nil)
states, err := brick.NewSealedStates(brick.SealedOptions{Sealer: sealer})
if err != nil { return err }
eng, err := brick.New(brick.Options{FS: fsys, States: states})
```

Runnable version: `Example` builds the ring over an offline stand-in for `*kms.Client` (the `DecryptAPI` and `GenerateDataKeyAPI` interfaces), seals a stateful component into a rendered page and opens it again; `ExampleNew_wrongKey` shows a data key made under another KMS key refused at start. Run it with `go test -run Example .` in this folder ([example_test.go](example_test.go)).

Guide: [sealed-state.md](../../docs/guides/production/sealed-state.md#keys-from-a-kms).

| Option | Default | Meaning |
|---|---|---|
| `KeyID` | required | the KMS key the data keys were encrypted under (id, ARN or alias); pinned so a ciphertext under any other key is refused |
| `Wrapped` | required | the encrypted data keys, newest first: the first seals, every one opens |
| `EncryptionContext` | none | must match the context used at encryption; KMS binds it as authenticated data and logs it in CloudTrail |

**Making a data key.** `NewWrappedKey(ctx, client, keyID, encryptionContext)` calls `GenerateDataKey` (AES-256) and returns only the ciphertext; the plaintext KMS returns next to it is zeroed inside the function. `EncodeWrapped` turns one or more ciphertexts into the comma-separated base64 that `ParseWrapped` reads. The AWS CLI does the same: `aws kms generate-data-key-without-plaintext --key-id alias/brick-state --key-spec AES_256 --query CiphertextBlob --output text`.

**Rotation.** Same as `StaticKeys`: add a new wrapped key second and deploy, promote it to first in a later deploy, and drop the old one a state TTL after that. Rotating the KMS key itself (automatic yearly rotation) needs no change: KMS still decrypts ciphertexts made under its older backing keys.

**Failure is a hard stop.** No client, no `KeyID`, no wrapped key, a KMS error, the wrong key or context, or a data key that does not decrypt to 32 bytes: each makes `New` return an error and no ring. Nothing falls back to running without a key. Errors name the data key's position (`data key 2`), never its bytes or its ciphertext.

**Memory.** The plaintexts KMS returns are zeroed once the ring holds its own copies, on every path out of `New`. The ring keeps the keys in process memory for its lifetime, exactly like a `BRICK_SECRET` ring: envelope encryption protects the keys at rest, not from the running process. A team whose policy forbids keys in process memory needs a sealer that calls KMS for every blob instead, at a KMS call per action.

**IAM.** The server's role needs `kms:Decrypt` on the key (with a condition on `kms:EncryptionContext:app` if you use a context). Only the person or job that makes data keys needs `kms:GenerateDataKey`.

## Tests

`go test ./...` needs no network: it runs against an in-process fake that refuses another key id or encryption context the way KMS does, and checks rotation, every refusal path, that errors carry no key material, and that the plaintexts are zeroed. `task test:keys:aws` starts a throwaway LocalStack KMS from the repo's `compose.yml` (profile `kms`, `127.0.0.1:14566`), runs the live round trip (generate, decrypt, seal, rotate, open) and the live refusals (wrong context, missing context, another key, a non-ciphertext), and removes the container (`BRICK_KMS_TEST=1`; `BRICK_KMS_ENDPOINT` overrides the endpoint). LocalStack is pinned to 4.4 because later images need an account token.
