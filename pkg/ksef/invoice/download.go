package invoice

import (
	"context"
	"fmt"

	"github.com/CodartEU/ksef-go/pkg/ksef/session"
)

// DownloadInvoice fetches the FA(3) XML document for the invoice identified by
// ksefNumber from the KSeF repository. The caller must hold a valid access
// token associated with s.
//
// The returned bytes are the raw XML content of the invoice.
func (m *Manager) DownloadInvoice(ctx context.Context, s *session.OnlineSession, ksefNumber string) ([]byte, error) {
	path := "/invoices/ksef/" + ksefNumber
	headers := map[string]string{
		"Authorization": "Bearer " + s.AccessToken(),
		"Accept":        "application/xml",
	}

	data, err := m.http.Get(ctx, path, headers)
	if err != nil {
		return nil, fmt.Errorf("invoice: download: %w", err)
	}
	return data, nil
}

// DownloadUPO fetches the UPO (Urzędowe Potwierdzenie Odbioru) XML for the
// invoice identified by invoiceRef within the session identified by s.
//
// The UPO is available only after the invoice has reached [StatusAccepted].
// invoiceRef is the ReferenceNumber returned by [Manager.SubmitInvoice].
//
// The session may be already terminated; the access token embedded in s
// remains valid for UPO retrieval after the session is closed.
func (m *Manager) DownloadUPO(ctx context.Context, s *session.OnlineSession, invoiceRef string) ([]byte, error) {
	path := "/sessions/" + s.ReferenceNumber + "/invoices/" + invoiceRef + "/upo"
	headers := map[string]string{
		"Authorization": "Bearer " + s.AccessToken(),
		"Accept":        "application/xml",
	}

	data, err := m.http.Get(ctx, path, headers)
	if err != nil {
		return nil, fmt.Errorf("invoice: download upo: %w", err)
	}
	return data, nil
}
