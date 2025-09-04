// hyperproviders-go module
//
// reference: https://go.dev/doc/modules/gomod-ref#replace
module github.com/herdworks/hyperproviders-go

go 1.23.5

// direct dependencies
require (
	github.com/aws/aws-sdk-go-v2 v1.33.0
	github.com/aws/aws-sdk-go-v2/config v1.29.1
	github.com/aws/aws-sdk-go-v2/credentials v1.17.54
	github.com/aws/aws-sdk-go-v2/feature/s3/manager v1.17.53
	github.com/aws/aws-sdk-go-v2/service/s3 v1.74.0
	github.com/herdworks/hypertemplates-go v0.17.3
	golang.org/x/sync v0.10.0
)

// local development dependencies (replacements)
//
// uncomment to use local dependencies
// replace github.com/herdworks/hypertemplates-go => ../hypertemplates-go

// transitive dependencies
require (
	github.com/alecthomas/chroma/v2 v2.2.0 // indirect
	github.com/andybalholm/cascadia v1.3.2 // indirect
	github.com/aws/aws-sdk-go-v2/aws/protocol/eventstream v1.6.7 // indirect
	github.com/aws/aws-sdk-go-v2/feature/ec2/imds v1.16.24 // indirect
	github.com/aws/aws-sdk-go-v2/internal/configsources v1.3.28 // indirect
	github.com/aws/aws-sdk-go-v2/internal/endpoints/v2 v2.6.28 // indirect
	github.com/aws/aws-sdk-go-v2/internal/ini v1.8.1 // indirect
	github.com/aws/aws-sdk-go-v2/internal/v4a v1.3.28 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/accept-encoding v1.12.1 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/checksum v1.5.2 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/presigned-url v1.12.9 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/s3shared v1.18.9 // indirect
	github.com/aws/aws-sdk-go-v2/service/sso v1.24.11 // indirect
	github.com/aws/aws-sdk-go-v2/service/ssooidc v1.28.10 // indirect
	github.com/aws/aws-sdk-go-v2/service/sts v1.33.9 // indirect
	github.com/aws/smithy-go v1.22.1 // indirect
	github.com/dlclark/regexp2 v1.7.0 // indirect
	github.com/yuin/goldmark v1.7.8 // indirect
	github.com/yuin/goldmark-emoji v1.0.4 // indirect
	github.com/yuin/goldmark-highlighting/v2 v2.0.0-20230729083705-37449abec8cc // indirect
	github.com/yuin/goldmark-meta v1.1.0 // indirect
	golang.org/x/net v0.32.0 // indirect
	gopkg.in/yaml.v2 v2.3.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)
