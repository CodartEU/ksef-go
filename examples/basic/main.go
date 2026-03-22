// Package main demonstrates the full KSeF invoice lifecycle using ksef-go:
//
//	authenticate → open session → build invoice → submit → poll → download → terminate → UPO
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
//	KSEF_NIP=1234567890 KSEF_TOKEN=your-token go run examples/basic/main.go
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
	// ── Configuration from environment ───────────────────────────────────────

	nip := mustEnv("KSEF_NIP")
	token := mustEnv("KSEF_TOKEN")

	pubKeyPath := os.Getenv("KSEF_PUBKEY_PATH")
	if pubKeyPath == "" {
		pubKeyPath = "../../testdata/ksef-test-public-key.pem"
	}

	// ── Step 0: Set up the HTTP client ───────────────────────────────────────
	//
	// The internal httpclient wraps net/http with retry logic, structured
	// logging, and automatic KSeF error parsing. Point it at the KSeF test
	// environment base URL.

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	hc := httpclient.New(
		ksef.Test.BaseURL(),
		&http.Client{Timeout: 30 * time.Second},
		logger,
		httpclient.DefaultRetryConfig,
	)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// ── Step 1: Load the KSeF environment public key ─────────────────────────
	//
	// The KSeF test environment public key is used to encrypt the API token
	// before submission (RSA-OAEP / SHA-256). Download the current key from:
	// https://ksef.mf.gov.pl/static/etap3/api/KSeF-test-public.pem

	pubKeyPEM, err := os.ReadFile(pubKeyPath)
	if err != nil {
		log.Fatalf("read public key %q: %v", pubKeyPath, err)
	}
	pubKey, err := ksefcrypto.LoadPublicKeyFromPEM(pubKeyPEM)
	if err != nil {
		log.Fatalf("parse public key: %v", err)
	}

	// ── Step 2: Authenticate ─────────────────────────────────────────────────
	//
	// TokenAuthenticator performs the full KSeF token auth flow:
	//   challenge → encrypt token (RSA-OAEP) → submit → poll → redeem tokens

	authenticator := auth.NewTokenAuthenticator(hc, pubKey)
	authResult, err := authenticator.Authenticate(ctx, nip, token)
	if err != nil {
		log.Fatalf("authenticate: %v", err)
	}
	fmt.Printf("Authenticated. Access token valid until %s\n",
		authResult.AccessTokenValidUntil.Format(time.RFC3339))

	// ── Step 3: Prepare session encryption ───────────────────────────────────
	//
	// Each KSeF session uses a unique AES-256 key to encrypt invoices. The
	// plaintext key is kept in memory; the key wrapped with RSA-OAEP is sent
	// to KSeF when opening the session.

	aesKey, err := ksefcrypto.GenerateAESKey()
	if err != nil {
		log.Fatalf("generate AES key: %v", err)
	}

	// Generate a random 16-byte IV for the session open request.
	iv := make([]byte, 16)
	if _, err := rand.Read(iv); err != nil {
		log.Fatalf("generate IV: %v", err)
	}

	// Wrap the AES key with the KSeF environment public key.
	encryptedAESKey, err := rsa.EncryptOAEP(sha256.New(), rand.Reader, pubKey, aesKey, nil)
	if err != nil {
		log.Fatalf("wrap AES key: %v", err)
	}

	enc := session.EncryptionInfo{
		SymmetricKey:          aesKey,          // plaintext — kept in memory
		InitializationVector:  iv,              // random IV sent to KSeF
		EncryptedSymmetricKey: encryptedAESKey, // RSA-OAEP wrapped — sent to KSeF
	}

	// ── Step 4: Open an online session ───────────────────────────────────────
	//
	// An online session is a server-side context into which invoices are
	// submitted. It expires automatically if not terminated within ~30 minutes
	// of inactivity. Use defer to guarantee termination even on failure.

	sessionMgr := session.NewManager(hc)
	sess, err := sessionMgr.OpenOnline(ctx, authResult.AccessToken, session.FormCodeFA3, enc)
	if err != nil {
		log.Fatalf("open session: %v", err)
	}
	fmt.Printf("Session opened: ref=%s, valid until %s\n",
		sess.ReferenceNumber, sess.ValidUntil.Format(time.RFC3339))

	// Always terminate the session, even if subsequent steps fail.
	defer func() {
		tCtx, tCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer tCancel()
		if _, err := sess.Terminate(tCtx); err != nil {
			fmt.Fprintf(os.Stderr, "terminate session: %v\n", err)
		}
	}()

	// ── Step 5: Build a FA(3) invoice ────────────────────────────────────────
	//
	// Use the fluent InvoiceBuilder to construct a valid FA(3) VAT invoice.
	// All monetary values are strings with up to two decimal places.

	now := time.Now()
	invoiceNumber := fmt.Sprintf("FV/%04d/%02d/%02d/%d",
		now.Year(), int(now.Month()), now.Day(), now.UnixNano()%100000)

	invoiceXML, err := fa3.NewInvoiceBuilder().
		SetSeller("Moja Firma Sp. z o.o.", nip, fa3.Adres{
			KodKraju: "PL",
			AdresL1:  "ul. Testowa 1",
			AdresL2:  strptr("00-001 Warszawa"),
		}).
		SetBuyer("Kontrahent Sp. z o.o.", "9999999999", fa3.Adres{
			KodKraju: "PL",
			AdresL1:  "ul. Przykładowa 10",
			AdresL2:  strptr("30-001 Kraków"),
		}).
		SetInvoiceNumber(invoiceNumber).
		SetDates(now, now).
		SetPayment(
			fa3.PlatnoscPrzelew,
			now.Add(14*24*time.Hour),
			"PL61109010140000071219812874",
		).
		AddItem(fa3.LineItem{
			Description:  "Usługa programistyczna",
			Unit:         "godz",
			Quantity:     "8",
			UnitNetPrice: "200.00",
			NetValue:     "1600.00",
			VATRate:      fa3.Stawka23,
		}).
		AddItem(fa3.LineItem{
			Description:  "Licencja na oprogramowanie",
			Unit:         "szt",
			Quantity:     "1",
			UnitNetPrice: "500.00",
			NetValue:     "500.00",
			VATRate:      fa3.Stawka23,
		}).
		BuildXML()
	if err != nil {
		log.Fatalf("build invoice XML: %v", err)
	}
	fmt.Printf("Invoice %s built (%d bytes of XML)\n", invoiceNumber, len(invoiceXML))

	// ── Step 6: Submit the invoice ───────────────────────────────────────────
	//
	// SubmitInvoice encrypts invoiceXML with the session's AES key (AES-256-CBC)
	// and sends the ciphertext to KSeF. It returns a reference number for
	// tracking processing status.

	invoiceMgr := invoice.NewManager(hc)
	submitResult, err := invoiceMgr.SubmitInvoice(ctx, sess, invoiceXML)
	if err != nil {
		log.Fatalf("submit invoice: %v", err)
	}
	fmt.Printf("Invoice submitted: ref=%s\n", submitResult.ReferenceNumber)

	// ── Step 7: Poll until processed ─────────────────────────────────────────
	//
	// KSeF processes invoices asynchronously. PollUntilProcessed uses
	// exponential backoff (starting at 3s, capped at 30s) until a terminal
	// status is reached (accepted or error).

	fmt.Println("Waiting for KSeF to process the invoice...")
	invoiceStatus, err := invoiceMgr.PollUntilProcessed(ctx, sess, submitResult.ReferenceNumber, 3*time.Second)
	if err != nil {
		log.Fatalf("poll invoice status: %v", err)
	}

	if !invoiceStatus.Status.Code.IsAccepted() {
		log.Fatalf("invoice rejected (status %d): %s\n  details: %v",
			invoiceStatus.Status.Code,
			invoiceStatus.Status.Description,
			invoiceStatus.Status.Details,
		)
	}
	fmt.Printf("Invoice accepted!\n  KSeF number: %s\n  Invoice number: %s\n",
		invoiceStatus.KSeFNumber, invoiceStatus.InvoiceNumber)

	// ── Step 8: Download the invoice XML from the repository ─────────────────
	//
	// Once accepted, the invoice is available in the KSeF repository by its
	// permanent KSeF number.

	downloadedXML, err := invoiceMgr.DownloadInvoice(ctx, sess, invoiceStatus.KSeFNumber)
	if err != nil {
		log.Fatalf("download invoice: %v", err)
	}
	fmt.Printf("Invoice downloaded from repository (%d bytes)\n", len(downloadedXML))

	// ── Step 9: Terminate the session ────────────────────────────────────────
	//
	// Terminating explicitly triggers immediate session processing. After this
	// call the session transitions to StatusClosed (170) and then to
	// StatusProcessedOK (200) once KSeF finishes. The deferred terminate above
	// handles the case where we reach this point or bail out early.
	//
	// We call it here explicitly so we can log session statistics.

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

	// ── Step 10: Download the individual UPO ─────────────────────────────────
	//
	// UPO (Urzędowe Potwierdzenie Odbioru) is the official receipt confirming
	// KSeF accepted the invoice. It is available immediately after the invoice
	// reaches StatusAccepted, even before the session is fully processed.

	upoBytes, err := invoiceMgr.DownloadUPO(ctx, sess, submitResult.ReferenceNumber)
	if err != nil {
		log.Fatalf("download UPO: %v", err)
	}
	fmt.Printf("UPO downloaded (%d bytes)\n", len(upoBytes))

	// Write the UPO to a local file for inspection.
	upoFile := invoiceStatus.KSeFNumber + ".upo.xml"
	if err := os.WriteFile(upoFile, upoBytes, 0o644); err != nil {
		log.Fatalf("write UPO file: %v", err)
	}
	fmt.Printf("UPO saved to %s\n", upoFile)
}

// mustEnv returns the value of the named environment variable or exits with an
// error message if it is not set.
func strptr(s string) *string { return &s }

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		log.Fatalf("required environment variable %s is not set", key)
	}
	return v
}
