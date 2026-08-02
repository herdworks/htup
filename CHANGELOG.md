# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog], and this project adheres to [Semantic Versioning].

## Unreleased

* Added config-backed `Deployment` setup: callers provide a regular YAML or JSON `ConfigPath`, an independent `PayloadPath`, and either explicit providers or a named configured environment. `ConfigBytes` supports an immutable caller-provided configuration snapshot while the path remains validated.
* Changed `htdt deploy` to resolve its selected environment through `Deployment` and to pass the config path and payload path separately.

## v0.24.0 (2026-07-31)

* Added declarative `Deployment` planning and execution. `Prepare` loads a build root's `.deployment.json`, resolves ignore and enqueue rules, materializes preconditions, and exposes read-only `Instructions`, `Plan`, and `Status` snapshots; `Execute` checks all selected providers before applying sequential conditional operations and concurrent ordinary operations.
* Added `.deployment.json` declarations for exact-byte/create-only preconditions, build-root ignore rules, and post-bulk enqueue ordering. The `.deployment/` directory is now reserved for provider and generator workspace state and is never deployed.
* Added provider-native `DeploymentOperation` validation and execution, including S3 compare-and-swap preconditions and ordered enqueue uploads, plus Git base-tree precondition checks and one final commit/push.
* Changed `htdt deploy --dry-run` to prepare and render the declarative operation plan without resolving credentials or making remote writes. Deployment kinds are now `complete` and `incremental`.
* **BREAKING:** Replaced the `HostingEnvironment`/`DeploymentMode` deployment entrypoints and the old `hosting`, `secrets`, and `session` subpackages with the root-level declarative `Deployment`, `HostingEngine`, and secrets APIs. Provider engines now implement `ValidateOperation` and `PerformOperation`; callers supply ordered `SecretsProviders`, and runtime secrets are constructed with `providers/runtime.New(runtime.Secrets{...})`.

## v0.23.0 (2026-05-28)

* **BREAKING:** Changed deployment logging to use explicit `io.Writer` plumbing across deploy entrypoints and provider engines, removing package-global log output mutation. Affected APIs include `hosting.BeginDeployment`, `hosting.Provider.Deploy`, `hosting.Provider.DeployNamed`, `hosting.Engine.Deploy`, `HostingProviders.Deploy`, `HostingEnvironment.Deploy`, and provider engine implementations. Passing nil preserves previous stdout behavior; non-nil writers are used directly.
* Changed deployment log helpers (`hosting.LogProvider`, `hosting.LogSecret`, `hosting.LogFile`, and `hosting.LogResource`) to accept the deployment writer explicitly.
* Changed generated deployment log files to mirror the selected deployment writer through a synchronized tee when a deployment root is available.

## v0.22.0 (2026-05-28)

* Added root `.deploymentignore` support during deployment file discovery; ignored paths are removed before provider include/exclude filters run, and the ignore file itself is never deployed.
* Added generated deployment log files under `.deployment/logs/` that mirror standard output when a deployment root is available.
* Added explicit deployment modes: `replacement` for authoritative artifact sets and `overlay` for non-authoritative writes that must preserve absent remote resources.
* Changed `HostingProvider.Deploy`, `HostingProviders.Deploy`, `HostingEnvironment.Deploy`, and the provider engine `Deploy` interface to require a deployment mode argument. **Breaking change:** callers and provider engines must pass or accept an explicit `DeploymentMode`.
* Changed `htdt deploy` to require `--mode replacement` or `--mode overlay`. **Breaking change:** deploy commands without `--mode` now fail validation.
* Changed Git overlay deployments to copy and stage only provided files without clearing `publish_dir`; overlay deployments now fail when the remote branch does not exist.
* Changed S3 deployments to validate the requested deployment mode while retaining existing upload-only behavior for both `replacement` and `overlay`.
* Changed provider-managed workspace state from the root `.provider/` directory to the root `.deployment/` directory.

## v0.21.0 (2026-05-27)

* Added `providers/files`, a secret provider that treats secret keys as filesystem paths and resolves them to file contents with support for only `~/`, `$HOME/`, and `${HOME}/` expansion.
* Added Git provider `config.known_hosts` for literal OpenSSH `known_hosts` content; SSH host key verification now uses that callback when configured and `ssh.InsecureIgnoreHostKey` when omitted.
* Changed Git SSH authentication to require `ssh_private_key` PEM content and removed provider-level `ssh_key_path` file-reading behavior so hosting providers treat secret providers as key/value stores.
* Changed declared Git `ssh_passphrase` secrets to return resolution errors instead of being silently ignored.

## v0.20.0 (2026-05-21)

* Added local-only provider readiness checks via `HostingProvider.Ready`, `HostingProviders.Ready`, `HostingEnvironment.Ready`, and the `HostingProviderEngine.Ready` interface method.
* Added `htdt ready` to verify runtime-resolved provider credentials without remote network probes.

## v0.19.1 (2026-05-19)

* Changed secret declarations to require `key`; `Secret.LookupKey()` now returns only `key` and no longer falls back to `name`.

## v0.19.0 (2026-04-30)

* Added `HostingProviders` as the primary deployment surface, with provider names supplied by map keys and `HostingEnvironment` reduced to an ordered provider-name selection.
* Changed provider filters from nested `filter.include` / `filter.exclude` fields to root-level `include` / `exclude` fields; exclude rules continue to win when both include and exclude match.
* Removed `HostingProvider.Name`; callers name providers with `HostingProviders` map keys.

## v0.18.2 (2026-04-28)

* Fixed Git deploys to create a configured branch that is missing from a non-empty remote repository.

## v0.18.1 (2026-04-27)

* Changed JSON and YAML provider decoding to parse config passively; provider engines are resolved during validation, planning, and deployment.

## v0.18.0 (2026-04-27)

Complete redesign of the `hyperproviders` package.
Every exported type changes; the `hypertemplates-go` dependency is removed entirely.

* Added `HostingProviders` and `HostingEnvironment` types for map-named providers and provider-name selections.
* Added `HostingProvider` concrete type and `HostingProviderEngine` interface, separating shared provider configuration from provider-specific deployment behavior.
* Added include/exclude rules over path regexes and mimetype regexes — routes files to the right provider in multi-provider environments.
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
* Changed `HostingProvider.Deploy` and `HostingEnvironment.Deploy` accept a deployment root directory; hyperproviders owns file discovery, filtering, and provider fan-out internally.
* Changed Provider-specific serialized settings now live under `HostingProvider.Config`; shared provider fields remain at the provider object root.
* Changed S3 upload worker configuration moved under provider-specific `config.workers` instead of a root-level provider concurrency setting.
* Changed `HostingProvider` now validates provider-specific settings through its registered `HostingProviderEngine`.
* Changed symlinks under deployment roots are rejected instead of followed or silently skipped.
* Changed `Deploy` now takes `Session` so callers can choose secret provider precedence per deployment.
* Changed `Secrets.Get` returns `(*Secret, error)` instead of `*Secret` so missing-secret errors are explicit.
* Changed secret declarations no longer include `provider` or `unsafe_value`; durable config names the required secret and lookup key only.
* Removed `HyperProvider`, `HyperProviderConfig`, and `HyperProviderEngine` types.
* Removed the `hypertemplates-go` dependency (HyperFile semantics are not needed inside a deployment engine).
* Removed the `aws-sdk-go-v2/feature/s3/manager` dependency (single-part PUTs only; keeps S3 ETag == MD5).

## v0.17.3 (2025-09-02)

Initial release of the `hyperproviders` package from the standalone `herdworks/hyperproviders-go` repo.

* Completed initial migration from `herdworks/hypertexting-monorepo`
* Added `github.com/herdworks/hypertemplates-go v0.17.3` dependency to `go.sum`
* Adopted `v` prefixed tag naming convention beginning with the `v0.17.3` release

## 0.17.2

See `herdworks/hypertexting-monorepo` for version 0.17.2 and prior changes.
