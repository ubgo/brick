module github.com/ubgo/brick/contrib/markdown-goldmark

go 1.25

require (
	github.com/alecthomas/chroma/v2 v2.27.0
	github.com/ubgo/brick v0.0.0
	github.com/yuin/goldmark-highlighting/v2 v2.0.0-20230729083705-37449abec8cc
)

require github.com/dlclark/regexp2/v2 v2.2.1 // indirect

require (
	github.com/yuin/goldmark v1.8.6
	go.yaml.in/yaml/v3 v3.0.5 // indirect
)

replace github.com/ubgo/brick => ../..
