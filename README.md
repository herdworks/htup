# HyperProviders

HyperProviders prepares and executes a declarative deployment of one build
directory to one or more selected hosting providers. It includes S3-compatible
object storage and Git providers.

## Declarative deployments

A `Deployment` is the high-level operation. Construct it with the providers
selected for this attempt, the build root, and whether the build is complete or
incremental. `Prepare` validates local configuration, loads deployment
instructions, discovers the build once, and materializes an immutable plan. It
does not resolve credentials or write to a remote service. `Execute` performs
the prepared plan after every provider passes its readiness check.

```go
import (
    "context"

    hp "github.com/herdworks/hyperproviders-go"
    "github.com/herdworks/hyperproviders-go/providers/runtime"
    "github.com/herdworks/hyperproviders-go/providers/s3"
)

providers := hp.HostingProviders{
    "assets": {
        Kind:     s3.Kind,
        Endpoint: "https://example.r2.cloudflarestorage.com",
        BaseURL:  "https://assets.example.com",
        Config: map[string]any{
            "bucket": "my-site-assets",
            "region": "auto",
        },
        Secrets: hp.Secrets{
            {Name: "access_key_id", Key: "R2_ACCESS_KEY_ID"},
            {Name: "secret_access_key", Key: "R2_SECRET_ACCESS_KEY"},
        },
    },
}

deployment := &hp.Deployment{
    Providers: providers,
    Path:      "./build",
    Kind:      hp.CompleteDeployment,
}

if err := deployment.Prepare(context.Background(), nil); err != nil {
    // The plan is not ready; no provider operation has run.
}
plan := deployment.Plan() // A caller-owned snapshot, suitable for a dry run.
_ = plan

secrets := hp.SecretsProviders{runtime.New(runtime.Secrets{
    "R2_ACCESS_KEY_ID":     hp.StringSecret(accessKey),
    "R2_SECRET_ACCESS_KEY": hp.StringSecret(secretAccessKey),
})}
if err := deployment.Execute(context.Background(), secrets, nil); err != nil {
    // No completed or failed Deployment may be executed again.
}
```

`CompleteDeployment` asks a provider to deploy the complete build; providers
that support replacement, such as Git, remove files absent from that build.
`IncrementalDeployment` overlays the supplied files and preserves remote files
that are absent from the build. `Instructions()`, `Plan()`, and `Status()`
return snapshots of the prepared deployment state; changing those returned
values cannot change an eventual execution.

Each selected provider produces exactly one operation. Operations with
applicable preconditions run sequentially in instruction order; remaining
operations run concurrently after they succeed. A provider's `include` and
`exclude` path/MIME rules determine both file ownership and whether it owns a
precondition target. The same artifact can intentionally belong to multiple
providers.

## Build-root workspace and instructions

The build root may contain two deployment-control paths:

- `.deployment.json` declares conditional writes, ignored build files, and
  files that must be uploaded after the ordinary bulk set.
- `.deployment/` is reserved workspace state. HyperProviders never deploys it.
  Generators may place the byte sources referenced by `.deployment.json` here;
  the Git provider also uses it for its local checkout and deployment logs.

This makes the build output self-contained for upstream generators. For
example, a sync-aware generator can write its expected and pending state beside
the build artifacts:

```text
build/
  index.html
  .hypersync/
    manifests/next.json
    revision.json
  .deployment/
    expected-revision.json
    pending-revision.json
  .deployment.json
```

```json
{
  "preconditions": [
    {
      "target": ".hypersync/revision.json",
      "expected": ".deployment/expected-revision.json",
      "contents": ".deployment/pending-revision.json"
    }
  ],
  "ignore": ["drafts/*"],
  "enqueue": [".hypersync/manifests/*.json", ".hypersync/revision.json"]
}
```

`target`, `expected`, and `contents` are normalized build-relative paths.
Omitting `expected` is a create-only precondition; a present but empty expected
file requires an existing empty target. `contents` and `expected` are read and
copied during preparation, so later changes to their source files do not alter
the plan. Provider filters classify a precondition by its target path (for
example, `revision.json` is JSON), not the extension of the local source file.

`enqueue` is a declaration-ordered list of glob patterns. The first matching
pattern claims a file; claimed files are removed from bulk uploads and written
one at a time after bulk uploads. `.deployment.json.ignore` replaces the legacy
`.deploymentignore`; using both is an error.

## Providers

- **S3** supports S3-compatible services including AWS S3, Cloudflare R2,
  MinIO, and DigitalOcean Spaces. Precondition replacements compare exact
  remote bytes and use the remote ETag for the conditional write.
- **Git** publishes one final commit to a remote branch. It validates
  preconditions against the fetched branch base, materializes the final tree,
  and safely fails if the branch advances before push.

Import each provider package used by a configuration so its engine is
registered before preparation:

```go
import (
    _ "github.com/herdworks/hyperproviders-go/providers/git"
    _ "github.com/herdworks/hyperproviders-go/providers/s3"
)
```

## `htdt`

`htdt` reads JSON or YAML provider/environment configuration. An environment
is an explicit ordered selection from the provider map.

```sh
go install github.com/herdworks/hyperproviders-go/cmd/htdt@latest

htdt validate --config ./htdt.json
htdt ready --config ./htdt.json --environment production
htdt deploy --config ./htdt.json --environment production --source ./build --mode complete --dry-run
htdt deploy --config ./htdt.json --environment production --source ./build --mode complete
```

`--dry-run` calls `Prepare` and prints the sequential and concurrent operations,
including each precondition, bulk artifact, and enqueued artifact. It performs
no provider readiness check or remote write.

Example configuration:

```json
{
  "version": 1,
  "providers": {
    "assets": {
      "kind": "s3",
      "endpoint": "https://<accountid>.r2.cloudflarestorage.com",
      "config": { "bucket": "my-site-assets", "region": "auto" },
      "secrets": [
        { "name": "access_key_id", "key": "R2_ACCESS_KEY_ID" },
        { "name": "secret_access_key", "key": "R2_SECRET_ACCESS_KEY" }
      ]
    }
  },
  "environments": { "production": ["assets"] }
}
```

`validate` checks static provider configuration. `ready` also resolves local
credentials (from environment variables in `htdt`) without contacting a remote
service.

## Development

Run tests from this module:

```sh
go test ./... -v
```

The Git tests use temporary local `file://` remotes. The S3 tests use local HTTP
test servers. The AWS SDK requires Go 1.25 or newer.
