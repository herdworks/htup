# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog], and this project adheres to [Semantic Versioning].

## v1.0.0

Complete redesign of the `hyperproviders` package.
Every exported type changes; the `hypertemplates-go` dependency is removed entirely.

### Added

* `Environment` type with `Name` and concrete `HostingProvider` values, modeling named deployment targets (e.g. `development`, `staging`, `production`).
* `HostingProvider` concrete type and `HostingProviderEngine` interface, separating shared provider configuration from provider-specific deployment behavior.
* `ProviderFilter` with include/exclude rules over path regexes and mimetype regexes — routes files to the right provider in multi-provider environments.
* `SecretProvider` interface, `SecretProviderBase` helper, and `providers/env.Provider` that reads from `os.Getenv`.
* `Session`, `SecretValue`, and `providers/runtime.Provider` for deploy-time secret injection.
* Native `File` type with lazy size, mimetype, SHA-256, and S3-compatible MD5 ETag helpers.
* `hosting`, `secrets`, and `session` subpackages for provider engines and advanced integrations, with root aliases for the common API.
* `providers/s3.Engine`, an AWS-SDK-v2-backed S3 engine with opt-in ETag-based conditional PUT (`conditional_mode: "etag"`) for skip-if-unchanged uploads and bounded concurrent uploads.
* `providers/s3.ProviderConfig.Workers` to configure S3 upload workers with a default of `10`.
* `providers/git.Engine`, a `go-git/v5`-backed Git engine with linear commit history, delta-efficient pushes, opt-in build tagging with retention pruning, and first-deploy handling for empty/missing remote branches.
* `cmd/htdt`, the HyperTexting Deploy Tool — a standalone CLI for deploying arbitrary directories to S3 or Git via a JSON or YAML environments config.

### Removed

* `HyperProvider`, `HyperProviderConfig`, `HyperProviderEngine`, and `HostingProviders` types.
* The `hypertemplates-go` dependency (HyperFile semantics are not needed inside a deployment engine).
* The `aws-sdk-go-v2/feature/s3/manager` dependency (single-part PUTs only; keeps S3 ETag == MD5).

### Changed

* Minimum Go toolchain is `1.25.0` (driven by AWS SDK v2 transitive requirements).
* `HostingProvider.Deploy` and `Environment.Deploy` accept a deployment root directory; hyperproviders owns file discovery, filtering, and provider fan-out internally.
* Provider-specific serialized settings now live under `HostingProvider.Config`; shared provider fields remain at the provider object root.
* S3 upload worker configuration moved under provider-specific `config.workers` instead of a root-level provider concurrency setting.
* `HostingProvider` now validates provider-specific settings through its registered `HostingProviderEngine`.
* Symlinks under deployment roots are rejected instead of followed or silently skipped.
* `Deploy` now takes `Session` so callers can choose secret provider precedence per deployment.
* `Secrets.Get` returns `(*Secret, error)` instead of `*Secret` so missing-secret errors are explicit.
* Secret declarations no longer include `provider` or `unsafe_value`; durable config names the required secret and lookup key only.

## v0.17.3 (2025-09-02)

Initial release of the `hyperproviders` package from the standalone `herdworks/hyperproviders-go` repo.

* Completed initial migration from `herdworks/hypertexting-monorepo`
* Added `github.com/herdworks/hypertemplates-go v0.17.3` dependency to `go.sum`
* Adopted `v` prefixed tag naming convention beginning with the `v0.17.3` release

## 0.17.2

See `herdworks/hypertexting-monorepo` for version 0.17.2 and prior changes.
