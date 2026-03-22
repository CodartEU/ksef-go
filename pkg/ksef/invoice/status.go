package invoice

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/CodartEU/ksef-go/pkg/ksef/session"
)

// GetInvoiceStatus fetches the current processing status of an invoice
// previously submitted within the given online session.
//
// invoiceRef is the ReferenceNumber returned by [Manager.SubmitInvoice].
func (m *Manager) GetInvoiceStatus(ctx context.Context, s *session.OnlineSession, invoiceRef string) (*InvoiceStatus, error) {
	path := "/sessions/" + s.ReferenceNumber + "/invoices/" + invoiceRef
	headers := map[string]string{"Authorization": "Bearer " + s.AccessToken()}

	raw, err := m.http.Get(ctx, path, headers)
	if err != nil {
		return nil, fmt.Errorf("invoice: status: %w", err)
	}

	var resp sessionInvoiceStatusResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("invoice: status: decode response: %w", err)
	}

	return resp.toInvoiceStatus(), nil
}

// PollUntilProcessed calls [Manager.GetInvoiceStatus] repeatedly until the
// invoice reaches a terminal status ([StatusAccepted] or any error state) or
// ctx is cancelled.
//
// interval is the initial delay between polls. Each subsequent delay doubles
// (exponential backoff) up to a maximum of 30 seconds.
//
// Returns the final [InvoiceStatus] when a terminal state is reached, or a
// non-nil error if ctx is cancelled or a network error occurs.
func (m *Manager) PollUntilProcessed(ctx context.Context, s *session.OnlineSession, ref string, interval time.Duration) (*InvoiceStatus, error) {
	const maxDelay = 30 * time.Second
	delay := interval

	for {
		status, err := m.GetInvoiceStatus(ctx, s, ref)
		if err != nil {
			return nil, fmt.Errorf("invoice: poll: %w", err)
		}

		if status.Status.Code.IsTerminal() {
			return status, nil
		}

		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("invoice: poll: %w", ctx.Err())
		case <-time.After(delay):
		}

		delay *= 2
		if delay > maxDelay {
			delay = maxDelay
		}
	}
}

// ── wire types ─────────────────────────────────────────────────────────────────

type invoiceStatusInfoWire struct {
	Code        int32             `json:"code"`
	Description string            `json:"description"`
	Details     []string          `json:"details"`
	Extensions  map[string]string `json:"extensions"`
}

type sessionInvoiceStatusResponse struct {
	OrdinalNumber                int32                 `json:"ordinalNumber"`
	InvoiceNumber                string                `json:"invoiceNumber"`
	KsefNumber                   string                `json:"ksefNumber"`
	ReferenceNumber              string                `json:"referenceNumber"`
	InvoicingDate                time.Time             `json:"invoicingDate"`
	AcquisitionDate              *time.Time            `json:"acquisitionDate"`
	UPODownloadURL               string                `json:"upoDownloadUrl"`
	UPODownloadURLExpirationDate *time.Time            `json:"upoDownloadUrlExpirationDate"`
	Status                       invoiceStatusInfoWire `json:"status"`
}

func (r *sessionInvoiceStatusResponse) toInvoiceStatus() *InvoiceStatus {
	return &InvoiceStatus{
		OrdinalNumber:           r.OrdinalNumber,
		ReferenceNumber:         r.ReferenceNumber,
		KSeFNumber:              r.KsefNumber,
		InvoiceNumber:           r.InvoiceNumber,
		InvoicingDate:           r.InvoicingDate,
		AcquisitionDate:         r.AcquisitionDate,
		UPODownloadURL:          r.UPODownloadURL,
		UPODownloadURLExpiresAt: r.UPODownloadURLExpirationDate,
		Status: StatusInfo{
			Code:        ProcessingStatus(r.Status.Code),
			Description: r.Status.Description,
			Details:     r.Status.Details,
			Extensions:  r.Status.Extensions,
		},
	}
}
