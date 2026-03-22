package session

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/CodartEU/ksef-go/internal/httpclient"
)

// ── shared fixtures ────────────────────────────────────────────────────────────

const (
	testRefNum      = "20250514-AU-AABBCC0000-DDEEFF1122-A1"
	testAccessTok   = "access.jwt.test"
	testUPORefNum   = "20250514-AU-AABBCC0000-DDEEFF1122-B2"
	testDownloadURL = "https://ksef.example/upo/download/abc123"
)

var fixedTime = time.Date(2025, 7, 11, 12, 0, 0, 0, time.UTC)

// sampleEncryption provides a minimal EncryptionInfo with non-empty byte slices.
var sampleEncryption = EncryptionInfo{
	EncryptedSymmetricKey: make([]byte, 32),
	InitializationVector:  []byte("test-iv-16bytes!"), // 16 bytes
	SymmetricKey:          make([]byte, 32),
}

// ── helpers ───────────────────────────────────────────────────────────────────

// writeJSON writes body as JSON with the given status code.
func writeJSON(w http.ResponseWriter, status int, body []byte) {
	t := w.Header()
	t.Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

// makeOpenOnlineResp builds the JSON body for POST /sessions/online 201.
func makeOpenOnlineResp(refNum string, validUntil time.Time) []byte {
	b, _ := json.Marshal(map[string]any{
		"referenceNumber": refNum,
		"validUntil":      validUntil.Format(time.RFC3339),
	})
	return b
}

// makeStatusResp builds the JSON body for GET /sessions/{ref}.
func makeStatusResp(code int32, desc string, upo *upoWire) []byte {
	payload := map[string]any{
		"status": map[string]any{
			"code":        code,
			"description": desc,
		},
		"dateCreated": fixedTime.Format(time.RFC3339),
		"dateUpdated": fixedTime.Format(time.RFC3339),
	}
	if upo != nil {
		pages := make([]map[string]any, len(upo.Pages))
		for i, p := range upo.Pages {
			pages[i] = map[string]any{
				"referenceNumber":           p.ReferenceNumber,
				"downloadUrl":               p.DownloadURL,
				"downloadUrlExpirationDate": p.DownloadURLExpirationDate.Format(time.RFC3339),
			}
		}
		payload["upo"] = map[string]any{"pages": pages}
	}
	b, _ := json.Marshal(payload)
	return b
}

// newManager spins up an httptest.Server backed by handler and returns a Manager
// pointed at it. The server is closed when the test ends.
func newManager(t *testing.T, handler http.Handler) *Manager {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	hc := httpclient.New(srv.URL, srv.Client(), nil, httpclient.RetryConfig{MaxRetries: 0})
	return NewManager(hc)
}

// ── OpenOnline tests ──────────────────────────────────────────────────────────

func TestOpenOnline_HappyPath(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /sessions/online", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+testAccessTok {
			http.Error(w, "bad auth", http.StatusUnauthorized)
			return
		}

		var body openOnlineRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if body.FormCode.SystemCode != "FA (3)" {
			http.Error(w, "unexpected system code", http.StatusBadRequest)
			return
		}
		if body.Encryption.EncryptedSymmetricKey == "" || body.Encryption.InitializationVector == "" {
			http.Error(w, "missing encryption fields", http.StatusBadRequest)
			return
		}

		writeJSON(w, http.StatusCreated, makeOpenOnlineResp(testRefNum, fixedTime))
	})
	mux.HandleFunc("GET /sessions/{referenceNumber}", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, makeStatusResp(int32(StatusOpened), "Sesja otwarta", nil))
	})

	m := newManager(t, mux)
	sess, err := m.OpenOnline(context.Background(), testAccessTok, FormCodeFA3, sampleEncryption)
	if err != nil {
		t.Fatalf("OpenOnline: unexpected error: %v", err)
	}
	if sess.ReferenceNumber != testRefNum {
		t.Errorf("ReferenceNumber = %q, want %q", sess.ReferenceNumber, testRefNum)
	}
	if sess.ValidUntil.IsZero() {
		t.Error("ValidUntil is zero")
	}
	if len(sess.IV) != 16 {
		t.Errorf("IV length = %d, want 16", len(sess.IV))
	}
	if string(sess.IV) != string(sampleEncryption.InitializationVector) {
		t.Error("IV not propagated from EncryptionInfo to OnlineSession")
	}
}

func TestOpenOnline_UnknownFormCode(t *testing.T) {
	// No server needed — error is returned before any HTTP call.
	hc := httpclient.New("http://unused", nil, nil, httpclient.RetryConfig{})
	m := NewManager(hc)

	_, err := m.OpenOnline(context.Background(), testAccessTok, "UNKNOWN(1)", sampleEncryption)
	if err == nil {
		t.Fatal("expected error for unknown form code, got nil")
	}
	if !strings.Contains(err.Error(), "unknown form code") {
		t.Errorf("error = %q, want it to mention 'unknown form code'", err.Error())
	}
}

func TestOpenOnline_APIError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /sessions/online", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusUnauthorized, []byte(`{}`))
	})

	m := newManager(t, mux)
	_, err := m.OpenOnline(context.Background(), "bad-token", FormCodeFA3, sampleEncryption)
	if err == nil {
		t.Fatal("expected error for unauthorized request, got nil")
	}
}

func TestOpenOnline_ContextCancelled(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /sessions/online", func(w http.ResponseWriter, r *http.Request) {
		// Simulate a slow server — context should cancel before reply.
		<-r.Context().Done()
		http.Error(w, "cancelled", http.StatusServiceUnavailable)
	})

	m := newManager(t, mux)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := m.OpenOnline(ctx, testAccessTok, FormCodeFA3, sampleEncryption)
	if err == nil {
		t.Fatal("expected error from cancelled context, got nil")
	}
}

// ── Status tests ──────────────────────────────────────────────────────────────

func TestStatus_HappyPath(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /sessions/{referenceNumber}", func(w http.ResponseWriter, r *http.Request) {
		if r.PathValue("referenceNumber") != testRefNum {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+testAccessTok {
			http.Error(w, "bad auth", http.StatusUnauthorized)
			return
		}
		writeJSON(w, http.StatusOK, makeStatusResp(100, "Sesja otwarta", nil))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	hc := httpclient.New(srv.URL, srv.Client(), nil, httpclient.RetryConfig{MaxRetries: 0})

	s := &OnlineSession{ReferenceNumber: testRefNum, accessToken: testAccessTok, http: hc}
	status, err := s.Status(context.Background())
	if err != nil {
		t.Fatalf("Status: unexpected error: %v", err)
	}
	if status.Status.Code != StatusOpened {
		t.Errorf("Status.Code = %d, want %d", status.Status.Code, StatusOpened)
	}
	if status.DateCreated.IsZero() {
		t.Error("DateCreated is zero")
	}
}

func TestStatus_WithUPO(t *testing.T) {
	upo := &upoWire{
		Pages: []upoPageWire{{
			ReferenceNumber:           testUPORefNum,
			DownloadURL:               testDownloadURL,
			DownloadURLExpirationDate: fixedTime.Add(24 * time.Hour),
		}},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /sessions/{referenceNumber}", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, makeStatusResp(200, "Sesja przetworzona", upo))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	hc := httpclient.New(srv.URL, srv.Client(), nil, httpclient.RetryConfig{MaxRetries: 0})

	s := &OnlineSession{ReferenceNumber: testRefNum, accessToken: testAccessTok, http: hc}
	status, err := s.Status(context.Background())
	if err != nil {
		t.Fatalf("Status: unexpected error: %v", err)
	}
	if status.Status.Code != StatusProcessedOK {
		t.Errorf("Status.Code = %d, want %d", status.Status.Code, StatusProcessedOK)
	}
	if status.UPO == nil {
		t.Fatal("UPO is nil, want non-nil")
	}
	if len(status.UPO.Pages) != 1 {
		t.Fatalf("len(UPO.Pages) = %d, want 1", len(status.UPO.Pages))
	}
	if status.UPO.Pages[0].DownloadURL != testDownloadURL {
		t.Errorf("DownloadURL = %q, want %q", status.UPO.Pages[0].DownloadURL, testDownloadURL)
	}
	if status.UPO.Pages[0].ReferenceNumber != testUPORefNum {
		t.Errorf("UPO ReferenceNumber = %q, want %q", status.UPO.Pages[0].ReferenceNumber, testUPORefNum)
	}
}

func TestStatus_APIError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /sessions/{referenceNumber}", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusUnauthorized, []byte(`{}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	hc := httpclient.New(srv.URL, srv.Client(), nil, httpclient.RetryConfig{MaxRetries: 0})

	s := &OnlineSession{ReferenceNumber: testRefNum, accessToken: testAccessTok, http: hc}
	_, err := s.Status(context.Background())
	if err == nil {
		t.Fatal("expected error for unauthorized request, got nil")
	}
}

// ── Terminate tests ───────────────────────────────────────────────────────────

func TestTerminate_HappyPath(t *testing.T) {
	var closeCalled bool

	mux := http.NewServeMux()
	mux.HandleFunc("POST /sessions/online/{referenceNumber}/close", func(w http.ResponseWriter, r *http.Request) {
		if r.PathValue("referenceNumber") != testRefNum {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+testAccessTok {
			http.Error(w, "bad auth", http.StatusUnauthorized)
			return
		}
		closeCalled = true
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /sessions/{referenceNumber}", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, makeStatusResp(170, "Sesja zamknięta", nil))
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	hc := httpclient.New(srv.URL, srv.Client(), nil, httpclient.RetryConfig{MaxRetries: 0})

	s := &OnlineSession{ReferenceNumber: testRefNum, accessToken: testAccessTok, http: hc}
	summary, err := s.Terminate(context.Background())
	if err != nil {
		t.Fatalf("Terminate: unexpected error: %v", err)
	}
	if !closeCalled {
		t.Error("close endpoint was not called")
	}
	if summary.Status.Code != StatusClosed {
		t.Errorf("Status.Code = %d, want %d", summary.Status.Code, StatusClosed)
	}
}

func TestTerminate_CloseAPIError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /sessions/online/{referenceNumber}/close", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusUnauthorized, []byte(`{}`))
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	hc := httpclient.New(srv.URL, srv.Client(), nil, httpclient.RetryConfig{MaxRetries: 0})

	s := &OnlineSession{ReferenceNumber: testRefNum, accessToken: testAccessTok, http: hc}
	_, err := s.Terminate(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "terminate") {
		t.Errorf("error = %q, want it to mention 'terminate'", err.Error())
	}
}

func TestTerminate_StatusErrorAfterClose(t *testing.T) {
	// Close succeeds (204) but the subsequent GET /sessions fails.
	mux := http.NewServeMux()
	mux.HandleFunc("POST /sessions/online/{referenceNumber}/close", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /sessions/{referenceNumber}", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusInternalServerError, []byte(`{}`))
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	hc := httpclient.New(srv.URL, srv.Client(), nil, httpclient.RetryConfig{MaxRetries: 0})

	s := &OnlineSession{ReferenceNumber: testRefNum, accessToken: testAccessTok, http: hc}
	_, err := s.Terminate(context.Background())
	if err == nil {
		t.Fatal("expected error from status call, got nil")
	}
}

// ── table-driven: form code mapping ───────────────────────────────────────────

func TestFormCodeMapping(t *testing.T) {
	tests := []struct {
		name           string
		formCode       string
		wantSystemCode string
		wantValue      string
		wantErr        bool
	}{
		{
			name:           "FA(3)",
			formCode:       FormCodeFA3,
			wantSystemCode: "FA (3)",
			wantValue:      "FA",
		},
		{
			name:     "unknown",
			formCode: "XX(9)",
			wantErr:  true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var captured openOnlineRequest

			mux := http.NewServeMux()
			mux.HandleFunc("POST /sessions/online", func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewDecoder(r.Body).Decode(&captured)
				writeJSON(w, http.StatusCreated, makeOpenOnlineResp(testRefNum, fixedTime))
			})
			mux.HandleFunc("GET /sessions/{referenceNumber}", func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, http.StatusOK, makeStatusResp(int32(StatusOpened), "Sesja otwarta", nil))
			})

			m := newManager(t, mux)
			_, err := m.OpenOnline(context.Background(), testAccessTok, tc.formCode, sampleEncryption)
			if (err != nil) != tc.wantErr {
				t.Fatalf("OpenOnline error = %v, wantErr = %v", err, tc.wantErr)
			}
			if tc.wantErr {
				return
			}
			if captured.FormCode.SystemCode != tc.wantSystemCode {
				t.Errorf("SystemCode = %q, want %q", captured.FormCode.SystemCode, tc.wantSystemCode)
			}
			if captured.FormCode.Value != tc.wantValue {
				t.Errorf("Value = %q, want %q", captured.FormCode.Value, tc.wantValue)
			}
		})
	}
}
