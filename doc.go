// Package hyperproviders is the HyperTexting CMS deployment engine.
//
// Deployment is modeled as an [Environment] (named: development / staging /
// production) that owns one-or-more [HostingProvider] values. Each provider
// delegates provider-specific behavior to a registered [HostingProviderEngine].
// Callers pass a deployment root directory plus a [Session], and hyperproviders
// owns file discovery, filtering, secret resolution, and
// provider fan-out. Every file's deployment path is computed relative to the
// source directory root, so a file at "./build/blog/index.html" lands at
// "blog/index.html" under every provider. Symlinks are rejected during discovery.
//
// Provider engine packages such as providers/s3 and providers/git register
// their engines from init so validation, planning, and deployment can bind
// provider-specific behavior.
package hyperproviders
