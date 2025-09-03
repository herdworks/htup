---
description: Instructions for writing Go code
applyTo: "*.go,**/*.go,**/go.mod,**/go.sum"
---

# Go Development Instructions

Follow idiomatic Go practices and community standards when writing Go code.
Familiarize yourself with the instructions in [Effective Go](https://go.dev/doc/effective_go), [Go Code Review Comments](https://go.dev/wiki/CodeReviewComments), and [Google's Go Style Guide](https://google.github.io/styleguide/go/).

## Golang Dependency Management
* Use Go modules (`go.mod` / `go.sum`)
* Run `go mod tidy` to clean up unused dependencies
* Vendor only when necessary

## Golang Packages & Naming
* Use lowercase, single-word package names (no underscores or hyphens)
* Avoid stutter (e.g. `http.Server` not `http.HTTPServer`)
* Keep `main` packages in `cmd/`; use `internal/` for non-exported packages
* Avoid generic names like `util` or `common`

## Golang Formatting & Tools
* Always format with `gofmt` (or `go fmt`)
* Use `goimports` to manage imports
* Run `go vet` and `golangci-lint` before committing

## Golang Errors
* Use `errors.New` for static errors, `fmt.Errorf` with `%w` for wrapped errors
* Compare with `errors.Is` / `errors.As`
* Keep error messages lowercase without punctuation
* Always name error variables `err`

## Golang Concurrency
* Don’t start goroutines in libraries — let the caller control concurrency
* Always know how goroutines exit; avoid leaks
* Close channels only from the sender side
* Use `select` for non-blocking operations
* Never modify maps concurrently without a lock

## Golang Testing
* Use table-driven tests
* Place tests in `_test.go` files next to the code
* Mark helpers with `t.Helper()` and use `t.Cleanup()` for teardown
* Use subtests with `t.Run` for clarity

## Golang Common Pitfalls
* Nil interfaces vs nil pointers are not the same
* Overusing `interface{}` instead of small, focused interfaces
* Forgetting to make the zero value useful
* Not using `defer` for cleanup