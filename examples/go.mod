module github.com/ubgo/brick/examples

go 1.25.0

require (
	github.com/ubgo/brick v0.0.0
	github.com/ubgo/brick/contrib/assets-http v0.0.0-00010101000000-000000000000
	github.com/ubgo/brick/contrib/image-webp v0.0.0-00010101000000-000000000000
	github.com/ubgo/brick/contrib/markdown-goldmark v0.0.0
	github.com/ubgo/brick/contrib/sanitize-bluemonday v0.0.0
	github.com/ubgo/brick/contrib/script-starlark v0.0.0
	go.starlark.net v0.0.0-20260908191801-89a6a09411d5
	go.yaml.in/yaml/v3 v3.0.5
	modernc.org/sqlite v1.59.0
)

require (
	github.com/HugoSmits86/nativewebp v1.3.0 // indirect
	github.com/alecthomas/chroma/v2 v2.27.0 // indirect
	github.com/aymerick/douceur v0.2.0 // indirect
	github.com/dlclark/regexp2/v2 v2.2.1 // indirect
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/gorilla/css v1.0.1 // indirect
	github.com/mattn/go-isatty v0.0.24 // indirect
	github.com/microcosm-cc/bluemonday v1.0.27 // indirect
	github.com/ncruces/go-strftime v1.0.0 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	github.com/yuin/goldmark v1.8.6 // indirect
	github.com/yuin/goldmark-highlighting/v2 v2.0.0-20230729083705-37449abec8cc // indirect
	golang.org/x/image v0.24.0 // indirect
	golang.org/x/net v0.26.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	modernc.org/libc v1.75.7 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.12.1 // indirect
)

replace (
	github.com/ubgo/brick => ../
	github.com/ubgo/brick/contrib/assets-http => ../contrib/assets-http
	github.com/ubgo/brick/contrib/markdown-goldmark => ../contrib/markdown-goldmark
	github.com/ubgo/brick/contrib/sanitize-bluemonday => ../contrib/sanitize-bluemonday
	github.com/ubgo/brick/contrib/script-starlark => ../contrib/script-starlark
)

replace github.com/ubgo/brick/contrib/image-webp => ../contrib/image-webp
