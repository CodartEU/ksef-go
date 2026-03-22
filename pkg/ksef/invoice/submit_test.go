package invoice_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/CodartEU/ksef-go/internal/httpclient"
	ksefcrypto "github.com/CodartEU/ksef-go/pkg/ksef/crypto"
	"github.com/CodartEU/ksef-go/pkg/ksef/invoice"
	"github.com/CodartEU/ksef-go/pkg/ksef/session"
)

const (
	testSessionRef = "20250625-SS-319D7EE000-B67F415CDC-AA"
	testInvoiceRef = "20250625-EE-319D7EE000-B67F415CDC-2C"
)

// testEnv sets up a mock HTTP server, an httpclient.Client pointing to it,
// and an open OnlineSession. The caller supplies optional per-test handlers;
// nil means the endpoint is not expected to be called.
//
// The server handles POST /sessions/online for session setup plus any
// additional routes registered via extraRoutes (pattern → handler).
func testEnv(t *testing.T, extraRoutes map[string]http.HandlerFunc) (*invoice.Manager, *session.OnlineSession) {
	t.Helper()

	aesKey, err := ksefcrypto.GenerateAESKey()
	if err != nil {
		t.Fatalf("generate AES key: %v", err)
	}

	mux := http.NewServeMux()

	// Session open endpoint — always available so the session can be created.
	mux.HandleFunc("POST /sessions/online", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		type openResp struct {
			ReferenceNumber string    `json:"referenceNumber"`
			ValidUntil      time.Time `json:"validUntil"`
		}
		_ = json.NewEncoder(w).Encode(openResp{
			ReferenceNumber: testSessionRef,
			ValidUntil:      time.Now().Add(time.Hour),
		})
	})

	// Session status endpoint — WaitUntilActive calls this after session open.
	mux.HandleFunc("GET /sessions/"+testSessionRef, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		type statusInfo struct {
			Code        int32  `json:"code"`
			Description string `json:"description"`
		}
		type statusResp struct {
			Status statusInfo `json:"status"`
		}
		_ = json.NewEncoder(w).Encode(statusResp{
			Status: statusInfo{Code: 100, Description: "Sesja otwarta"},
		})
	})

	for pattern, h := range extraRoutes {
		mux.HandleFunc(pattern, h)
	}

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	hc := httpclient.New(srv.URL, srv.Client(), nil, httpclient.RetryConfig{MaxRetries: 0})

	enc := session.EncryptionInfo{
		EncryptedSymmetricKey: make([]byte, 32), // placeholder for test
		InitializationVector:  make([]byte, 16), // placeholder for test
		SymmetricKey:          aesKey,
	}
	s, err := session.NewManager(hc).OpenOnline(context.Background(), "test-token", session.FormCodeFA3, enc)
	if err != nil {
		t.Fatalf("open test session: %v", err)
	}

	mgr := invoice.NewManager(hc)
	return mgr, s
}

func TestSubmitInvoice(t *testing.T) {
	invoiceXML := []byte(`<Faktura><Numer>1/2025</Numer></Faktura>`)

	tests := []struct {
		name           string
		serverStatus   int
		serverBody     string
		wantRef        string
		wantErrContain string
	}{
		{
			name:         "accepted 202",
			serverStatus: http.StatusAccepted,
			serverBody:   `{"referenceNumber":"` + testInvoiceRef + `"}`,
			wantRef:      testInvoiceRef,
		},
		{
			name:           "server returns 400",
			serverStatus:   http.StatusBadRequest,
			serverBody:     `{"exceptionCode":21180,"exceptionDescription":"Status sesji nie pozwala na wykonanie operacji."}`,
			wantErrContain: "invoice: submit",
		},
		{
			name:           "server returns 429",
			serverStatus:   http.StatusTooManyRequests,
			serverBody:     `{"message":"too many requests"}`,
			wantErrContain: "invoice: submit",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var capturedReq sendInvoiceRequestCapture

			routes := map[string]http.HandlerFunc{
				"POST /sessions/online/{ref}/invoices": func(w http.ResponseWriter, r *http.Request) {
					if err := json.NewDecoder(r.Body).Decode(&capturedReq); err != nil {
						t.Errorf("decode request body: %v", err)
					}
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(tc.serverStatus)
					_, _ = w.Write([]byte(tc.serverBody))
				},
			}

			mgr, s := testEnv(t, routes)

			result, err := mgr.SubmitInvoice(context.Background(), s, invoiceXML)

			if tc.wantErrContain != "" {
				if err == nil {
					t.Fatalf("want error containing %q, got nil", tc.wantErrContain)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result.ReferenceNumber != tc.wantRef {
				t.Errorf("ReferenceNumber = %q, want %q", result.ReferenceNumber, tc.wantRef)
			}

			// Verify request body integrity.
			wantInvoiceHash := base64.StdEncoding.EncodeToString(sha256Sum(invoiceXML))
			if capturedReq.InvoiceHash != wantInvoiceHash {
				t.Errorf("invoiceHash = %q, want %q", capturedReq.InvoiceHash, wantInvoiceHash)
			}
			if capturedReq.InvoiceSize != int64(len(invoiceXML)) {
				t.Errorf("invoiceSize = %d, want %d", capturedReq.InvoiceSize, len(invoiceXML))
			}
			if capturedReq.EncryptedInvoiceContent == "" {
				t.Error("encryptedInvoiceContent must not be empty")
			}
			// Encrypted size must be larger than plaintext (PKCS7 padding adds at least 1 byte).
			if capturedReq.EncryptedInvoiceSize <= int64(len(invoiceXML)) {
				t.Errorf("encryptedInvoiceSize %d should be > invoiceSize %d",
					capturedReq.EncryptedInvoiceSize, len(invoiceXML))
			}
			// Encrypted hash must match actual encoded content.
			encBytes, err := base64.StdEncoding.DecodeString(capturedReq.EncryptedInvoiceContent)
			if err != nil {
				t.Fatalf("decode encryptedInvoiceContent: %v", err)
			}
			wantEncHash := base64.StdEncoding.EncodeToString(sha256Sum(encBytes))
			if capturedReq.EncryptedInvoiceHash != wantEncHash {
				t.Errorf("encryptedInvoiceHash mismatch: got %q, want %q",
					capturedReq.EncryptedInvoiceHash, wantEncHash)
			}
		})
	}
}

func TestSubmitInvoice_MissingAESKey(t *testing.T) {
	// Create a session without a SymmetricKey to trigger the guard.
	routes := map[string]http.HandlerFunc{
		"POST /sessions/online/{ref}/invoices": func(w http.ResponseWriter, r *http.Request) {
			t.Error("endpoint must not be called when AES key is missing")
		},
	}

	// We need a session with no AES key. Build one via testEnv but then zero
	// the key using a separate session with an empty SymmetricKey.
	mux := http.NewServeMux()
	mux.HandleFunc("POST /sessions/online", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		type openResp struct {
			ReferenceNumber string    `json:"referenceNumber"`
			ValidUntil      time.Time `json:"validUntil"`
		}
		_ = json.NewEncoder(w).Encode(openResp{
			ReferenceNumber: testSessionRef,
			ValidUntil:      time.Now().Add(time.Hour),
		})
	})
	mux.HandleFunc("GET /sessions/"+testSessionRef, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		type statusInfo struct {
			Code        int32  `json:"code"`
			Description string `json:"description"`
		}
		type statusResp struct {
			Status statusInfo `json:"status"`
		}
		_ = json.NewEncoder(w).Encode(statusResp{
			Status: statusInfo{Code: 100, Description: "Sesja otwarta"},
		})
	})
	for pattern, h := range routes {
		mux.HandleFunc(pattern, h)
	}

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	hc := httpclient.New(srv.URL, srv.Client(), nil, httpclient.RetryConfig{MaxRetries: 0})

	// SymmetricKey intentionally not set.
	enc := session.EncryptionInfo{
		EncryptedSymmetricKey: make([]byte, 32),
		InitializationVector:  make([]byte, 16),
	}
	s, err := session.NewManager(hc).OpenOnline(context.Background(), "test-token", session.FormCodeFA3, enc)
	if err != nil {
		t.Fatalf("open test session: %v", err)
	}

	mgr := invoice.NewManager(hc)
	_, submitErr := mgr.SubmitInvoice(context.Background(), s, []byte(`<Faktura/>`))
	if submitErr == nil {
		t.Fatal("want error when AES key is missing, got nil")
	}
}

func TestSubmitInvoice_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancel

	mgr, s := testEnv(t, nil)
	_, err := mgr.SubmitInvoice(ctx, s, []byte(`<Faktura/>`))
	if err == nil {
		t.Fatal("want error for cancelled context, got nil")
	}
}

// ── helpers ────────────────────────────────────────────────────────────────────

type sendInvoiceRequestCapture struct {
	InvoiceHash             string `json:"invoiceHash"`
	InvoiceSize             int64  `json:"invoiceSize"`
	EncryptedInvoiceHash    string `json:"encryptedInvoiceHash"`
	EncryptedInvoiceSize    int64  `json:"encryptedInvoiceSize"`
	EncryptedInvoiceContent string `json:"encryptedInvoiceContent"`
}

func sha256Sum(b []byte) []byte {
	s := sha256.Sum256(b)
	return s[:]
}
