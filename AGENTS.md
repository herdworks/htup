# AGENTS.md

Greetings, coding agent. Welcome to the team!

* [Project Instructions](#project-instructions)
* [General Instructions](#general-instructions)
* [Programming Languages](#programming-languages)

--
## Project Instructions

<!-- Project Instructions take priority over all other instructions -->

### Development Instructions

* Always run `go fmt ./...` before committing

### Testing Instructions

* Test this project with `go test ./... -v`

--
## General Instructions

<!-- General Instructions are default preferences and best practices -->

### Best Practices

* Always refer to `README.md` for a description of this project's goals
* Write simple, clear, and idiomatic code
* Favor clarity and simplicity over cleverness
* Follow the principle of least surprise
* Write "straightline" (left-aligned) code wherever possible, exiting early to avoid nesting
* Keep line length reasonable (no hard limit, but assume readability suffers beyond 120 characters)
* Add blank lines to separate logical groups of code
* When asked to do something that deviates from these instruction (AGENTS.md), ask for clarification!

### Development Environment

* Use `curl` for performing HTTP requests or when documenting example HTTP requests
* Use `git` for revision control
* Use `.envrc` files to manage environment variables (see: https://direnv.net)

### Dependency Management

* Use standard libraries liberally
* Use language-native dependency management tools (if available)
* Avoid adding dependencies when the problem can be solved in "dozens" of lines
* Avoid adding dependencies when implementing core differentiation features
* When evaluating dependencies, prefer projects that make extensive use of standard libraries
* When evaluating dependencies, prefer mature projects with permissive licenses (MIT, BSD, Apache)
* Evaluate transitive dependencies (via dependency graph) before adding
* Pin versions for reproducibility
* Avoid circular dependencies at all costs

### Methods & Functions

* Functions should do one thing well; extract helpers when complexity grows
* Functions that need more than ~3-4 arguments are a code smell and should be refactored to accept a type
* If a function accepts a type (class or struct) as an argument, it should probably be a method
* If a function uses multiple optional arguments, wrap them in a single options object argument
* Use default argument values for optional arguments
* Use conditional statements and early returns to validate function inputs and avoid nesting;
* Front-load functions with "if (condition) { return }" style one-liners as a best practice
* Prefer pattern matching / switch statements where the language supports it
* If a function returns an error, it should be the last return value

### Errors

* Propagate errors with context; surface actionable messages without leaking sensitive data
* Return errors (preferred) OR log them, but never both
* Handle errors at the appropriate level
* Consider using structured errors for better debugging

### Serialization & Deserialization

* When implementing custom types, always add support for serialization & deserialization
* Serialization & deserialization implementations should always support JSON
* Where possible, serialization & deserialization implementations should also support YAML
* If serialization & deserialization support for TOML and/or XML is cheap, add them as well

### Cryptography

* Always use standard library cryptography packages unless otherwise noted
* Never implement your own cryptography
* Use cryptography standard libraries for random number generation
* Use strong key sizes and modern algorithms (AES-256, SHA-256 or better). Avoid MD5/SHA-1.
* Always store passwords using bcrypt or similar
* Always use TLS for network communication

### Secrets

* Never hardcode secrets; always use environment variables or secret managers

### Concurrency

* Minimize shared mutable state and prefer message passing to maintain clear ownership
* Use locks sparingly — keep scope minimal and never nest locks without strict ordering
* Never perform heavy I/O or call out to third-party code while holding a lock
* Apply timeouts to all external calls and only retry with bounded backoff + jitter
* Use bounded queues and thread pools to control resource utilization
* Assume race conditions exist until proven otherwise — test with stress, chaos, and randomized scheduling
* Make operations idempotent and use operation identifiers/deduplication where side effects matter
* Keep concurrency mechanisms separate from core business logic
* Always use cooperative cancellation to prevent orphaned operations and treat cancellation as distinct from errors

### Comments

* Write comments in complete sentences
* Don’t repeat the code in prose
* Document why, not what, unless the what is complex
* Start sentences with the name of the thing being described
* Use line comments for most comments
* Use multi-line/block comments sparingly
* Comments should include links to specifications and/or reference documents when appropriate

### Tests

* Keep tests close to the code being tested
* Use the `_test` or `_tests` suffixes for organizing test directories and/or files
* Use the `_helper` suffix for organizing test helper directories and/or files
* Use the `_sampledata` suffix for organizing test data directories and/or files
* Test identifiers should include the target function name and scenario
* Test both success and error cases
* Tests should run deterministically (no hidden time or network dependencies unless explicitly mocked)
* Keep tests fast; long-running tests should be clearly marked (integration, e2e)
* Tests should always clean up after themselves

### Documentation

* Every project should have a CHANGELOG based on "Keep a Changelog" (https://keepachangelog.com/)
* Every project should have at least one README
* README files should always be in Markdown format
* Keep documentation close to code
* Update documentation when code changes
* READMEs should include the following:
  * An overview or introduction with clear explanation of the "why" as much as the "what"
  * Project or component goals
  * Project or component architecture, including major dependencies
  * Basic usage examples
  * Clear setup instructions
  * A troubleshooting section

### Commits

* Reference issues or tickets when relevant
* Run tests before committing
* Keep commits focused and atomic
* Review diffs before committing
* Write meaningful commit messages
* Follow semantic versioning best practices (https://semver.org)

### Common Pitfalls to Avoid

* Not checking errors
* Ignoring race conditions
* Forgetting to close resources (files, connections)
* Using global variables unnecessarily
* Swallowing errors instead of handling or propagating them
* Over-optimizing prematurely (especially around concurrency)

### Operations

* Assume all source code will be hosted on GitHub.com
* Assume all projects will use GitHub Actions for CI/CD

--
## Programming Languages

<!-- Programming Language instructions provide additional guidance beyond General Instructions and Project Instructions -->

### JavaScript

* JavaScript (NodeJS) is my preferred language for writing serverless functions

### Golang

* Follow idiomatic Go practices and community standards when writing Go code, including:
  * [Effective Go](https://go.dev/doc/effective_go)
  * [Go Code Review Comments](https://go.dev/wiki/CodeReviewComments)
  * [Google's Go Style Guide](https://google.github.io/styleguide/go/)
* Use Go modules (`go.mod` / `go.sum`)
* Run `go mod tidy` to clean up unused dependencies
* Always format with `go fmt` (or `gofmt`)
* Always use the `testing` package for tests
  * Place tests in `_test.go` files next to the code
  * Mark helpers with `t.Helper()` and use `t.Cleanup()` for teardown
  * Use subtests with `t.Run` for clarity
* Treat the Go X-Repositories as part of the standard library (https://go.dev/wiki/X-Repositories)

### Swift

* Treat Swift Foundation as part of the standard library (https://github.com/swiftlang/swift-foundation)
* When writing CLI tools, always use Swift Argument Parser (https://github.com/apple/swift-argument-parser)
