package invoice_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/CodartEU/ksef-go/pkg/ksef/invoice"
)

// statusBody returns JSON for a SessionInvoiceStatusResponse with the given
// status code and optional ksefNumber.
func statusBody(code int32, description, ksefNumber string) string {
	type statusWire struct {
		Code        int32  `json:"code"`
		Description string `json:"description"`
	}
	type body struct {
		OrdinalNumber   int32      `json:"ordinalNumber"`
		ReferenceNumber string     `json:"referenceNumber"`
		InvoicingDate   time.Time  `json:"invoicingDate"`
		KsefNumber      string     `json:"ksefNumber,omitempty"`
		Status          statusWire `json:"status"`
	}
	b, _ := json.Marshal(body{
		OrdinalNumber:   1,
		ReferenceNumber: testInvoiceRef,
		InvoicingDate:   time.Now(),
		KsefNumber:      ksefNumber,
		Status:          statusWire{Code: code, Description: description},
	})
	return string(b)
}

func TestGetInvoiceStatus(t *testing.T) {
	tests := []struct {
		name           string
		serverStatus   int
		serverBody     string
		wantCode       invoice.ProcessingStatus
		wantKSeFNumber string
		wantErrContain string
	}{
		{
			name:         "received (100)",
			serverStatus: http.StatusOK,
			serverBody:   statusBody(100, "Faktura przyjęta do dalszego przetwarzania", ""),
			wantCode:     invoice.StatusReceived,
		},
		{
			name:         "processing (150)",
			serverStatus: http.StatusOK,
			serverBody:   statusBody(150, "Trwa przetwarzanie", ""),
			wantCode:     invoice.StatusProcessing,
		},
		{
			name:           "accepted (200) with KSeF number",
			serverStatus:   http.StatusOK,
			serverBody:     statusBody(200, "Sukces", "5265877635-20250625-010080DD2B5E-26"),
			wantCode:       invoice.StatusAccepted,
			wantKSeFNumber: "5265877635-20250625-010080DD2B5E-26",
		},
		{
			name:         "duplicate (440)",
			serverStatus: http.StatusOK,
			serverBody:   statusBody(440, "Duplikat faktury", ""),
			wantCode:     invoice.StatusDuplicate,
		},
		{
			name:         "semantic error (450)",
			serverStatus: http.StatusOK,
			serverBody:   statusBody(450, "Błąd weryfikacji semantyki dokumentu faktury", ""),
			wantCode:     invoice.StatusSemanticError,
		},
		{
			name:           "server returns 400",
			serverStatus:   http.StatusBadRequest,
			serverBody:     `{"exceptionCode":21405,"exceptionDescription":"Błąd walidacji."}`,
			wantErrContain: "invoice: status",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			routes := map[string]http.HandlerFunc{
				"GET /sessions/{ref}/invoices/{invRef}": func(w http.ResponseWriter, r *http.Request) {
					// Verify path parameters are forwarded correctly.
					if got := r.PathValue("ref"); got != testSessionRef {
						t.Errorf("session ref in path = %q, want %q", got, testSessionRef)
					}
					if got := r.PathValue("invRef"); got != testInvoiceRef {
						t.Errorf("invoice ref in path = %q, want %q", got, testInvoiceRef)
					}
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(tc.serverStatus)
					_, _ = w.Write([]byte(tc.serverBody))
				},
			}

			mgr, s := testEnv(t, routes)

			status, err := mgr.GetInvoiceStatus(context.Background(), s, testInvoiceRef)

			if tc.wantErrContain != "" {
				if err == nil {
					t.Fatalf("want error containing %q, got nil", tc.wantErrContain)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if status.Status.Code != tc.wantCode {
				t.Errorf("status code = %d, want %d", status.Status.Code, tc.wantCode)
			}
			if status.KSeFNumber != tc.wantKSeFNumber {
				t.Errorf("KSeFNumber = %q, want %q", status.KSeFNumber, tc.wantKSeFNumber)
			}
			if status.ReferenceNumber != testInvoiceRef {
				t.Errorf("ReferenceNumber = %q, want %q", status.ReferenceNumber, testInvoiceRef)
			}
		})
	}
}

func TestGetInvoiceStatus_DuplicateExtensions(t *testing.T) {
	type statusExtWire struct {
		Code        int32             `json:"code"`
		Description string            `json:"description"`
		Extensions  map[string]string `json:"extensions"`
	}
	type body struct {
		OrdinalNumber   int32         `json:"ordinalNumber"`
		ReferenceNumber string        `json:"referenceNumber"`
		InvoicingDate   time.Time     `json:"invoicingDate"`
		Status          statusExtWire `json:"status"`
	}

	want := map[string]string{
		"originalSessionReferenceNumber": "20250626-SO-2F14610000-242991F8C9-B4",
		"originalKsefNumber":             "5265877635-20250626-010080DD2B5E-26",
	}
	b, _ := json.Marshal(body{
		OrdinalNumber:   2,
		ReferenceNumber: testInvoiceRef,
		InvoicingDate:   time.Now(),
		Status: statusExtWire{
			Code:        440,
			Description: "Duplikat faktury",
			Extensions:  want,
		},
	})

	routes := map[string]http.HandlerFunc{
		"GET /sessions/{ref}/invoices/{invRef}": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(b)
		},
	}

	mgr, s := testEnv(t, routes)

	status, err := mgr.GetInvoiceStatus(context.Background(), s, testInvoiceRef)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status.Status.Code != invoice.StatusDuplicate {
		t.Errorf("code = %d, want %d", status.Status.Code, invoice.StatusDuplicate)
	}
	for k, v := range want {
		if status.Status.Extensions[k] != v {
			t.Errorf("Extensions[%q] = %q, want %q", k, status.Status.Extensions[k], v)
		}
	}
}

func TestPollUntilProcessed_EventuallyAccepted(t *testing.T) {
	var callCount atomic.Int32

	routes := map[string]http.HandlerFunc{
		"GET /sessions/{ref}/invoices/{invRef}": func(w http.ResponseWriter, r *http.Request) {
			n := callCount.Add(1)
			w.Header().Set("Content-Type", "application/json")
			if n < 3 {
				// First two calls return non-terminal processing status.
				_, _ = w.Write([]byte(statusBody(150, "Trwa przetwarzanie", "")))
			} else {
				// Third call returns the accepted terminal status.
				_, _ = w.Write([]byte(statusBody(200, "Sukces", "5265877635-20250625-010080DD2B5E-26")))
			}
		},
	}

	mgr, s := testEnv(t, routes)

	status, err := mgr.PollUntilProcessed(context.Background(), s, testInvoiceRef, time.Millisecond)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status.Status.Code != invoice.StatusAccepted {
		t.Errorf("final code = %d, want %d", status.Status.Code, invoice.StatusAccepted)
	}
	if status.KSeFNumber == "" {
		t.Error("KSeFNumber must be set on accepted status")
	}
	if callCount.Load() < 3 {
		t.Errorf("expected at least 3 polls, got %d", callCount.Load())
	}
}

func TestPollUntilProcessed_ImmediateRejection(t *testing.T) {
	routes := map[string]http.HandlerFunc{
		"GET /sessions/{ref}/invoices/{invRef}": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(statusBody(430, "Błąd weryfikacji pliku faktury", "")))
		},
	}

	mgr, s := testEnv(t, routes)

	status, err := mgr.PollUntilProcessed(context.Background(), s, testInvoiceRef, time.Millisecond)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status.Status.Code != invoice.StatusFileVerificationError {
		t.Errorf("final code = %d, want %d", status.Status.Code, invoice.StatusFileVerificationError)
	}
}

func TestPollUntilProcessed_ContextCancelledDuringWait(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	routes := map[string]http.HandlerFunc{
		"GET /sessions/{ref}/invoices/{invRef}": func(w http.ResponseWriter, r *http.Request) {
			// Cancel the context after the first successful status call.
			cancel()
			w.Header().Set("Content-Type", "application/json")
			// Return non-terminal so the poll loop has to wait before retrying.
			_, _ = w.Write([]byte(statusBody(150, "Trwa przetwarzanie", "")))
		},
	}

	mgr, s := testEnv(t, routes)

	_, err := mgr.PollUntilProcessed(ctx, s, testInvoiceRef, time.Hour)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("want context.Canceled, got: %v", err)
	}
}

func TestPollUntilProcessed_ContextAlreadyCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	mgr, s := testEnv(t, nil)

	_, err := mgr.PollUntilProcessed(ctx, s, testInvoiceRef, time.Millisecond)
	if err == nil {
		t.Fatal("want error for pre-cancelled context, got nil")
	}
}

func TestProcessingStatus_IsTerminal(t *testing.T) {
	tests := []struct {
		code     invoice.ProcessingStatus
		terminal bool
	}{
		{invoice.StatusReceived, false},
		{invoice.StatusProcessing, false},
		{invoice.StatusAccepted, true},
		{invoice.StatusSessionError, true},
		{invoice.StatusPermissionError, true},
		{invoice.StatusAttachmentError, true},
		{invoice.StatusFileVerificationError, true},
		{invoice.StatusDecryptionError, true},
		{invoice.StatusDuplicate, true},
		{invoice.StatusSemanticError, true},
		{invoice.StatusUnknownError, true},
		{invoice.StatusCancelledBySystem, true},
	}

	for _, tc := range tests {
		if got := tc.code.IsTerminal(); got != tc.terminal {
			t.Errorf("ProcessingStatus(%d).IsTerminal() = %v, want %v", tc.code, got, tc.terminal)
		}
	}
}
