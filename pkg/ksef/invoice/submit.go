package invoice

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"

	"github.com/CodartEU/ksef-go/internal/httpclient"
	ksefcrypto "github.com/CodartEU/ksef-go/pkg/ksef/crypto"
	"github.com/CodartEU/ksef-go/pkg/ksef/session"
)

// Manager handles invoice submission and status operations within a KSeF
// online session.
type Manager struct {
	http *httpclient.Client
}

// NewManager creates a Manager using the given internal HTTP client.
func NewManager(hc *httpclient.Client) *Manager {
	return &Manager{http: hc}
}

// SubmitInvoice encrypts invoiceXML using the session's AES key and submits
// it to KSeF for processing within the given online session.
//
// The session must be open (StatusOpened) and must have been created with
// [session.EncryptionInfo.SymmetricKey] set so that the AES key is available
// for encryption.
//
// The encrypted content uses AES-256-CBC with a freshly generated random IV
// prepended to the ciphertext, which is then base64-encoded for the API.
//
// Returns a [SubmitResult] whose ReferenceNumber can be passed to
// [Manager.GetInvoiceStatus] or [Manager.PollUntilProcessed] to track
// processing.
func (m *Manager) SubmitInvoice(ctx context.Context, s *session.OnlineSession, invoiceXML []byte) (*SubmitResult, error) {
	if len(s.AESKey) == 0 {
		return nil, fmt.Errorf("invoice: submit: session AES key is missing; set EncryptionInfo.SymmetricKey when opening the session")
	}
	if len(s.IV) == 0 {
		return nil, fmt.Errorf("invoice: submit: session IV is missing; set EncryptionInfo.InitializationVector when opening the session")
	}

	// SHA-256 of the original plaintext invoice.
	origDigest := sha256.Sum256(invoiceXML)
	invoiceHashB64 := base64.StdEncoding.EncodeToString(origDigest[:])

	// Encrypt using the session's IV (the same IV sent to KSeF at session open).
	// KSeF uses that IV to decrypt; the output is ciphertext only — no prepended IV.
	encBytes, err := ksefcrypto.EncryptAESCBCWithIV(invoiceXML, s.AESKey, s.IV)
	if err != nil {
		return nil, fmt.Errorf("invoice: submit: encrypt: %w", err)
	}

	// SHA-256 of the ciphertext.
	encDigest := sha256.Sum256(encBytes)
	encHashB64 := base64.StdEncoding.EncodeToString(encDigest[:])

	body := sendInvoiceRequest{
		InvoiceHash:             invoiceHashB64,
		InvoiceSize:             int64(len(invoiceXML)),
		EncryptedInvoiceHash:    encHashB64,
		EncryptedInvoiceSize:    int64(len(encBytes)),
		EncryptedInvoiceContent: base64.StdEncoding.EncodeToString(encBytes),
	}

	path := "/sessions/online/" + s.ReferenceNumber + "/invoices"
	headers := map[string]string{"Authorization": "Bearer " + s.AccessToken()}

	raw, err := m.http.Post(ctx, path, body, headers)
	if err != nil {
		return nil, fmt.Errorf("invoice: submit: %w", err)
	}

	var resp sendInvoiceResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("invoice: submit: decode response: %w", err)
	}

	return &SubmitResult{ReferenceNumber: resp.ReferenceNumber}, nil
}

// ── wire types ─────────────────────────────────────────────────────────────────

type sendInvoiceRequest struct {
	InvoiceHash             string `json:"invoiceHash"`
	InvoiceSize             int64  `json:"invoiceSize"`
	EncryptedInvoiceHash    string `json:"encryptedInvoiceHash"`
	EncryptedInvoiceSize    int64  `json:"encryptedInvoiceSize"`
	EncryptedInvoiceContent string `json:"encryptedInvoiceContent"`
	// OfflineMode declares offline-mode invoicing. Omitted when false (default).
	OfflineMode bool `json:"offlineMode,omitempty"`
}

type sendInvoiceResponse struct {
	ReferenceNumber string `json:"referenceNumber"`
}
