// Package hyperproviders is the HyperTexting CMS deployment engine.
//
// A [Deployment] combines provider selection, a payload directory, and a
// [DeploymentKind]. It owns planning, file discovery, filtering, secret
// resolution, and provider fan-out. Each provider delegates provider-specific
// behavior to a registered [HostingEngine]. Every file's deployment path is computed relative to the
// source directory root, so a file at "./build/blog/index.html" lands at
// "blog/index.html" under every provider. Symlinks are rejected during discovery.
//
// HostingProvider engine packages such as providers/s3 and providers/git register
// their engines from init so validation, planning, and deployment can bind
// provider-specific behavior.
package hyperproviders
