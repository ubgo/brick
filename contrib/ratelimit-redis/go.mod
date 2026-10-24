module github.com/ubgo/brick/contrib/ratelimit-redis

go 1.24

require (
	github.com/redis/go-redis/v9 v9.22.0
	github.com/ubgo/brick v0.0.0
)

require (
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	go.uber.org/atomic v1.11.0 // indirect
	go.yaml.in/yaml/v3 v3.0.5 // indirect
	golang.org/x/sys v0.30.0 // indirect
)

replace github.com/ubgo/brick => ../..
