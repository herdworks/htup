# HyperProviders

HyperProviders is the HyperTexting CMS deployment engine.
As a static site generation system, the HyperTexting CMS should support a wide variety of hosting providers, including those that support S3, Git, SSH, and Containers.

The goal of the HyperTexting platform is to make updating your website _easier_ than posting to social media.
Getting your website online should be easy because it should "just work" with your preferred provider.
Updating your website should be easy because the provider optimizes updates to reduce payload sizes on mobile devices and networks.

## Architecture

Deployment is modeled as a `HostingProviders` map plus `HostingEnvironment` provider-name selections.
Callers pass a deployment root directory, and HyperProviders handles file discovery, filtering, and provider fan-out internally.
Every file's deployment path is always relative to that source directory's root.
For example, deploying `relative/path/to/a/build/` writes `relative/path/to/a/build/index.html` as the object key `index.html` for an S3 provider, and as the path `index.html` in the root of the git repo for a Git provider.

A provider's root `include` / `exclude` rules narrow what it accepts.
HyperProviders does **not** dedupe across providers — the same file can legitimately be deployed by multiple providers (e.g. CDN + backup).
The top-level `.provider/` directory is reserved for provider-managed workspace state and is never deployed.
Symlinks are not supported in deployment roots.

## Providers

* **S3** — AWS S3, Cloudflare R2, MinIO, DigitalOcean Spaces, any S3-compatible object store.
    Concurrent uploads (`workers`, default `10`), opt-in ETag-based conditional PUT (`conditional_mode: "etag"`) for skip-if-unchanged deploys.
* **Git** — any git remote (HTTPS or SSH), backed by `go-git/v5`.
    Pure-Go, delta-efficient (unchanged files reuse blob IDs), provider workspace under `.provider/git/`, optional `publish_dir` subtree, optional `known_hosts` config, opt-in build tagging with retention pruning.
* **SSH** (planned)
* **Containers** (planned)

## Install

```sh
go get github.com/herdworks/hyperproviders-go@latest
```

## Library usage

```go
import (
    hp "github.com/herdworks/hyperproviders-go"
    "github.com/herdworks/hyperproviders-go/providers/s3"
)

providers := hp.HostingProviders{
    "assets": {
        Kind:     s3.Kind,
        Endpoint: "https://example.r2.cloudflarestorage.com",
        BaseURL:  "https://assets.example.com",
        Secrets: hp.Secrets{
            {Name: "access_key_id", Key: "AWS_ACCESS_KEY_ID"},
            {Name: "secret_access_key", Key: "AWS_SECRET_ACCESS_KEY"},
        },
        Config: map[string]any{
            "bucket": "my-bucket",
            "region": "us-east-1",
            "addressing_style": "path",
        },
    },
}
environment := hp.HostingEnvironment{"assets"}
session := &hp.Session{
    Secrets: hp.SecretValues{
        "AWS_ACCESS_KEY_ID":     hp.StringSecret(accessKey),
        "AWS_SECRET_ACCESS_KEY": hp.StringSecret(secretAccessKey),
    },
}
if err := environment.Deploy(ctx, providers, "./build", session); err != nil { /* ... */ }
```

Deploys write progress logs to standard output with `[deployment]` and
`[provider/<kind>/<name>]` prefixes. Provider setup logs hosting init/ready, configured
secret names as found/not_found/empty/error without logging values, and source
files as included/excluded after filtering. Provider engines log destination
resources; resource URLs use the provider's `BaseURL` when set and fall back to
root-relative paths when `BaseURL` is empty.

Callers that manage multiple deployment environments should usually model
providers as `hyperproviders.HostingProviders` and environments as
`map[string]*hyperproviders.HostingEnvironment`. The provider map key is the provider
identifier used for selection, and each environment is an ordered list of those
provider names.

Secret declarations require both `name` and `key`: `name` is the provider-defined
credential role, and `key` is the caller-defined runtime lookup key. Hosting
providers treat secret providers as key/value stores and do not perform
provider-specific secondary lookups. When `Session.SecretProviders` is omitted,
HyperProviders resolves secrets only from `Session.Secrets`. Add
`providers/env.New()` explicitly when environment variable lookup should be
available. Add `providers/files.New()` explicitly when secret keys should be
read as local filesystem paths; the files provider expands only `~/`, `$HOME/`,
and `${HOME}/`. Add `providers/runtime.New()` explicitly when runtime secrets
should participate in a custom precedence list.
Import each provider engine package you need before validating, planning, or
deploying so its `init` registration makes that `kind` available.

## `htdt` — HyperTexting Deploy Tool

`htdt` is a standalone CLI for deploying any directory using hyperproviders-go.
It reads a JSON or YAML config file with a `providers` map and an `environments` map, then deploys the chosen environment's provider names.
The CLI session resolves secrets from OS environment variables.
Generic provider fields (`kind`, `endpoint`, `base_url`, `include`, `exclude`, `secrets`) live at the provider root.
Provider-specific settings live under `config`.
For S3 providers, `workers` controls concurrent object uploads and must be at least `1` when set.
For Git providers, `publish_dir` controls where files are written in the repository and defaults to `.`.
For Git SSH remotes, `known_hosts` is optional literal OpenSSH `known_hosts` content; when omitted, SSH host key verification uses `ssh.InsecureIgnoreHostKey`.
Git SSH auth requires an `ssh_private_key` secret whose resolved value is PEM private key content. CLI callers that want to keep key material in a file can include `providers/files.New()` or `providers/files.NewWithBaseDirectory(baseDir)` in their session and configure `ssh_private_key.key` as the file path.

```sh
go install github.com/herdworks/hyperproviders-go/cmd/htdt@latest

htdt validate --config ./htdt.json
htdt ready    --config ./htdt.json --environment production
htdt deploy   --config ./htdt.json --environment production --source ./build
htdt list     --config ./htdt.json
```

`validate` checks static provider configuration. `ready` also resolves runtime
secrets from OS environment variables and performs local provider-specific
credential checks, without contacting remote services.

Example `htdt.json`:

```json
{
  "version": 1,
  "providers": {
    "production-assets": {
      "kind": "s3",
      "endpoint": "https://<accountid>.r2.cloudflarestorage.com",
      "base_url": "https://assets.example.com",
      "config": {
        "bucket": "my-site-assets",
        "region": "auto",
        "addressing_style": "path",
        "cache_control": "public, max-age=3600",
        "conditional_mode": "etag",
        "workers": 10
      },
      "include": {
        "types": ["^image/", "^video/", "^audio/"]
      },
      "secrets": [
        { "name": "access_key_id", "key": "R2_ACCESS_KEY_ID" },
        { "name": "secret_access_key", "key": "R2_SECRET_ACCESS_KEY" }
      ]
    },
    "production-pages": {
      "kind": "git",
      "endpoint": "git@github.com:acme/site.git",
      "base_url": "https://example.com",
      "config": {
        "branch": "gh-pages",
        "publish_dir": ".",
        "author_name": "hyperproviders bot",
        "author_email": "bot@example.com"
      },
      "exclude": {
        "types": ["^image/", "^video/", "^audio/"]
      },
      "secrets": [
        { "name": "ssh_private_key", "key": "DEPLOY_SSH_KEY" }
      ]
    }
  },
  "environments": {
    "production": ["production-assets", "production-pages"]
  }
}
```

See `cmd/htdt/testdata/config.sample.json` and `cmd/htdt/testdata/config.sample.yaml` for reference configs.

## Compatible Hosting Services

1. Cloudflare Pages (`git`)
1. Cloudflare Workers w/ Static Assets (`git`)
1. GitHub Pages (`git`)
1. Netlify (`git`)
1. Vercel (`git`)
1. Heroku (`git`)
1. Self-hosted (`git`)
1. Cloudflare R2 (`s3`)
1. Fastly Object Storage (`s3`)
1. Digital Ocean Spaces (`s3`)
1. Amazon S3 (`s3`)
1. Azure Blob Storage (`s3`)
1. Google Cloud Storage (`s3`)

> ### Coming soon
> 
> With the addition of SSH and container providers (coming soon), HyperProviders will gain support for automated deployment to the following hosting services: 
> 
> 1. Linode (`ssh`)
> 1. Self hosted (`ssh`)
> 1. Amazon EC2 (`ssh`)
> 1. Google Cloud Compute (`ssh`)
> 1. Azure Virtual Machines (`ssh`)
> 1. Digital Ocean Droplets (`ssh`)
> 1. Cloudflare Containers (`containers`)
> 1. Amazon Fargate (`containers`)
> 1. Amazon ECS (`containers`)
> 1. Google Cloud Run (`containers`)
> 1. Google Cloud GKE (`containers`)
> 1. Azure Container Apps (`containers`)
> 1. Azure AKS (`containers`)

## Troubleshooting

_Running `go test ./...` for the `providers/git` package requires a working local git toolchain._
The tests stand up a bare repo under `t.TempDir()` and use `file://` URLs.

_The AWS SDK dependency bumps the minimum Go toolchain to `go1.25.0`._
`go get` will fetch the required toolchain automatically.
