// Package ksef_test contains integration tests for the KSeF SDK.
//
// Integration tests require live KSeF test-environment credentials and are
// skipped unless the KSEF_INTEGRATION environment variable is set to "true".
//
// Required environment variables:
//
//	KSEF_INTEGRATION=true       — enable this test file
//	KSEF_TEST_NIP               — 10-digit NIP registered in the test environment
//
// At least one of the following credential sets must be provided:
//
//	KSEF_TEST_TOKEN             — token-based auth token
//	KSEF_TEST_CERT_PATH         — path to PEM certificate for XAdES auth
//	KSEF_TEST_KEY_PATH          — path to PEM private key for XAdES auth
package ksef_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/CodartEU/ksef-go/internal/httpclient"
	ksef "github.com/CodartEU/ksef-go/pkg/ksef"
	"github.com/CodartEU/ksef-go/pkg/ksef/auth"
	ksefcrypto "github.com/CodartEU/ksef-go/pkg/ksef/crypto"
	"github.com/CodartEU/ksef-go/pkg/ksef/fa3"
	"github.com/CodartEU/ksef-go/pkg/ksef/invoice"
	"github.com/CodartEU/ksef-go/pkg/ksef/session"
)

// TestMain skips the entire file when KSEF_INTEGRATION is not "true".
func TestMain(m *testing.M) {
	if os.Getenv("KSEF_INTEGRATION") != "true" {
		fmt.Println("skipping integration tests: KSEF_INTEGRATION != true")
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// TestIntegrationRoundTrip performs a full invoice round-trip against the KSeF
// test environment:
//
//  1. Authenticate (token and/or XAdES)
//  2. Open an online session
//  3. Build a sample FA(3) invoice
//  4. Submit the invoice
//  5. Poll until processing completes
//  6. Verify the assigned KSeF number
//  7. Download the invoice XML
//  8. Terminate the session
//  9. Download the UPO
func TestIntegrationRoundTrip(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	nip := requireEnv(t, "KSEF_TEST_NIP")

	hc := httpclient.New(
		ksef.Test.BaseURL(),
		nil,
		nil,
		httpclient.DefaultRetryConfig,
	)

	// State accumulated across subtests.
	var (
		authResult    *auth.AuthResult
		sess          *session.OnlineSession
		submitResult  *invoice.SubmitResult
		invoiceStatus *invoice.InvoiceStatus
		terminated    bool
	)

	// Ensure the session is terminated even if the test panics or exits early.
	t.Cleanup(func() {
		if !terminated && sess != nil {
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cleanupCancel()
			if _, err := sess.Terminate(cleanupCtx); err != nil {
				t.Logf("cleanup: terminate session: %v", err)
			}
		}
	})

	// ── Step 1: Authenticate ─────────────────────────────────────────────────

	t.Run("Authenticate", func(t *testing.T) {
		token := os.Getenv("KSEF_TEST_TOKEN")
		certPath := os.Getenv("KSEF_TEST_CERT_PATH")
		keyPath := os.Getenv("KSEF_TEST_KEY_PATH")

		if token == "" && (certPath == "" || keyPath == "") {
			t.Skip("no credentials: set KSEF_TEST_TOKEN or (KSEF_TEST_CERT_PATH + KSEF_TEST_KEY_PATH)")
		}

		// Token-based authentication.
		if token != "" {
			t.Run("Token", func(t *testing.T) {
				pubKey := loadKSeFPublicKey(t)
				a := auth.NewTokenAuthenticator(hc, pubKey)
				result, err := a.Authenticate(ctx, nip, token)
				if err != nil {
					t.Fatalf("token auth failed: %v", err)
				}
				if authResult == nil {
					authResult = result // first success is used for the session
				}
				t.Logf("token auth OK: ref=%s, valid until %v",
					result.ReferenceNumber, result.AccessTokenValidUntil)
			})
		}

		// XAdES certificate-based authentication.
		if certPath != "" && keyPath != "" {
			t.Run("XAdES", func(t *testing.T) {
				certPEM, err := os.ReadFile(certPath)
				if err != nil {
					t.Fatalf("read certificate %q: %v", certPath, err)
				}
				keyPEM, err := os.ReadFile(keyPath)
				if err != nil {
					t.Fatalf("read private key %q: %v", keyPath, err)
				}
				a := auth.NewXAdESAuthenticator(hc)
				result, err := a.Authenticate(ctx, nip, certPEM, keyPEM)
				if err != nil {
					t.Fatalf("XAdES auth failed: %v", err)
				}
				if authResult == nil {
					authResult = result
				}
				t.Logf("XAdES auth OK: ref=%s, valid until %v",
					result.ReferenceNumber, result.AccessTokenValidUntil)
			})
		}
	})
	if t.Failed() || authResult == nil {
		return
	}

	// ── Step 2: Open online session ──────────────────────────────────────────

	t.Run("OpenOnlineSession", func(t *testing.T) {
		enc := buildEncryptionInfo(t)
		t.Logf("session open request: formCode=%s encryptedKeyLen=%d ivLen=%d",
			session.FormCodeFA3, len(enc.EncryptedSymmetricKey), len(enc.InitializationVector))

		mgr := session.NewManager(hc)
		var err error
		sess, err = mgr.OpenOnline(ctx, authResult.AccessToken, session.FormCodeFA3, enc)
		if err != nil {
			t.Fatalf("open session: %v", err)
		}
		t.Logf("session opened: ref=%s, valid until %v", sess.ReferenceNumber, sess.ValidUntil)

		// Log the session status immediately after opening to confirm it is active.
		status, statusErr := sess.Status(ctx)
		if statusErr != nil {
			t.Logf("session status (post-open): error: %v", statusErr)
		} else {
			t.Logf("session status (post-open): code=%d description=%q details=%v",
				status.Status.Code, status.Status.Description, status.Status.Details)
		}
	})
	if t.Failed() {
		return
	}

	// ── Step 3: Build sample invoice ─────────────────────────────────────────

	var invoiceXML []byte
	t.Run("BuildInvoice", func(t *testing.T) {
		var err error
		invoiceXML, err = buildSampleInvoice(nip)
		if err != nil {
			t.Fatalf("build invoice: %v", err)
		}
		t.Logf("invoice XML built: %d bytes", len(invoiceXML))
	})
	if t.Failed() {
		return
	}

	// ── Step 4: Submit the invoice ───────────────────────────────────────────

	t.Run("SubmitInvoice", func(t *testing.T) {
		// Re-check session status immediately before submitting.
		preSendStatus, statusErr := sess.Status(ctx)
		if statusErr != nil {
			t.Logf("pre-submit session status: error: %v", statusErr)
		} else {
			t.Logf("pre-submit session status: code=%d description=%q details=%v",
				preSendStatus.Status.Code, preSendStatus.Status.Description, preSendStatus.Status.Details)
		}

		mgr := invoice.NewManager(hc)
		t.Logf("submit request: sessionRef=%s invoiceXMLLen=%d aesKeyLen=%d ivLen=%d",
			sess.ReferenceNumber, len(invoiceXML), len(sess.AESKey), len(sess.IV))
		var err error
		submitResult, err = mgr.SubmitInvoice(ctx, sess, invoiceXML)
		if err != nil {
			// Log full error details including any Details fields.
			var ksefErr *ksef.KSeFError
			if errors.As(err, &ksefErr) {
				for i, ex := range ksefErr.Exceptions {
					t.Logf("exception[%d]: code=%d desc=%q details=%v",
						i, ex.ExceptionCode, ex.ExceptionDescription, ex.Details)
				}
			}
			t.Fatalf("submit invoice: %v", err)
		}
		t.Logf("invoice submitted: ref=%s", submitResult.ReferenceNumber)
	})
	if t.Failed() {
		return
	}

	// ── Step 5: Poll until processed ─────────────────────────────────────────

	t.Run("PollStatus", func(t *testing.T) {
		mgr := invoice.NewManager(hc)
		var err error
		invoiceStatus, err = mgr.PollUntilProcessed(ctx, sess, submitResult.ReferenceNumber, 3*time.Second)
		if err != nil {
			t.Fatalf("poll invoice status: %v", err)
		}
		t.Logf("invoice processing complete: status=%d (%s)",
			invoiceStatus.Status.Code, invoiceStatus.Status.Description)
		if len(invoiceStatus.Status.Details) > 0 {
			t.Logf("  details: %v", invoiceStatus.Status.Details)
		}
		if len(invoiceStatus.Status.Extensions) > 0 {
			t.Logf("  extensions: %v", invoiceStatus.Status.Extensions)
		}
	})
	if t.Failed() {
		return
	}

	// ── Step 6: Verify KSeF number assignment ────────────────────────────────

	t.Run("VerifyKSeFNumber", func(t *testing.T) {
		if !invoiceStatus.Status.Code.IsAccepted() {
			t.Fatalf("invoice not accepted: status=%d (%s)\n  details: %v\n  extensions: %v",
				invoiceStatus.Status.Code,
				invoiceStatus.Status.Description,
				invoiceStatus.Status.Details,
				invoiceStatus.Status.Extensions,
			)
		}
		if invoiceStatus.KSeFNumber == "" {
			t.Fatal("invoice accepted but KSeF number is empty")
		}
		t.Logf("KSeF number: %s", invoiceStatus.KSeFNumber)
		t.Logf("invoice number: %s", invoiceStatus.InvoiceNumber)
		if invoiceStatus.AcquisitionDate != nil {
			t.Logf("acquisition date: %v", invoiceStatus.AcquisitionDate.Format(time.RFC3339))
		}
	})
	if t.Failed() {
		return
	}

	// ── Step 7: Download invoice XML ─────────────────────────────────────────

	t.Run("DownloadInvoice", func(t *testing.T) {
		mgr := invoice.NewManager(hc)
		xmlBytes, err := mgr.DownloadInvoice(ctx, sess, invoiceStatus.KSeFNumber)
		if err != nil {
			t.Fatalf("download invoice: %v", err)
		}
		if len(xmlBytes) == 0 {
			t.Fatal("downloaded invoice XML is empty")
		}
		t.Logf("downloaded invoice XML: %d bytes", len(xmlBytes))
	})

	// ── Step 8: Terminate the session ────────────────────────────────────────

	t.Run("TerminateSession", func(t *testing.T) {
		status, err := sess.Terminate(ctx)
		if err != nil {
			t.Fatalf("terminate session: %v", err)
		}
		terminated = true
		t.Logf("session terminated: status=%d (%s)", status.Status.Code, status.Status.Description)
		if status.InvoiceCount != nil {
			t.Logf("  invoices: total=%d successful=%d failed=%d",
				derefInt32(status.InvoiceCount),
				derefInt32(status.SuccessfulInvoiceCount),
				derefInt32(status.FailedInvoiceCount),
			)
		}
	})
	if t.Failed() {
		return
	}

	// ── Step 9: Download UPO ─────────────────────────────────────────────────

	t.Run("DownloadUPO", func(t *testing.T) {
		mgr := invoice.NewManager(hc)
		// The session access token remains valid after termination. KSeF
		// processes the session asynchronously; PollUntilProcessed already
		// ensured the invoice reached StatusAccepted so the individual UPO
		// should be available.
		upoBytes, err := mgr.DownloadUPO(ctx, sess, submitResult.ReferenceNumber)
		if err != nil {
			t.Fatalf("download UPO: %v", err)
		}
		if len(upoBytes) == 0 {
			t.Fatal("downloaded UPO is empty")
		}
		t.Logf("downloaded UPO: %d bytes", len(upoBytes))
	})
}

// ── Helper functions ──────────────────────────────────────────────────────────

// requireEnv returns the value of the named environment variable.
// It calls t.Skip if the variable is not set, so the whole test is skipped
// rather than failed when a required configuration is absent.
func requireEnv(t *testing.T, key string) string {
	t.Helper()
	v := os.Getenv(key)
	if v == "" {
		t.Skipf("required environment variable %s is not set", key)
	}
	return v
}

// loadKSeFPublicKey loads and parses the KSeF test-environment RSA public key
// used for token encryption (KsefTokenEncryption) from
// testdata/ksef-test-public-key.pem.
//
// Tests in this package run with their working directory set to pkg/ksef/, so
// the testdata directory is two levels up.
func loadKSeFPublicKey(t *testing.T) *rsa.PublicKey {
	t.Helper()
	data, err := os.ReadFile("../../testdata/ksef-test-public-key.pem")
	if err != nil {
		t.Fatalf("load KSeF token-encryption public key from testdata: %v", err)
	}
	key, err := ksefcrypto.LoadPublicKeyFromPEM(data)
	if err != nil {
		t.Fatalf("parse KSeF token-encryption public key: %v", err)
	}
	return key
}

// loadKSeFSymmetricEncryptionKey loads and parses the KSeF test-environment
// RSA public key used for AES symmetric key encryption (SymmetricKeyEncryption)
// from testdata/ksef-test-symmetric-key.pem.
//
// This is a separate key from the token-encryption key; see
// GET /security/public-key-certificates for the distinction.
func loadKSeFSymmetricEncryptionKey(t *testing.T) *rsa.PublicKey {
	t.Helper()
	data, err := os.ReadFile("../../testdata/ksef-test-symmetric-key.pem")
	if err != nil {
		t.Fatalf("load KSeF symmetric-encryption public key from testdata: %v", err)
	}
	key, err := ksefcrypto.LoadPublicKeyFromPEM(data)
	if err != nil {
		t.Fatalf("parse KSeF symmetric-encryption public key: %v", err)
	}
	return key
}

// buildEncryptionInfo generates fresh AES-256 and RSA-OAEP encryption
// parameters suitable for passing to session.Manager.OpenOnline.
func buildEncryptionInfo(t *testing.T) session.EncryptionInfo {
	t.Helper()
	aesKey, err := ksefcrypto.GenerateAESKey()
	if err != nil {
		t.Fatalf("generate AES key: %v", err)
	}
	iv := make([]byte, 16)
	if _, err := rand.Read(iv); err != nil {
		t.Fatalf("generate IV: %v", err)
	}
	pubKey := loadKSeFSymmetricEncryptionKey(t)
	encKey, err := rsa.EncryptOAEP(sha256.New(), rand.Reader, pubKey, aesKey, nil)
	if err != nil {
		t.Fatalf("wrap AES key with RSA-OAEP: %v", err)
	}

	encKeyB64 := base64.StdEncoding.EncodeToString(encKey)
	ivB64 := base64.StdEncoding.EncodeToString(iv)
	t.Logf("debug encryption params:")
	t.Logf("  public key: RSA %d-bit", pubKey.N.BitLen())
	t.Logf("  aesKey length: %d bytes (want 32)", len(aesKey))
	t.Logf("  iv length: %d bytes (want 16)", len(iv))
	t.Logf("  encryptedKey length: %d bytes (want 256 for 2048-bit RSA)", len(encKey))
	t.Logf("  encryptedKey base64 length: %d chars (want 344)", len(encKeyB64))

	bodyDebug := map[string]any{
		"formCode": map[string]string{
			"systemCode":    "FA (3)",
			"schemaVersion": "1-0E",
			"value":         "FA",
		},
		"encryption": map[string]string{
			"encryptedSymmetricKey": encKeyB64,
			"initializationVector":  ivB64,
		},
	}
	bodyJSON, _ := json.Marshal(bodyDebug)
	t.Logf("  POST /sessions/online body: %s", bodyJSON)

	return session.EncryptionInfo{
		SymmetricKey:          aesKey,
		InitializationVector:  iv,
		EncryptedSymmetricKey: encKey,
	}
}

// buildSampleInvoice constructs a minimal FA(3) VAT invoice for the given
// seller NIP and returns the serialised XML. The buyer uses a fixed test NIP;
// adjust as needed for the target test environment.
func buildSampleInvoice(sellerNIP string) ([]byte, error) {
	sellerAddr := fa3.Adres{
		KodKraju: "PL",
		AdresL1:  "ul. Testowa 1",
		AdresL2:  strptr("00-001 Warszawa"),
	}
	buyerAddr := fa3.Adres{
		KodKraju: "PL",
		AdresL1:  "ul. Przykładowa 10",
		AdresL2:  strptr("30-001 Kraków"),
	}

	now := time.Now()
	// Invoice number must be unique per seller per day in the test environment.
	invoiceNum := fmt.Sprintf("TEST/%04d/%02d/%02d/%d",
		now.Year(), int(now.Month()), now.Day(), now.UnixNano()%100000)

	return fa3.NewInvoiceBuilder().
		SetSeller("Firma Testowa Sp. z o.o.", sellerNIP, sellerAddr).
		SetBuyer("Kontrahent Testowy Sp. z o.o.", "9999999999", buyerAddr).
		SetInvoiceNumber(invoiceNum).
		SetDates(now, now).
		SetPayment(
			fa3.PlatnoscPrzelew,
			now.Add(14*24*time.Hour),
			"PL61109010140000071219812874",
		).
		AddItem(fa3.LineItem{
			Description:  "Usługa doradcza",
			Unit:         "usł",
			Quantity:     "1",
			UnitNetPrice: "1000.00",
			NetValue:     "1000.00",
			VATRate:      fa3.Stawka23,
		}).
		BuildXML()
}

func strptr(s string) *string { return &s }

// derefInt32 safely dereferences an *int32, returning 0 for nil.
func derefInt32(p *int32) int32 {
	if p == nil {
		return 0
	}
	return *p
}
