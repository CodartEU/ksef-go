# CLAUDE.md

## Project: ksef-go

A Go SDK for Poland's National e-Invoicing System (KSeF) 2.0 API.

### What this project is

An open-source Go library that provides a clean, idiomatic Go interface to the
KSeF 2.0 API. It handles authentication, session management, invoice submission,
invoice retrieval, encryption, and FA(3) XML generation/validation.

This is NOT an application — it's a library consumed by other Go programs.

### Tech stack and constraints

- **Language:** Go 1.26+ (use latest stable features including rangefunc if useful)
- **Dependencies:** MINIMAL. Prefer stdlib over third-party packages.
  - `encoding/xml` for XML handling
  - `crypto/*` packages for AES, RSA, X509, XAdES
  - `net/http` for HTTP client (no third-party HTTP clients)
  - `encoding/json` for JSON (only in examples/tests)
  - ALLOWED third-party: `github.com/beevik/etree` for complex XML manipulation if stdlib is insufficient
  - ALLOWED third-party: `golang.org/x/crypto` if needed for specific crypto operations
  - NO other third-party dependencies without explicit approval
- **No CGO:** Must compile without CGO for easy cross-compilation
- **Test framework:** stdlib `testing` package only. No testify, no gomega.

### Architecture
ksef-go/
├── pkg/ksef/            # Public API — what users import
│   ├── client.go        # Main Client struct, NewClient()
│   ├── options.go       # Functional options for client config
│   ├── environment.go   # Environment enum (Test, Demo, Production)
│   ├── errors.go        # Error types
│   ├── auth/            # Authentication (token, XAdES, certificate)
│   │   ├── token.go     # Token-based auth
│   │   ├── xades.go     # XAdES signature auth
│   │   └── session.go   # Auth session management
│   ├── session/         # KSeF session management
│   │   ├── online.go    # Online sessions
│   │   ├── batch.go     # Batch sessions
│   │   └── types.go     # Session types
│   ├── invoice/         # Invoice operations
│   │   ├── submit.go    # Invoice submission
│   │   ├── status.go    # Status polling
│   │   ├── query.go     # Invoice queries
│   │   ├── download.go  # Invoice/UPO download
│   │   └── types.go     # Invoice types
│   ├── fa3/             # FA(3) XML schema
│   │   ├── builder.go   # Invoice builder (Go structs → FA(3) XML)
│   │   ├── types.go     # FA(3) struct definitions
│   │   ├── marshal.go   # XML marshaling
│   │   └── validate.go  # Schema validation
│   └── crypto/          # Encryption helpers
│       ├── aes.go       # AES-256-CBC encryption
│       ├── rsa.go       # RSA-OAEP key wrapping
│       └── xades.go     # XAdES signature generation
├── internal/            # Private implementation details
│   ├── httpclient/      # HTTP client wrapper with retry logic
│   │   ├── client.go
│   │   └── retry.go
│   └── xmlutil/         # XML helper utilities
│       └── namespace.go
├── testdata/            # Test fixtures
│   ├── ksef-openapi.json
│   ├── fa3-schema.xsd
│   ├── sample-invoice.xml
│   └── ksef-test-public-key.pem
├── examples/            # Usage examples
│   ├── basic/           # Simple invoice submission
│   │   └── main.go
│   └── batch/           # Batch invoice processing
│       └── main.go
├── CLAUDE.md            # This file
├── README.md
├── LICENSE              # MIT
├── go.mod
├── go.sum
└── .github/
└── workflows/
└── ci.yml       # GitHub Actions CI

### Code style and conventions

- Follow standard Go conventions (gofumpt, go vet, golint)
- All exported types and functions MUST have godoc comments
- Error handling: return wrapped errors with `fmt.Errorf("operation: %w", err)`
- Context: all operations that make HTTP calls MUST accept `context.Context` as first param
- Naming: use Go conventions — `InvoiceStatus` not `Invoice_Status`, `NIP` not `Nip`
- No panics in library code — always return errors
- Use functional options pattern for client configuration
- Prefer table-driven tests
- Use `t.Helper()` in test helper functions
- Test files go next to the code they test: `submit.go` → `submit_test.go`

### KSeF API details

- **API version:** 2.0.0
- **Base URLs:**
  - TEST: `https://api-test.ksef.mf.gov.pl/v2`
  - DEMO: `https://api-demo.ksef.mf.gov.pl/v2`
  - PRODUCTION: `https://api.ksef.mf.gov.pl/v2`
- **OpenAPI spec:** `testdata/ksef-openapi.json`
- **Auth methods:** Token, XAdES certificate
- **Encryption:** AES-256-CBC for invoice payload, RSA-OAEP for symmetric key wrapping
- **Invoice schema:** FA(3) — XML format, XSD at `testdata/fa3-schema.xsd`
- **Rate limits (production):** 100 req/s, 300 req/min, 1200 req/h (general)
- **Rate limits (test):** 10x production values

### What NOT to do

- Do NOT add any web framework dependencies
- Do NOT add logging libraries — use `log/slog` if logging is needed at all
- Do NOT create a CLI tool in this repo — SDK only
- Do NOT store any real credentials or tokens in code or tests
- Do NOT use `interface{}` or `any` where a concrete type is possible
- Do NOT use init() functions
- Do NOT add build tags unless absolutely necessary
