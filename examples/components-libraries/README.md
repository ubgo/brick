# components-libraries

Three kinds of component library in one `brick.yaml`: a folder inside the project (`~/ui`, with a `prefix`), the embedded UI kit offered by name (`brick-kit`, cut down with `only:`), and a remote library (`github.com/acme/ui@v1.2.0`) that `brick vendor` copies into `brick_modules/` and pins in `brick.lock`. A second page imports one remote component with `use:`. The remote side is served from `remote-src/` instead of GitHub, so nothing touches the network.

```sh
go run .      # vendor into a scratch folder, render both pages, print the lock list
go test .     # check the exact output (testdata/output.txt), precedence, only:, Config.Apply
```

Run both from this folder. A passing run ends with `ok  	github.com/ubgo/brick/examples/components-libraries`. The guide that teaches it: [Component libraries](../../docs/guides/components/libraries.md).
