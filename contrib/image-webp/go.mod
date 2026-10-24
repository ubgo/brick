module github.com/ubgo/brick/contrib/image-webp

go 1.24

replace github.com/ubgo/brick => ../..

require (
	github.com/HugoSmits86/nativewebp v1.3.0
	github.com/ubgo/brick v0.0.0
	golang.org/x/image v0.24.0
)

require go.yaml.in/yaml/v3 v3.0.5 // indirect
