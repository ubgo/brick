# components-props

Three pages that pass values to components: typed props with defaults and `oneof`, an object prop with a declared shape (`fields:`), the `:post` shorthand, the two tag spellings, undeclared attributes placed with `{{ attrs }}` and `cx`, `brick:spread`, and a host template function and prop type (`Options.Funcs`, `Options.PropTypes`). The tests also pin the error text for a bad `oneof` value, a missing required prop, `:post` with no `$post`, `strictProps` and the `fields:` warnings.

```sh
go run .      # print what the guide shows
go test .   # check the exact output (testdata/output.txt)
```

Run both from this folder. The guide that walks through it: [Components and props](../../docs/guides/components/props.md).
