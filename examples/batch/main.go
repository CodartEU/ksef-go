// Package main demonstrates batch invoice submission using ksef-go.
//
// A single KSeF online session accepts multiple invoices submitted sequentially.
// This example opens one session, submits several invoices, polls all of them
// concurrently, then terminates the session and downloads the session-level UPO.
//
// Required environment variables:
//
//	KSEF_NIP          — 10-digit NIP registered in the KSeF test environment
//	KSEF_TOKEN        — API token issued by the KSeF test environment
//	KSEF_PUBKEY_PATH  — path to the KSeF test environment RSA public key PEM file
//	                    (defaults to ../../testdata/ksef-test-public-key.pem)
//
// Run:
//
//	KSEF_NIP=1234567890 KSEF_TOKEN=your-token go run examples/batch/main.go
package main

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/CodartEU/ksef-go/internal/httpclient"
	ksef "github.com/CodartEU/ksef-go/pkg/ksef"
	"github.com/CodartEU/ksef-go/pkg/ksef/auth"
	ksefcrypto "github.com/CodartEU/ksef-go/pkg/ksef/crypto"
	"github.com/CodartEU/ksef-go/pkg/ksef/fa3"
	"github.com/CodartEU/ksef-go/pkg/ksef/invoice"
	"github.com/CodartEU/ksef-go/pkg/ksef/session"
)

// invoiceSpec describes a single invoice to submit in the batch.
type invoiceSpec struct {
	number       string
	buyerName    string
	buyerNIP     string
	description  string
	quantity     string
	unitNetPrice string
	netValue     string
}

// submittedInvoice pairs a submitted invoice spec with its reference number.
type submittedInvoice struct {
	spec invoiceSpec
	ref  string
}

func main() {
	// ── Configuration ─────────────────────────────────────────────────────────

	nip := mustEnv("KSEF_NIP")
	token := mustEnv("KSEF_TOKEN")

	pubKeyPath := os.Getenv("KSEF_PUBKEY_PATH")
	if pubKeyPath == "" {
		pubKeyPath = "../../testdata/ksef-test-public-key.pem"
	}

	// ── Set up shared infrastructure ─────────────────────────────────────────

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	hc := httpclient.New(
		ksef.Test.BaseURL(),
		&http.Client{Timeout: 30 * time.Second},
		logger,
		httpclient.DefaultRetryConfig,
	)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	// ── Load the KSeF environment public key ─────────────────────────────────

	pubKeyPEM, err := os.ReadFile(pubKeyPath)
	if err != nil {
		log.Fatalf("read public key %q: %v", pubKeyPath, err)
	}
	pubKey, err := ksefcrypto.LoadPublicKeyFromPEM(pubKeyPEM)
	if err != nil {
		log.Fatalf("parse public key: %v", err)
	}

	// ── Authenticate ─────────────────────────────────────────────────────────

	authResult, err := auth.NewTokenAuthenticator(hc, pubKey).Authenticate(ctx, nip, token)
	if err != nil {
		log.Fatalf("authenticate: %v", err)
	}
	fmt.Printf("Authenticated (valid until %s)\n",
		authResult.AccessTokenValidUntil.Format(time.RFC3339))

	// ── Prepare encryption for the session ───────────────────────────────────
	//
	// One AES-256 key covers all invoices within the session.

	aesKey, err := ksefcrypto.GenerateAESKey()
	if err != nil {
		log.Fatalf("generate AES key: %v", err)
	}
	iv := make([]byte, 16)
	if _, err := rand.Read(iv); err != nil {
		log.Fatalf("generate IV: %v", err)
	}
	encryptedAESKey, err := rsa.EncryptOAEP(sha256.New(), rand.Reader, pubKey, aesKey, nil)
	if err != nil {
		log.Fatalf("wrap AES key: %v", err)
	}
	enc := session.EncryptionInfo{
		SymmetricKey:          aesKey,
		InitializationVector:  iv,
		EncryptedSymmetricKey: encryptedAESKey,
	}

	// ── Open one session for the whole batch ─────────────────────────────────

	sess, err := session.NewManager(hc).OpenOnline(ctx, authResult.AccessToken, session.FormCodeFA3, enc)
	if err != nil {
		log.Fatalf("open session: %v", err)
	}
	fmt.Printf("Session opened: ref=%s\n", sess.ReferenceNumber)

	// Guarantee the session is always terminated.
	defer func() {
		tCtx, tCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer tCancel()
		if _, err := sess.Terminate(tCtx); err != nil {
			fmt.Fprintf(os.Stderr, "terminate session: %v\n", err)
		}
	}()

	// ── Define the batch of invoices ─────────────────────────────────────────

	now := time.Now()
	invoices := []invoiceSpec{
		{
			number:       fvNumber(now, 1),
			buyerName:    "Alpha Sp. z o.o.",
			buyerNIP:     "1111111111",
			description:  "Wdrożenie systemu",
			quantity:     "1",
			unitNetPrice: "5000.00",
			netValue:     "5000.00",
		},
		{
			number:       fvNumber(now, 2),
			buyerName:    "Beta S.A.",
			buyerNIP:     "2222222222",
			description:  "Usługi konsultingowe",
			quantity:     "20",
			unitNetPrice: "150.00",
			netValue:     "3000.00",
		},
		{
			number:       fvNumber(now, 3),
			buyerName:    "Gamma Sp. k.",
			buyerNIP:     "3333333333",
			description:  "Licencja roczna",
			quantity:     "1",
			unitNetPrice: "1200.00",
			netValue:     "1200.00",
		},
	}

	// ── Submit all invoices sequentially ─────────────────────────────────────
	//
	// The KSeF API requires sequential invoice submission within a session.
	// Concurrent submission within a single session is not supported.

	invoiceMgr := invoice.NewManager(hc)
	submitted := make([]submittedInvoice, 0, len(invoices))

	sellerStreet := "ul. Testowa"
	for _, spec := range invoices {
		xmlBytes, err := buildInvoice(nip, sellerStreet, spec, now)
		if err != nil {
			log.Fatalf("build invoice %s: %v", spec.number, err)
		}

		result, err := invoiceMgr.SubmitInvoice(ctx, sess, xmlBytes)
		if err != nil {
			log.Fatalf("submit invoice %s: %v", spec.number, err)
		}
		fmt.Printf("  Submitted %s → ref=%s\n", spec.number, result.ReferenceNumber)

		submitted = append(submitted, submittedInvoice{spec: spec, ref: result.ReferenceNumber})
	}

	fmt.Printf("All %d invoices submitted. Polling for status...\n", len(submitted))

	// ── Poll all invoices concurrently ───────────────────────────────────────
	//
	// Status polling is read-only and safe to do concurrently across invoices.

	type pollResult struct {
		inv    submittedInvoice
		status *invoice.InvoiceStatus
		err    error
	}

	results := make(chan pollResult, len(submitted))
	var wg sync.WaitGroup

	for _, s := range submitted {
		wg.Add(1)
		go func(si submittedInvoice) {
			defer wg.Done()
			status, err := invoiceMgr.PollUntilProcessed(ctx, sess, si.ref, 3*time.Second)
			results <- pollResult{inv: si, status: status, err: err}
		}(s)
	}

	// Close the channel once all goroutines finish.
	go func() {
		wg.Wait()
		close(results)
	}()

	// Collect and display results.
	var accepted, rejected int
	for r := range results {
		if r.err != nil {
			fmt.Fprintf(os.Stderr, "  ERROR polling %s: %v\n", r.inv.spec.number, r.err)
			rejected++
			continue
		}
		if r.status.Status.Code.IsAccepted() {
			fmt.Printf("  ACCEPTED %s → KSeF: %s\n", r.inv.spec.number, r.status.KSeFNumber)
			accepted++
		} else {
			fmt.Printf("  REJECTED %s → status=%d: %s (details: %v)\n",
				r.inv.spec.number,
				r.status.Status.Code,
				r.status.Status.Description,
				r.status.Status.Details,
			)
			rejected++
		}
	}

	fmt.Printf("\nBatch complete: %d accepted, %d rejected\n", accepted, rejected)

	// ── Terminate the session ─────────────────────────────────────────────────
	//
	// Explicit termination triggers immediate session closure and generates the
	// session-level UPO once KSeF finishes processing.

	sessionStatus, err := sess.Terminate(ctx)
	if err != nil {
		log.Fatalf("terminate session: %v", err)
	}
	fmt.Printf("Session terminated: status=%d (%s)\n",
		sessionStatus.Status.Code, sessionStatus.Status.Description)
	if sessionStatus.InvoiceCount != nil {
		fmt.Printf("  Invoices: total=%d accepted=%d rejected=%d\n",
			*sessionStatus.InvoiceCount,
			*sessionStatus.SuccessfulInvoiceCount,
			*sessionStatus.FailedInvoiceCount,
		)
	}

	// The session-level UPO is available via sessionStatus.UPO once the session
	// transitions to StatusProcessedOK (200). Poll sess.Status() if needed.
	if sessionStatus.UPO != nil {
		fmt.Printf("Session UPO available (%d pages):\n", len(sessionStatus.UPO.Pages))
		for i, page := range sessionStatus.UPO.Pages {
			fmt.Printf("  Page %d: %s (expires %s)\n", i+1, page.DownloadURL,
				page.ExpiresAt.Format(time.RFC3339))
		}
	} else {
		fmt.Println("Session UPO not yet available — poll sess.Status() until StatusProcessedOK (200)")
	}
}

func strptr(s string) *string { return &s }

// buildInvoice constructs FA(3) XML for a single invoice spec.
func buildInvoice(sellerNIP, sellerStreet string, spec invoiceSpec, now time.Time) ([]byte, error) {
	return fa3.NewInvoiceBuilder().
		SetSeller("Moja Firma Sp. z o.o.", sellerNIP, fa3.Adres{
			KodKraju: "PL",
			AdresL1:  sellerStreet + " 1",
			AdresL2:  strptr("00-001 Warszawa"),
		}).
		SetBuyer(spec.buyerName, spec.buyerNIP, fa3.Adres{
			KodKraju: "PL",
			AdresL1:  "ul. Kupiecka 1",
			AdresL2:  strptr("00-002 Warszawa"),
		}).
		SetInvoiceNumber(spec.number).
		SetDates(now, now).
		SetPayment(fa3.PlatnoscPrzelew, now.Add(14*24*time.Hour), "PL61109010140000071219812874").
		AddItem(fa3.LineItem{
			Description:  spec.description,
			Unit:         "szt",
			Quantity:     spec.quantity,
			UnitNetPrice: spec.unitNetPrice,
			NetValue:     spec.netValue,
			VATRate:      fa3.Stawka23,
		}).
		BuildXML()
}

// fvNumber generates a unique invoice number for the given sequence position.
func fvNumber(now time.Time, seq int) string {
	return fmt.Sprintf("FV/%04d/%02d/%02d/%d/%d",
		now.Year(), int(now.Month()), now.Day(), now.UnixNano()%100000, seq)
}

// mustEnv returns the value of the named environment variable or exits.
func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		log.Fatalf("required environment variable %s is not set", key)
	}
	return v
}
