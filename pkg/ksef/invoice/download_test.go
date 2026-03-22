package invoice_test

import (
	"context"
	"net/http"
	"testing"
)

const testKSeFNumber = "5265877635-20250625-010080DD2B5E-26"

func TestDownloadInvoice(t *testing.T) {
	wantXML := []byte(`<?xml version="1.0"?><Faktura><Numer>1/2025</Numer></Faktura>`)

	tests := []struct {
		name           string
		serverStatus   int
		serverBody     []byte
		serverCT       string
		wantErrContain string
	}{
		{
			name:         "returns invoice XML",
			serverStatus: http.StatusOK,
			serverBody:   wantXML,
			serverCT:     "application/xml",
		},
		{
			name:           "invoice not found (400)",
			serverStatus:   http.StatusBadRequest,
			serverBody:     []byte(`{"exception":{"exceptionDetailList":[{"exceptionCode":21164,"exceptionDescription":"Faktura nie istnieje."}]}}`),
			serverCT:       "application/json",
			wantErrContain: "invoice: download",
		},
		{
			name:           "unauthorized (401)",
			serverStatus:   http.StatusUnauthorized,
			serverBody:     []byte(`{"title":"Unauthorized","status":401,"detail":"missing token"}`),
			serverCT:       "application/problem+json",
			wantErrContain: "invoice: download",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var capturedAccept string

			routes := map[string]http.HandlerFunc{
				"GET /invoices/ksef/{ksefNumber}": func(w http.ResponseWriter, r *http.Request) {
					capturedAccept = r.Header.Get("Accept")
					if got := r.PathValue("ksefNumber"); got != testKSeFNumber {
						t.Errorf("ksefNumber in path = %q, want %q", got, testKSeFNumber)
					}
					w.Header().Set("Content-Type", tc.serverCT)
					w.WriteHeader(tc.serverStatus)
					_, _ = w.Write(tc.serverBody)
				},
			}

			mgr, s := testEnv(t, routes)

			data, err := mgr.DownloadInvoice(context.Background(), s, testKSeFNumber)

			if tc.wantErrContain != "" {
				if err == nil {
					t.Fatalf("want error containing %q, got nil", tc.wantErrContain)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if string(data) != string(wantXML) {
				t.Errorf("body = %q, want %q", data, wantXML)
			}
			if capturedAccept != "application/xml" {
				t.Errorf("Accept header = %q, want %q", capturedAccept, "application/xml")
			}
		})
	}
}

func TestDownloadInvoice_AuthorizationHeader(t *testing.T) {
	routes := map[string]http.HandlerFunc{
		"GET /invoices/ksef/{ksefNumber}": func(w http.ResponseWriter, r *http.Request) {
			auth := r.Header.Get("Authorization")
			if auth != "Bearer test-token" {
				t.Errorf("Authorization = %q, want %q", auth, "Bearer test-token")
			}
			w.Header().Set("Content-Type", "application/xml")
			_, _ = w.Write([]byte(`<Faktura/>`))
		},
	}

	mgr, s := testEnv(t, routes)
	_, err := mgr.DownloadInvoice(context.Background(), s, testKSeFNumber)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDownloadInvoice_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	mgr, s := testEnv(t, nil)
	_, err := mgr.DownloadInvoice(ctx, s, testKSeFNumber)
	if err == nil {
		t.Fatal("want error for cancelled context, got nil")
	}
}

func TestDownloadUPO(t *testing.T) {
	wantXML := []byte(`<?xml version="1.0"?><UPO><Numer>1/2025</Numer></UPO>`)

	tests := []struct {
		name           string
		serverStatus   int
		serverBody     []byte
		serverCT       string
		wantErrContain string
	}{
		{
			name:         "returns UPO XML",
			serverStatus: http.StatusOK,
			serverBody:   wantXML,
			serverCT:     "application/xml",
		},
		{
			name:           "bad request (400)",
			serverStatus:   http.StatusBadRequest,
			serverBody:     []byte(`{"exception":{"exceptionDetailList":[{"exceptionCode":21405,"exceptionDescription":"Błąd walidacji."}]}}`),
			serverCT:       "application/json",
			wantErrContain: "invoice: download upo",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var capturedAccept string

			routes := map[string]http.HandlerFunc{
				"GET /sessions/{ref}/invoices/{invRef}/upo": func(w http.ResponseWriter, r *http.Request) {
					capturedAccept = r.Header.Get("Accept")
					if got := r.PathValue("ref"); got != testSessionRef {
						t.Errorf("session ref in path = %q, want %q", got, testSessionRef)
					}
					if got := r.PathValue("invRef"); got != testInvoiceRef {
						t.Errorf("invoice ref in path = %q, want %q", got, testInvoiceRef)
					}
					w.Header().Set("Content-Type", tc.serverCT)
					w.WriteHeader(tc.serverStatus)
					_, _ = w.Write(tc.serverBody)
				},
			}

			mgr, s := testEnv(t, routes)

			data, err := mgr.DownloadUPO(context.Background(), s, testInvoiceRef)

			if tc.wantErrContain != "" {
				if err == nil {
					t.Fatalf("want error containing %q, got nil", tc.wantErrContain)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if string(data) != string(wantXML) {
				t.Errorf("body = %q, want %q", data, wantXML)
			}
			if capturedAccept != "application/xml" {
				t.Errorf("Accept header = %q, want %q", capturedAccept, "application/xml")
			}
		})
	}
}

func TestDownloadUPO_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	mgr, s := testEnv(t, nil)
	_, err := mgr.DownloadUPO(ctx, s, testInvoiceRef)
	if err == nil {
		t.Fatal("want error for cancelled context, got nil")
	}
}
