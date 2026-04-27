# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog], and this project adheres to [Semantic Versioning].

## Unreleased

Complete redesign of the `hyperproviders` package.
Every exported type changes; the `hypertemplates-go` dependency is removed entirely.

* Added `Environment` type with `Name` and concrete `HostingProvider` values, modeling named deployment targets (e.g. `development`, `staging`, `production`).
* Added `HostingProvider` concrete type and `HostingProviderEngine` interface, separating shared provider configuration from provider-specific deployment behavior.
* Added `ProviderFilter` with include/exclude rules over path regexes and mimetype regexes — routes files to the right provider in multi-provider environments.
* Added `SecretProvider` interface, `SecretProviderBase` helper, and `providers/env.Provider` that reads from `os.Getenv`.
* Added `Session`, `SecretValue`, and `providers/runtime.Provider` for deploy-time secret injection.
* Added native `File` type with lazy size, mimetype, SHA-256, and S3-compatible MD5 ETag helpers.
* Added `HostingProvider.BaseURL` for human-readable deploy destination logs, with root-relative paths used when `BaseURL` is unset.
* Added standard output deploy progress logging with deployment, provider, secret-status, file filter, S3 resource, and Git operation/resource lines.
* Added `hosting`, `secrets`, and `session` subpackages for provider engines and advanced integrations, with root aliases for the common API.
* Added `providers/s3.Engine`, an AWS-SDK-v2-backed S3 engine with opt-in ETag-based conditional PUT (`conditional_mode: "etag"`) for skip-if-unchanged uploads and bounded concurrent uploads.
* Added `providers/s3.ProviderConfig.Workers` to configure S3 upload workers with a default of `10`.
* Added `providers/git.Engine`, a `go-git/v5`-backed Git engine with linear commit history, delta-efficient pushes, opt-in build tagging with retention pruning, and first-deploy handling for empty/missing remote branches.
* Added `providers/git` `publish_dir` config to write deployed files under a repository subdirectory, defaulting to the repository root.
* Added `cmd/htdt`, the HyperTexting Deploy Tool — a standalone CLI for deploying arbitrary directories to S3 or Git via a JSON or YAML environments config.
* Changed minimum Go toolchain is `1.25.0` (driven by AWS SDK v2 transitive requirements).
* Changed `HostingProvider.Deploy` and `Environment.Deploy` accept a deployment root directory; hyperproviders owns file discovery, filtering, and provider fan-out internally.
* Changed Provider-specific serialized settings now live under `HostingProvider.Config`; shared provider fields remain at the provider object root.
* Changed S3 upload worker configuration moved under provider-specific `config.workers` instead of a root-level provider concurrency setting.
* Changed `HostingProvider` now validates provider-specific settings through its registered `HostingProviderEngine`.
* Changed symlinks under deployment roots are rejected instead of followed or silently skipped.
* Changed `Deploy` now takes `Session` so callers can choose secret provider precedence per deployment.
* Changed `Secrets.Get` returns `(*Secret, error)` instead of `*Secret` so missing-secret errors are explicit.
* Changed secret declarations no longer include `provider` or `unsafe_value`; durable config names the required secret and lookup key only.
* Removed `HyperProvider`, `HyperProviderConfig`, `HyperProviderEngine`, and `HostingProviders` types.
* Removed the `hypertemplates-go` dependency (HyperFile semantics are not needed inside a deployment engine).
* Removed the `aws-sdk-go-v2/feature/s3/manager` dependency (single-part PUTs only; keeps S3 ETag == MD5).

## v0.17.3 (2025-09-02)

Initial release of the `hyperproviders` package from the standalone `herdworks/hyperproviders-go` repo.

* Completed initial migration from `herdworks/hypertexting-monorepo`
* Added `github.com/herdworks/hypertemplates-go v0.17.3` dependency to `go.sum`
* Adopted `v` prefixed tag naming convention beginning with the `v0.17.3` release

## 0.17.2

See `herdworks/hypertexting-monorepo` for version 0.17.2 and prior changes.
