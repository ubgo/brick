# Tailwind example

A site whose CSS is built by the standalone Tailwind CLI, only when the classes in its files change.

```sh
# once: install the standalone binary (https://tailwindcss.com/blog/standalone-cli) as `tailwindcss` on PATH
brick serve examples/tailwind     # rebuilds static/app.css when you change a class
brick build -o public examples/tailwind
```

`brick.yaml` names the input and output. The output's first line is a stamp — a hash of the classes used and of `styles/app.css` — so an edit that changes no class does not run the binary.
