# hyperproviders-go

A Go library for deploying websites to multiple hosting platforms simultaneously. Provides a unified interface for uploading static websites to different providers like AWS S3, Cloudflare R2, GitHub Pages, Netlify, and more.

HyperProviders is the HyperTexting CMS deployment engine. As a static site generation system, the HyperTexting CMS supports deploying to multiple hosting providers simultaneously, including those that support S3, Git, SSH, and Containers.

The goal of the HyperTexting platform is to make updating your website _easier_ than posting to social media. Getting your website online should be easy because it should "just work" with your preferred provider(s). Updating your website should be easy because the provider optimizes updates to reduce payload sizes on mobile devices and networks.

## Features

- **Multi-Provider Deployment**: Deploy to multiple providers simultaneously with a single command
- **Unified Interface**: Deploy to different providers using the same API
- **Provider Support**: S3-compatible services (with more providers coming soon)
- **File Discovery**: Automatically discover and process website assets
- **Secret Management**: Secure handling of credentials via configurable secret providers
- **Extensible Architecture**: Easy to add new provider and secret provider implementations

## Quick Start

### Installation

```bash
go get github.com/herdworks/hyperproviders-go
```

### Basic Usage

```go
package main

import (
    "context"
    "fmt"

    "github.com/herdworks/hyperproviders-go"
    "github.com/herdworks/hyperproviders-go/types"
)

func main() {
    // Configure providers (can deploy to multiple providers simultaneously)
    config := &types.ProvidersConfig{
        Providers: []types.DeployProviderConfig{
            {
                Name: "primary-s3",
                Kind: "s3",
                Config: map[string]interface{}{
                    "endpoint": "https://s3.amazonaws.com",
                    "store":    "my-website-bucket",
                },
                Secrets: []types.SecretConfig{
                    {Name: "access_key_id", Kind: "env", Key: "AWS_ACCESS_KEY_ID"},
                    {Name: "secret_access_key", Kind: "env", Key: "AWS_SECRET_ACCESS_KEY"},
                },
            },
            // Add more providers as needed - they'll all be deployed to simultaneously
        },
    }

    // Create providers
    providers, err := hyperproviders.New(config)
    if err != nil {
        panic(err)
    }

    // Deploy website to all configured providers
    ctx := context.Background()
    buildPath := "./dist"        // Your built website directory
    baseURL := "https://example.com"

    // Recursively scans all files in buildPath and publishes them to baseURL
    err = providers.Deploy(ctx, buildPath, baseURL)
    if err != nil {
        panic(err)
    }

    fmt.Println("Website deployed successfully to all providers!")
}
```

## Supported Providers

| Provider | Status | Description |
|----------|--------|-------------|
| `s3` | ✅ Stable | Deploy to AWS S3 or S3-compatible services |
| `git` | 🚧 Planned | Deploy via Git repositories (GitHub Pages, etc.) |
| `ssh` | 🚧 Planned | Deploy via SSH/SFTP |
| `container` | 🚧 Planned | Deploy to container platforms |

## Compatible Hosting Services

The currently planned HyperProviders implementations will support the following hosting services:

**S3-Compatible:**
- Cloudflare R2
- Amazon S3
- Digital Ocean Spaces
- Fastly Object Storage
- Google Cloud Storage
- Azure Blob Storage

**Git-Based:**
- Cloudflare Pages
- GitHub Pages
- Netlify
- Vercel
- Heroku

**SSH/SFTP:**
- Self-hosted servers
- Linode
- Digital Ocean Droplets
- Amazon EC2
- Google Cloud Compute
- Azure Virtual Machines

**Container Platforms:**
- Cloudflare Containers
- Amazon Fargate/ECS
- Google Cloud Run/GKE
- Azure Container Apps/AKS
- Heroku Containers

## Architecture

```
hyperproviders-go/
├── providers.go                    # Main API and factory
├── types/                         # Core types and interfaces
│   ├── interfaces.go              # DeployProvider and SecretProvider interfaces
│   ├── config.go                  # Configuration types and validation
│   ├── deploy_provider_base.go    # Base implementation for deploy providers
│   └── env_secret_provider.go     # Environment variable secret provider
└── s3/                           # S3 provider implementation
    └── s3.go
```

## Key Concepts

### Multi-Provider Deployment
HyperProviders supports deploying to multiple hosting providers simultaneously. Each provider in the configuration array will receive the same website files, enabling redundancy, geographic distribution, or different environments.

### Deploy Providers
Each deploy provider implements the `DeployProvider` interface:
```go
type DeployProvider interface {
    Deploy(ctx context.Context, buildPath string, baseURL string) error
    Validate() error
}
```

When implementing, `Validate()` should be used to ensure the provider-specific configuration meets the needs of the deploy provider. This includes verifying that needed secrets are set and that configuration input is complete and within range.

### Secret Providers
Credentials are managed through configurable secret providers that implement the `SecretProvider` interface:
```go
type SecretProvider interface {
    GetSecret(key string) (string, error)
    Validate(key string) error
}
```

Currently supported secret providers:
- **env**: Environment variables

### Base Provider Utilities
The `BaseDeployProvider` provides common functionality for deploy providers:
- Configuration and secret management
- MIME type detection
- File system traversal utilities

## API Methods

### Core Methods

- `hyperproviders.New(config)` - Create providers from configuration
- `providers.Deploy(ctx, buildPath, baseURL)` - Deploy to all configured providers

### Configuration

```go
type ProvidersConfig struct {
    Providers []DeployProviderConfig `json:"providers" yaml:"providers"`
}

type DeployProviderConfig struct {
    Name    string                 `json:"name" yaml:"name"`       // Unique provider name
    Kind    string                 `json:"kind" yaml:"kind"`       // Provider type: "s3", etc.
    Config  map[string]interface{} `json:"config" yaml:"config"`   // Provider-specific config
    Secrets []SecretConfig         `json:"secrets" yaml:"secrets"` // Authentication secrets
}

type SecretConfig struct {
    Name string `json:"name" yaml:"name"`     // Secret name for provider use
    Kind string `json:"kind" yaml:"kind"`     // Secret provider type: "env", etc.
    Key  string `json:"key" yaml:"key"`       // Key/path to secret value
}
```

## Example site.yaml Configuration

HyperProviders is intended for use with HyperTemplates static sites. Here's an example `site.yaml` configuration showing how to configure providers:

```yaml
---
base_url: https://example.com
title: My Website
description: A website deployed with HyperProviders
# ... other site configuration ...

provider:
  - name: primary-cloudflare-r2
    kind: s3
    config:
      endpoint: https://your-account.r2.cloudflarestorage.com
      store: my-website-bucket
    secrets:
      - name: access_key_id
        kind: env
        key: CLOUDFLARE_R2_ACCESS_KEY_ID
      - name: secret_access_key
        kind: env
        key: CLOUDFLARE_R2_SECRET_ACCESS_KEY

  - name: backup-aws-s3
    kind: s3
    config:
      endpoint: https://s3.amazonaws.com
      store: my-website-backup-bucket
    secrets:
      - name: access_key_id
        kind: env
        key: AWS_ACCESS_KEY_ID
      - name: secret_access_key
        kind: env
        key: AWS_SECRET_ACCESS_KEY
```

This configuration will deploy your website to both Cloudflare R2 and AWS S3 simultaneously when you build your site.

