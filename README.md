# ksef-go — Go SDK for Poland's KSeF 2.0 API

[![Go Reference](https://pkg.go.dev/badge/github.com/CodartEU/ksef-go.svg)](https://pkg.go.dev/github.com/CodartEU/ksef-go)
[![CI](https://github.com/CodartEU/ksef-go/actions/workflows/ci.yml/badge.svg)](https://github.com/CodartEU/ksef-go/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

> **Status: Alpha — API may change before v1.0**

`ksef-go` is an idiomatic Go library for Poland's National e-Invoicing System
(Krajowy System e-Faktur) 2.0 API. It covers the complete invoice lifecycle
without CGO and with minimal third-party dependencies.

## Features

| Feature | Status |
| --- | --- |
| Token authentication (KSeF API token) | ✅ Implemented |
| XAdES certificate-based authentication | ✅ Implemented |
| AES-256-CBC invoice payload encryption | ✅ Implemented |
| RSA-OAEP symmetric key wrapping | ✅ Implemented |
| Online session open / terminate | ✅ Implemented |
| Invoice submission within a session | ✅ Implemented |
| Invoice status polling (exponential backoff) | ✅ Implemented |
| Invoice query with rich filtering | ✅ Implemented |
| Invoice XML download from repository | ✅ Implemented |
| UPO (official receipt) download | ✅ Implemented |
| FA(3) invoice builder (Go structs → XML) | ✅ Implemented |
| FA(3) invoice validation | ✅ Implemented |
| Automatic retry with exponential backoff | ✅ Implemented |
| Structured logging via `log/slog` | ✅ Implemented |
| Integration tests against KSeF test env | ✅ Implemented |
| Batch session support | 🔜 Planned |
| Invoice refresh (token renewal) | 🔜 Planned |

## Requirements

- **Go 1.26+**
- No CGO — compiles on all GOOS/GOARCH targets
- No web frameworks or logging libraries

## Installation

```bash
go get github.com/CodartEU/ksef-go
```

## Quick start

The example below performs the full invoice round-trip against the KSeF test
environment. See [examples/basic/](examples/basic/) and
[examples/batch/](examples/batch/) for complete, runnable versions.

```go
package main

import (
    "context"
    "crypto/rand"
    "crypto/rsa"
    "crypto/sha256"
    "fmt"
    "log"
    "net/http"
    "os"
    "time"

    "github.com/CodartEU/ksef-go/internal/httpclient"
    ksef "github.com/CodartEU/ksef-go/pkg/ksef"
    "github.com/CodartEU/ksef-go/pkg/ksef/auth"
    ksefcrypto "github.com/CodartEU/ksef-go/pkg/ksef/crypto"
    "github.com/CodartEU/ksef-go/pkg/ksef/fa3"
    "github.com/CodartEU/ksef-go/pkg/ksef/invoice"
    "github.com/CodartEU/ksef-go/pkg/ksef/session"
)

func main() {
    ctx := context.Background()

    // 1. Wire up the HTTP client for the KSeF test environment.
    hc := httpclient.New(
        ksef.Test.BaseURL(),
        &http.Client{Timeout: 30 * time.Second},
        nil, // logger — pass slog.Default() to enable debug output
        httpclient.DefaultRetryConfig,
    )

    // 2. Load the KSeF environment RSA public key (encrypt token/AES key).
    pubKeyPEM, _ := os.ReadFile("testdata/ksef-test-public-key.pem")
    pubKey, _ := ksefcrypto.LoadPublicKeyFromPEM(pubKeyPEM)

    // 3. Authenticate with a KSeF API token.
    authResult, err := auth.NewTokenAuthenticator(hc, pubKey).
        Authenticate(ctx, "1234567890" /* NIP */, os.Getenv("KSEF_TOKEN"))
    if err != nil {
        log.Fatal(err)
    }

    // 4. Prepare per-session AES-256 encryption.
    aesKey, _ := ksefcrypto.GenerateAESKey()
    iv := make([]byte, 16)
    rand.Read(iv)
    encKey, _ := rsa.EncryptOAEP(sha256.New(), rand.Reader, pubKey, aesKey, nil)

    enc := session.EncryptionInfo{
        SymmetricKey:          aesKey,
        InitializationVector:  iv,
        EncryptedSymmetricKey: encKey,
    }

    // 5. Open an online session.
    sess, err := session.NewManager(hc).
        OpenOnline(ctx, authResult.AccessToken, session.FormCodeFA3, enc)
    if err != nil {
        log.Fatal(err)
    }
    defer sess.Terminate(ctx) //nolint:errcheck

    // 6. Build a FA(3) invoice.
    invoiceXML, err := fa3.NewInvoiceBuilder().
        SetSeller("Moja Firma Sp. z o.o.", "1234567890", fa3.Adres{
            KodKraju: "PL",
            AdresL1:  "ul. Testowa 1",
            AdresL2:  strptr("00-001 Warszawa"),
        }).
        SetInvoiceNumber("FV/2026/001").
        SetDates(time.Now(), time.Now()).
        AddItem(fa3.LineItem{
            Description: "Usługa doradcza", Unit: "godz",
            Quantity: "8", UnitNetPrice: "200.00",
            NetValue: "1600.00", VATRate: fa3.Stawka23,
        }).
        BuildXML()
    if err != nil {
        log.Fatal(err)
    }

    // 7. Submit the invoice — it is encrypted automatically with the session key.
    invMgr := invoice.NewManager(hc)
    result, err := invMgr.SubmitInvoice(ctx, sess, invoiceXML)
    if err != nil {
        log.Fatal(err)
    }

    // 8. Poll until KSeF assigns a permanent KSeF number.
    status, err := invMgr.PollUntilProcessed(ctx, sess, result.ReferenceNumber, 3*time.Second)
    if err != nil {
        log.Fatal(err)
    }
    if !status.Status.Code.IsAccepted() {
        log.Fatalf("invoice rejected: %s", status.Status.Description)
    }
    fmt.Printf("Accepted! KSeF number: %s\n", status.KSeFNumber)
}
```

## Configuration options

`ksef.NewClient` accepts zero or more functional options:

| Option | Default | Description |
| --- | --- | --- |
| `ksef.WithHTTPClient(hc)` | 30 s timeout | Replace the underlying `*http.Client` |
| `ksef.WithLogger(l)` | `slog.Default()` | Structured logger for debug output |
| `ksef.WithRetryConfig(n, d)` | 3 retries, 500 ms base | Retry count and initial back-off delay |

```go
client, err := ksef.NewClient(ksef.Production,
    ksef.WithLogger(slog.Default()),
    ksef.WithRetryConfig(5, time.Second),
)
```

## Environment variables reference

The examples and integration tests read the following environment variables:

**Basic example** (`examples/basic/`):

| Variable | Required | Description |
| --- | --- | --- |
| `KSEF_NIP` | Yes | 10-digit NIP registered in the KSeF environment |
| `KSEF_TOKEN` | Yes | KSeF API token for token-based authentication |
| `KSEF_PUBKEY_PATH` | No | Path to KSeF environment RSA public key PEM (defaults to `testdata/ksef-test-public-key.pem`) |

**Integration tests** (`pkg/ksef/integration_test.go`):

| Variable | Required | Description |
| --- | --- | --- |
| `KSEF_INTEGRATION` | Yes | Must be set to `true` to enable integration tests |
| `KSEF_TEST_NIP` | Yes | 10-digit NIP registered in the KSeF test environment |
| `KSEF_TEST_TOKEN` | Yes* | KSeF API token for token-based authentication |
| `KSEF_TEST_CERT_PATH` | Yes* | Path to PEM certificate for XAdES authentication |
| `KSEF_TEST_KEY_PATH` | Yes* | Path to PEM private key for XAdES authentication |

\* At least one credential set (`KSEF_TEST_TOKEN` or cert+key pair) must be provided.

## Environments

| Constant | Base URL | Purpose |
| --- | --- | --- |
| `ksef.Test` | `https://api-test.ksef.mf.gov.pl/v2` | Development and testing |
| `ksef.Demo` | `https://api-demo.ksef.mf.gov.pl/v2` | Acceptance testing |
| `ksef.Production` | `https://api.ksef.mf.gov.pl/v2` | Live invoicing |

Register a test account at the
[KSeF test environment portal](https://ksef-test.mf.gov.pl) to obtain a test NIP
and API token.

## Package overview

```text
pkg/ksef/           Main entry point — NewClient, Environment, Option
pkg/ksef/auth/      Authentication: TokenAuthenticator, XAdESAuthenticator
pkg/ksef/session/   Session management: Manager, OnlineSession
pkg/ksef/invoice/   Invoice operations: submit, status, query, download
pkg/ksef/fa3/       FA(3) invoice schema: builder, types, marshal, validate
pkg/ksef/crypto/    Cryptography helpers: AES-256-CBC, RSA-OAEP, XAdES
```

## FA(3) invoice builder

```go
street := "ul. Przykładowa"
invoice, err := fa3.NewInvoiceBuilder().
    SetSeller("Sprzedawca Sp. z o.o.", "1234567890", fa3.Adres{
        KodKraju: "PL",
        AdresL1:  "ul. Przykładowa 1",
        AdresL2:  strptr("00-001 Warszawa"),
    }).
    SetBuyer("Nabywca S.A.", "9876543210", fa3.Adres{ /* ... */ }).
    SetInvoiceNumber("FV/2026/001").
    SetDates(time.Now(), time.Now()).
    SetPayment(fa3.PlatnoscPrzelew, time.Now().Add(14*24*time.Hour), "PL61...").
    SetCurrency("PLN"). // optional — PLN is the default
    AddItem(fa3.LineItem{
        Description:  "Towar A",
        Unit:         "szt",
        Quantity:     "10",
        UnitNetPrice: "100.00",
        NetValue:     "1000.00",
        VATRate:      fa3.Stawka23, // 23 %
    }).
    AddItem(fa3.LineItem{
        Description:  "Towar B (stawka 8%)",
        Unit:         "szt",
        Quantity:     "5",
        UnitNetPrice: "50.00",
        NetValue:     "250.00",
        VATRate:      fa3.Stawka8, // 8 %
    }).
    Build() // returns (*fa3.Faktura, error); use BuildXML() to get []byte directly
```

### VAT rate constants

| Constant | Rate |
| --- | --- |
| `fa3.Stawka23` | 23 % (standard) |
| `fa3.Stawka8` | 8 % (reduced) |
| `fa3.Stawka5` | 5 % (reduced) |
| `fa3.Stawka0` | 0 % |
| `fa3.StawkaZW` | Exempt (zwolniony) |
| `fa3.StawkaNP` | Non-taxable (niepodlegający) |
| `fa3.StawkaOO` | Outside scope (poza VAT) |
| `fa3.StawkaNN` | Not applicable (nie dotyczy) |

### Payment method constants

| Constant | Method |
| --- | --- |
| `fa3.PlatnoscGotowka` | Cash |
| `fa3.PlatnoscKarta` | Card |
| `fa3.PlatnoscPrzelew` | Bank transfer |
| `fa3.PlatnoscCzek` | Cheque |
| `fa3.PlatnoscBarter` | Barter |
| `fa3.PlatnoscInna` | Other |

## Error handling

All errors returned by this library are either plain `error` values or typed
errors that can be inspected with `errors.As`:

```go
var ksefErr *ksef.KSeFError
if errors.As(err, &ksefErr) {
    for _, ex := range ksefErr.Exceptions {
        fmt.Printf("KSeF exception: %s (code %d)\n",
            ex.ExceptionDescription, ex.ExceptionCode)
    }
}

var rateLimitErr *ksef.RateLimitError
if errors.As(err, &rateLimitErr) {
    fmt.Printf("rate limited, retry after: %v\n", rateLimitErr.RetryAfter)
}
```

| Error type | Trigger |
| --- | --- |
| `ksef.KSeFError` | Non-2xx KSeF API response with exception body |
| `ksef.AuthenticationError` | 401 / 403 response |
| `ksef.SessionError` | Session state violation |
| `ksef.ValidationError` | Client-side or server-side validation failure |
| `ksef.RateLimitError` | 429 Too Many Requests |

## Rate limits

| Environment | General | Per minute | Per hour |
| --- | --- | --- | --- |
| Production | 100 req/s | 300 req/min | 1200 req/h |
| Test | 1000 req/s | 3000 req/min | 12000 req/h |

The HTTP client automatically retries `429` responses, honouring the
`Retry-After` header when present.

## FAQ

**Q: Do I need a real company NIP to use this SDK?**
No — the KSeF test environment accepts any valid 10-digit NIP format. Register a
test account at [ksef-test.mf.gov.pl](https://ksef-test.mf.gov.pl) to get credentials.

**Q: Can I submit invoices without opening a session first?**
No — the KSeF 2.0 API requires an open online (or batch) session before invoice
submission. A session is created by `session.Manager.OpenOnline()` and must be
terminated when done.

**Q: What happens if I don't terminate a session?**
KSeF automatically cancels sessions that are idle for longer than ~30 minutes
(status 440 — Cancelled). Invoices submitted to a cancelled session are not
accepted. Always call `sess.Terminate()` or use `defer`.

**Q: When is the UPO available?**
The individual invoice UPO is available immediately once the invoice reaches
`invoice.StatusAccepted` (200). The session-level UPO is available once the
session transitions to `session.StatusProcessedOK` (200), which happens
asynchronously after `Terminate()`.

**Q: How do I query invoices I've received (as a buyer)?**
Use `invoice.Manager.QueryInvoices()` with `SubjectType: invoice.SubjectBuyer`.
You can filter by date range, invoice type, amount, and more.

**Q: Does this library support XAdES authentication?**
Yes — use `auth.NewXAdESAuthenticator(hc)` and call `Authenticate(ctx, nip, certPEM, keyPEM)`.
Both PKCS#1 and PKCS#8 private key formats are accepted.

**Q: Is concurrent use safe?**
The internal HTTP client (`httpclient.Client`) is safe for concurrent use.
`session.OnlineSession` is **not** safe for concurrent use — submit invoices
sequentially within a session.

## Examples

- [examples/basic/](examples/basic/) — full round-trip: auth → session → submit → poll → download → UPO
- [examples/batch/](examples/batch/) — one session, multiple invoices, concurrent status polling

## Contributing

Contributions are welcome. Please open an issue before submitting a pull request
for anything beyond small bug fixes. See [CONTRIBUTING.md](CONTRIBUTING.md) for
the full guide.

## License

[MIT](LICENSE) © 2026 Codart
