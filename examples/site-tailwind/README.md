# site-tailwind

A site whose stylesheet is built by the standalone Tailwind CLI from Go: `tailwind.Build` runs once before serving and again from `site.Options.OnChange` when the watcher sees a change, and runs the binary only when the classes used in the project (or `styles/app.css`) changed.

```sh
# once: install the standalone CLI as `tailwindcss` on PATH (https://tailwindcss.com/blog/standalone-cli)
go run .                    # builds theme/static/app.css, then serves http://localhost:8097 with live reload
go run . -tailwind ./tw     # a binary that is not on PATH
brick serve theme           # the same with the brick command (reads theme/brick.yaml)
go test .                   # uses a stand-in binary; the real-binary test is skipped when tailwindcss is not on PATH
```

`theme/static/app.css` is generated and ignored by git.

Guide: [Tailwind](../../docs/guides/site/tailwind.md).
