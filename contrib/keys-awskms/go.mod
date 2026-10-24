module github.com/ubgo/brick/contrib/keys-awskms

go 1.24

require (
	github.com/aws/aws-sdk-go-v2 v1.47.3
	github.com/aws/aws-sdk-go-v2/credentials v1.20.9
	github.com/aws/aws-sdk-go-v2/service/kms v1.61.4
	github.com/ubgo/brick v0.0.0
)

require (
	github.com/aws/aws-sdk-go-v2/internal/configsources v1.5.6 // indirect
	github.com/aws/aws-sdk-go-v2/internal/endpoints/v2 v2.8.6 // indirect
	github.com/aws/smithy-go v1.28.5 // indirect
	go.yaml.in/yaml/v3 v3.0.5 // indirect
)

replace github.com/ubgo/brick => ../..
