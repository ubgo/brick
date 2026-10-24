# tooling-cli

The `brick` command used the way a CI job uses it: `brick version -json` to record which build ran, and `brick fmt -l` / `brick fmt -d` to gate a theme's formatting. The program shells out to a brick binary because `cmd/brick` is its own Go module, and it fails the run when a file is not formatted, which `brick fmt -l` alone never does (it exits 0 when files differ).

Build a binary and run the gate from `examples/`:

```sh
go build -C ../cmd/brick -o ./brick .
go run ./tooling-cli -brick ./brick
```

```text
brick v0.0.0-20261013175757-f76fdd502477+dirty
not formatted: tooling-cli/theme/components/Panel.html
files need `brick fmt -w`
exit status 1
```

`theme/components/Panel.html` is deliberately hand-written with uneven spacing; `./brick fmt -w tooling-cli/theme/components/Panel.html` fixes it and the gate then prints `formatted`.

Test it (the test builds the command itself, so it takes a few seconds):

```sh
go test ./tooling-cli/
```

```text
ok  	github.com/ubgo/brick/examples/tooling-cli
```

Guides: [formatting with brick fmt](../../docs/guides/tooling/fmt.md) (Step 6) and [the CLI reference](../../docs/reference/cli.md#brick-version).
