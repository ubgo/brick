# brick/contrib/script-starlark

Runs brick's scripts — `.star` modules imported with `use:`, inline `<brick:script>` blocks, and `load:` loaders — in [Starlark](https://github.com/google/starlark-go). Needs Go 1.25.

```go
sc, err := scriptstarlark.New(scriptstarlark.Options{
    DB:        readOnlyDB,                            // enables db.query (off when nil)
    HTTPAllow: []string{"https://api.example.com"},   // enables http.get (off when empty)
})
eng, err := brick.New(brick.Options{FS: fsys, Scripting: sc})
```

## The sandbox

- A script cannot read files, open sockets, read the clock or draw random numbers. It reaches the outside only through what `Options` grants: `Globals`, `DB`, `HTTPAllow`.
- The dialect is pinned: no `set`, no `while`, no top-level control flow, no global reassignment, no recursion.
- Every call has a step limit (`Steps`, default 1,000,000) and a time limit (`Timeout`, default 2s). The render's `ctx` cancels it too. Going over a limit is an ordinary error a `<brick:boundary>` can contain; the render's own context ending is not.
- `load("~/lib/strings.star", "lower")` imports another module, and `load("~/data/cfg.yaml", cfg = "data")` imports a data file. brick resolves the path before anything runs, so a script never chooses a file while running.

## `db`

| Call | |
|---|---|
| `db.query(sql, *args)` | read-only transaction, rolled back; the SQL must be a string literal or a module-level constant, with `?` for values — checked when the module compiles |
| `db.query_dynamic(sql, *args)` | the named escape for SQL built at run time |
| `db.exec(sql, *args)` | writes; exists only with `DBExec: true` |

Rows come back as a list of dicts: `NULL` → `None`, decimals and text → strings, times → ISO strings. More than `DBMaxRows` (10,000) rows is an error, never a silent truncation. One statement per call. The real guarantee is a read-only database role; give `DB` a connection that has one.

## `http`

`http.get(url)` returns `{status, headers, text}`; `http.get_json(url)` returns parsed JSON. Only origins in `HTTPAllow` (scheme + host + port, no wildcards) are reachable, every redirect is re-checked, credentials in URLs are refused, non-ASCII hosts must be written in punycode, and bodies over `HTTPMaxBytes` (5 MB) are an error.

## Values

Go → Starlark: `nil` → `None`, numbers, strings, `[]byte` → bytes, `time.Time` → ISO string, maps with string keys → dict (sorted keys), slices → list, structs → dict of exported fields. Back: integers must fit `int64`, NaN and Inf are errors, tuples become lists, sets and functions are errors. Conversion is depth-capped at 64 and refuses cycles.
