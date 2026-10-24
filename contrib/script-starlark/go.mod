module github.com/ubgo/brick/contrib/script-starlark

go 1.25.0

require github.com/ubgo/brick v0.0.0

require (
	go.starlark.net v0.0.0-20260908191801-89a6a09411d5
	go.yaml.in/yaml/v3 v3.0.5 // indirect
	golang.org/x/sys v0.42.0 // indirect
)

replace github.com/ubgo/brick => ../..
