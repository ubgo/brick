# brick/contrib/keys-gcpkms

A `brick.KeyRing` whose state keys are protected by Google Cloud KMS, so the sealing keys no longer sit in a `BRICK_SECRET` environment variable in plain form.

> **Not tested against real Google Cloud.** Google ships no Cloud KMS emulator, so this module is tested only against an in-process fake of the API. The request and response shapes follow the `cloud.google.com/go/kms` client, but no test in this repository has called a real Cloud KMS key. Try it against a test key before relying on it.

**How it works: envelope encryption.** Each 32-byte state key (a data key) is stored *encrypted* under a Cloud KMS CryptoKey. The ciphertext is not a secret: it can live in config, an environment variable or a repo. At start, `New` asks Cloud KMS to decrypt each data key once and builds an ordinary `brick.StaticKeys` ring from the results. Sealing and opening then run locally on brick's AES-GCM sealer, so no request waits on Cloud KMS, no request costs a call, and an outage after start changes nothing. What Cloud KMS adds over a plain secret: the stored form is useless without IAM permission to decrypt, every decrypt is in Cloud Audit Logs, and revoking the permission stops new servers from starting.

## Use it

```go
client, err := kms.NewKeyManagementClient(ctx) // your credentials, your way
if err != nil { return err }
defer client.Close()

wrapped, err := keysgcpkms.ParseWrapped(os.Getenv("BRICK_WRAPPED_KEYS")) // "new,old" base64 ciphertexts
if err != nil { return err }
keys, err := keysgcpkms.New(ctx, client, keysgcpkms.Options{
    KeyName: "projects/my-proj/locations/global/keyRings/brick/cryptoKeys/state",
    Wrapped: wrapped,
    AAD:     []byte("app=shop"),
})
if err != nil { return err } // never start without the key
sealer := brick.NewAESGCMSealer(keys, nil)
states, err := brick.NewSealedStates(brick.SealedOptions{Sealer: sealer})
if err != nil { return err }
eng, err := brick.New(brick.Options{FS: fsys, States: states})
```

Runnable version: `Example` builds the ring over an offline stand-in for `*kms.KeyManagementClient` (the `DecryptAPI` and `EncryptAPI` interfaces, with the CRC32C checksums Cloud KMS returns), seals a stateful component into a rendered page and opens it again; `ExampleNew_wrongAAD` shows another app's data key refused at start. Run it with `go test -run Example .` in this folder ([example_test.go](example_test.go)).

Guide: [sealed-state.md](../../docs/guides/production/sealed-state.md#keys-from-a-kms).

| Option | Default | Meaning |
|---|---|---|
| `KeyName` | required | the CryptoKey's resource name; Cloud KMS picks the version from the ciphertext |
| `Wrapped` | required | the encrypted data keys, newest first: the first seals, every one opens |
| `AAD` | none | additional authenticated data; must match the value used at encryption |

**Integrity.** Every request carries CRC32C checksums and every response's checksum is verified, as Google recommends, so a value corrupted in transit is a refusal rather than a wrong key.

**Making a data key.** Cloud KMS has no data-key endpoint, so `NewWrappedKey(ctx, client, keyName, aad)` makes 32 random bytes locally, encrypts them with Cloud KMS, verifies the checksums and returns only the ciphertext; the local plaintext is zeroed before it returns. `EncodeWrapped` turns one or more ciphertexts into the comma-separated base64 that `ParseWrapped` reads.

**Rotation.** Same as `StaticKeys`: add a new wrapped key second and deploy, promote it to first in a later deploy, and drop the old one a state TTL after that. Rotating the CryptoKey (a new primary version) needs no change while the older versions stay enabled.

**Failure is a hard stop.** No client, no `KeyName`, no wrapped key, a Cloud KMS error, the wrong key or AAD, a checksum mismatch, or a data key that does not decrypt to 32 bytes: each makes `New` return an error and no ring. Nothing falls back to running without a key. Errors name the data key's position (`data key 2`), never its bytes.

**Memory.** The plaintexts Cloud KMS returns are zeroed once the ring holds its own copies, on every path out of `New`. The ring keeps the keys in process memory for its lifetime, exactly like a `BRICK_SECRET` ring: envelope encryption protects the keys at rest, not from the running process.

**IAM.** The server's service account needs `roles/cloudkms.cryptoKeyDecrypter` on the key. Only the person or job that makes data keys needs `roles/cloudkms.cryptoKeyEncrypter`.

**Go version.** This module needs Go 1.26, because the current `cloud.google.com/go/kms` does.

## Tests

`go test ./...` needs no network: it runs against an in-process fake that checks the request checksums, returns response checksums, and refuses another key or AAD the way Cloud KMS does. It covers rotation, every refusal path (including a corrupted response), that errors carry no key material, that the plaintexts are zeroed, and that `*kms.KeyManagementClient` satisfies both interfaces. There is no live test.
