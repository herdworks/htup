# HyperProviders

HyperProviders is the HyperTexting CMS deployment engine.
As a static site generation system, the HyperTexting CMS should support a wide variety of hosting providers, including those that support S3, Git, SSH, and Containers.

The goal of the HyperTexting platform is to make updating your website _easier_ than posting to social media.
Getting your website online should be easy because it should "just work" with your preferred provider.
Updating your website should be easy because the provider optimizes updates to reduce payload sizes on mobile devices and networks.

## Architecture

Deployment is modeled as an `Environment` (e.g. `development`, `staging`, `production`) that owns one-or-more `Provider`s.
Callers pass a deployment root directory, and HyperProviders handles file discovery, filtering, and provider fan-out internally.
Every file's deployment path is always relative to that source directory's root.
For example, deploying `relative/path/to/a/build/` writes `relative/path/to/a/build/index.html` as the object key `index.html` for an S3 provider, and as the path `index.html` in the root of the git repo for a Git provider.

A provider's `Filter` (`include` / `exclude` on path regexes and mimetype regexes) narrows what it accepts.
HyperProviders does **not** dedupe across providers — the same file can legitimately be deployed by multiple providers (e.g. CDN + backup).
The top-level `.provider/` directory is reserved for provider-managed workspace state and is never deployed.
Symlinks are not supported in deployment roots.

## Providers

* **S3** — AWS S3, Cloudflare R2, MinIO, DigitalOcean Spaces, any S3-compatible object store.
    Concurrent uploads (`workers`, default `10`), opt-in ETag-based conditional PUT (`conditional_mode: "etag"`) for skip-if-unchanged deploys.
* **Git** — any git remote (HTTPS or SSH), backed by `go-git/v5`.
    Pure-Go, delta-efficient (unchanged files reuse blob IDs), provider workspace under `.provider/git/`, optional `publish_dir` subtree, opt-in build tagging with retention pruning.
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

provider := &hp.HostingProvider{
    Name:     "assets",
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
}
session := &hp.Session{
    Secrets: hp.SecretValues{
        "AWS_ACCESS_KEY_ID":     hp.StringSecret(accessKey),
        "AWS_SECRET_ACCESS_KEY": hp.StringSecret(secretAccessKey),
    },
}
if err := provider.Validate(); err != nil { /* ... */ }
if err := provider.Deploy(ctx, "./build", session); err != nil { /* ... */ }
```

Deploys write progress logs to standard output with `[deployment]` and
`[provider/<kind>/<name>]` prefixes. `Environment.Deploy` writes environment
start and completed lines. Provider setup logs hosting init/ready, configured
secret names as found/not_found/empty/error without logging values, and source
files as included/excluded after filtering. Provider engines log destination
resources; resource URLs use the provider's `BaseURL` when set and fall back to
root-relative paths when `BaseURL` is empty.

Callers that manage multiple deployment environments should usually model them
as `map[string]*hyperproviders.Environment` (or `map[string]*hp.Environment`
when importing with the alias above). The map key is the environment identifier
used for selection, and the pointer value avoids copying provider slices while
allowing config loaders to fill an empty `Environment.Name` from the key.

When `Session.SecretProviders` is omitted, HyperProviders resolves secrets only
from `Session.Secrets`. Add `providers/env.New()` explicitly when environment
variable lookup should be available. Add `providers/runtime.New()` explicitly
when runtime secrets should participate in a custom precedence list.
Import each provider engine package you need before validating, planning, or
deploying so its `init` registration makes that `kind` available.

## `htdt` — HyperTexting Deploy Tool

`htdt` is a standalone CLI for deploying any directory using hyperproviders-go.
It reads a JSON or YAML config file with an `environments` map and deploys the chosen environment's providers.
The CLI session resolves secrets from OS environment variables.
Generic provider fields (`name`, `kind`, `endpoint`, `base_url`, `filter`, `secrets`) live at the provider root.
Provider-specific settings live under `config`.
For S3 providers, `workers` controls concurrent object uploads and must be at least `1` when set.
For Git providers, `publish_dir` controls where files are written in the repository and defaults to `.`.

```sh
go install github.com/herdworks/hyperproviders-go/cmd/htdt@latest

htdt deploy --config ./htdt.json --environment production --source ./build
htdt validate --config ./htdt.json
htdt list     --config ./htdt.json
```

Example `htdt.json`:

```json
{
  "version": 1,
  "environments": {
    "production": {
      "providers": [
        {
          "kind": "s3",
          "name": "production-assets",
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
          "filter": {
            "include": {
              "types": ["^image/", "^video/", "^audio/"]
            }
          },
          "secrets": [
            { "name": "access_key_id", "key": "R2_ACCESS_KEY_ID" },
            { "name": "secret_access_key", "key": "R2_SECRET_ACCESS_KEY" }
          ]
        },
        {
          "kind": "git",
          "name": "production-pages",
          "endpoint": "git@github.com:acme/site.git",
          "base_url": "https://example.com",
          "config": {
            "branch": "gh-pages",
            "publish_dir": ".",
            "author_name": "hyperproviders bot",
            "author_email": "bot@example.com"
          },
          "filter": {
            "exclude": {
              "types": ["^image/", "^video/", "^audio/"]
            }
          },
          "secrets": [
            { "name": "ssh_private_key", "key": "DEPLOY_SSH_KEY" }
          ]
        }
      ]
    }
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
