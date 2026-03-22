# Contributing to ksef-go

Thank you for your interest in contributing. This document explains how to set
up a development environment, run tests, follow the code style, and submit pull
requests.

## Table of contents

- [Development environment](#development-environment)
- [Running tests](#running-tests)
- [Code style](#code-style)
- [Pull request process](#pull-request-process)
- [Reporting issues](#reporting-issues)

## Development environment

### Prerequisites

| Tool | Minimum version | Purpose |
| --- | --- | --- |
| Go | 1.26 | Build and test |
| `golangci-lint` | 1.57 | Lint (CI enforced) |
| `git` | any | Version control |

Install `golangci-lint` following the [official instructions](https://golangci-lint.run/usage/install/).
Do not install it with `go install` — that may produce a version mismatch.

### Clone and build

```bash
git clone https://github.com/CodartEU/ksef-go.git
cd ksef-go
go build ./...
```

There are no `go generate` steps or code generation scripts. The module has no
required third-party dependencies beyond the Go standard library.

### Project layout

```text
pkg/ksef/           Public API — what library consumers import
internal/           Private implementation (not importable by external code)
testdata/           Test fixtures: XSD schema, sample invoices, public keys
examples/           Standalone runnable examples (same module, can use internal/)
```

See [CLAUDE.md](CLAUDE.md) for the full architecture reference.

## Running tests

### Unit tests

```bash
go test ./...
```

All unit tests use only the standard library `testing` package and run without
any external services or credentials. They must pass before a PR is merged.

### Race detector

```bash
go test -race ./...
```

Run with the race detector for any change that touches concurrency.

### Linting

```bash
golangci-lint run
```

The CI pipeline runs `golangci-lint` with the configuration in
[.golangci.yml](.golangci.yml). Fix all lint errors before opening a PR.

### Integration tests

Integration tests exercise the real KSeF test environment. They are skipped
unless `KSEF_INTEGRATION=true` is set.

**Required environment variables:**

| Variable | Description |
| --- | --- |
| `KSEF_INTEGRATION` | Must be `"true"` to enable integration tests |
| `KSEF_TEST_NIP` | 10-digit NIP registered in the KSeF test environment |
| `KSEF_TEST_TOKEN` | API token (token-based auth) |
| `KSEF_TEST_CERT_PATH` | Path to PEM certificate (XAdES auth) |
| `KSEF_TEST_KEY_PATH` | Path to PEM private key (XAdES auth) |

At least one of `KSEF_TEST_TOKEN` or the cert+key pair must be provided.

Register a free test account at [ksef-test.mf.gov.pl](https://ksef-test.mf.gov.pl).

```bash
export KSEF_INTEGRATION=true
export KSEF_TEST_NIP=1234567890
export KSEF_TEST_TOKEN=your-token-here
go test -v -timeout 10m ./pkg/ksef/...
```

Integration tests are not required to pass for contributor PRs — they run in CI
only when a maintainer triggers them with live credentials.

### Running an example

```bash
export KSEF_NIP=1234567890
export KSEF_TOKEN=your-token-here
go run examples/basic/main.go
```

## Code style

### General rules

- Follow standard Go conventions: `gofmt`, `go vet`, `golangci-lint`.
- All exported types and functions **must** have godoc comments.
- Wrap errors with context: `fmt.Errorf("operation: %w", err)`.
- All HTTP-calling functions must accept `context.Context` as their first parameter.
- No panics in library code — always return errors.
- No CGO — the library must compile without `CGO_ENABLED=1`.
- No `init()` functions.
- No `interface{}` / `any` where a concrete type is possible.

### Dependencies

- **No new third-party dependencies** without explicit maintainer approval.
- The only allowed third-party modules are `golang.org/x/crypto` (if needed for
  specific crypto operations) and `github.com/beevik/etree` (complex XML
  manipulation). Open an issue first if you believe a new dependency is required.
- Prefer standard library solutions even when they require more code.

### Tests

- Use the standard `testing` package only — no testify, gomega, or other
  assertion libraries.
- Prefer **table-driven tests** (`for _, tc := range cases { ... }`).
- Use `t.Helper()` inside test helper functions.
- Test files live next to the code they test: `submit.go` → `submit_test.go`.
- Do not store real credentials or tokens in test fixtures.
- Unit tests must not make network calls. Use `httptest.NewServer` for HTTP tests.

### Naming

- Use Go naming conventions throughout: `InvoiceStatus`, not `Invoice_Status`.
- Acronyms stay uppercase: `NIP`, `UPO`, `KSeF`, `AES`, `RSA`.
- Avoid abbreviations unless they are universally understood in the KSeF domain.

### Commit messages

Write commit messages in the imperative mood, with a short subject line (≤72
characters) and an optional body:

```
Add XAdES authentication for certificate-based login

Implements the full XAdES-BES signature flow required by the KSeF
API for entities authenticating with a qualified certificate.
```

Reference relevant issues with `Fixes #123` or `Closes #123` in the body.

## Pull request process

1. **Open an issue first** for anything beyond small bug fixes or typos. Discuss
   the approach before spending time on implementation.
2. **Fork and branch** — create a feature branch from `main`:
   ```bash
   git checkout -b feature/my-feature
   ```
3. **Keep changes focused** — one logical change per PR. Split unrelated changes
   into separate PRs.
4. **Run the full test suite** before pushing:
   ```bash
   go test ./... && golangci-lint run
   ```
5. **Update documentation** — if you add or change public API, update godoc
   comments. If you add a feature, update the README feature table.
6. **Open the PR** against the `main` branch. Fill in the PR template:
   - What problem does this solve?
   - How was it tested?
   - Are there any breaking changes?
7. **Address review feedback** — push additional commits rather than force-pushing,
   so reviewers can see what changed.
8. **Squash on merge** — a maintainer will squash-merge once the PR is approved.

### What gets reviewed

- Correctness and test coverage
- API design consistency with the rest of the library
- Documentation quality
- Dependency footprint
- Security implications (encryption, authentication, input validation)

## Reporting issues

Use [GitHub Issues](https://github.com/CodartEU/ksef-go/issues) to report bugs
or request features. When reporting a bug, include:

- Go version (`go version`)
- Operating system and architecture
- The KSeF environment (Test / Demo / Production)
- A minimal reproducible example
- The full error message and stack trace if applicable

**Do not include real API tokens, NIPs, or invoice data in issue reports.**
Anonymise or redact all sensitive information before posting.
