package session

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"

	"github.com/CodartEU/ksef-go/internal/httpclient"
)

// Common form code identifiers for use with [Manager.OpenOnline].
const (
	// FormCodeFA3 identifies the FA(3) standard Polish VAT invoice schema.
	FormCodeFA3 = "FA(3)"
)

// formCodeDef is the wire format sent to the API in the openOnlineRequest body.
type formCodeDef struct {
	SystemCode    string `json:"systemCode"`
	SchemaVersion string `json:"schemaVersion"`
	Value         string `json:"value"`
}

// formCodeByName maps public form code constants to their API representations.
var formCodeByName = map[string]formCodeDef{
	FormCodeFA3: {SystemCode: "FA (3)", SchemaVersion: "1-0E", Value: "FA"},
}

// Manager provides methods for opening KSeF online sessions.
type Manager struct {
	http *httpclient.Client
}

// NewManager creates a Manager using the given internal HTTP client.
func NewManager(hc *httpclient.Client) *Manager {
	return &Manager{http: hc}
}

// OnlineSession represents an open KSeF interactive session. All invoice
// submissions within a session must be completed before calling Terminate.
//
// OnlineSession is not safe for concurrent use by multiple goroutines.
type OnlineSession struct {
	// ReferenceNumber is the 36-character session identifier assigned by KSeF.
	ReferenceNumber string
	// ValidUntil is the timestamp after which KSeF will automatically expire
	// the session if it has not been terminated by the caller.
	ValidUntil time.Time
	// AESKey is the plaintext AES-256 key used to encrypt invoices within this
	// session. It is set from EncryptionInfo.SymmetricKey when opening the session.
	AESKey []byte
	// IV is the 16-byte initialization vector sent to KSeF at session open.
	// It must be used (unchanged) when encrypting every invoice in this session.
	IV []byte

	accessToken string
	http        *httpclient.Client
}

// AccessToken returns the JWT access token associated with this session.
func (s *OnlineSession) AccessToken() string { return s.accessToken }

// OpenOnline opens a new interactive session on the KSeF API and returns the
// resulting [OnlineSession].
//
// accessToken must be a valid JWT access token obtained from the auth package.
//
// formCode identifies the invoice schema; use the FormCode* constants declared
// in this package (e.g. [FormCodeFA3] for standard Polish VAT invoices).
//
// enc must contain the pre-computed encryption parameters. Generate them with
// the crypto package using a freshly generated random AES-256 key and the
// KSeF environment's RSA public key.
func (m *Manager) OpenOnline(ctx context.Context, accessToken, formCode string, enc EncryptionInfo) (*OnlineSession, error) {
	fc, ok := formCodeByName[formCode]
	if !ok {
		return nil, fmt.Errorf("session: open online: unknown form code %q", formCode)
	}

	body := openOnlineRequest{
		FormCode: fc,
		Encryption: encryptionPayload{
			EncryptedSymmetricKey: base64.StdEncoding.EncodeToString(enc.EncryptedSymmetricKey),
			InitializationVector:  base64.StdEncoding.EncodeToString(enc.InitializationVector),
		},
	}

	headers := map[string]string{"Authorization": "Bearer " + accessToken}
	raw, err := m.http.Post(ctx, "/sessions/online", body, headers)
	if err != nil {
		return nil, fmt.Errorf("session: open online: %w", err)
	}

	var resp openOnlineResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("session: open online: decode response: %w", err)
	}

	sess := &OnlineSession{
		ReferenceNumber: resp.ReferenceNumber,
		ValidUntil:      resp.ValidUntil,
		AESKey:          enc.SymmetricKey,
		IV:              enc.InitializationVector,
		accessToken:     accessToken,
		http:            m.http,
	}

	if err := sess.WaitUntilActive(ctx); err != nil {
		return nil, fmt.Errorf("session: open online: %w", err)
	}

	return sess, nil
}

// Terminate closes the session and returns its state immediately after the
// close request is accepted. The status will be [StatusClosed] (170) at this
// point; KSeF processes the session asynchronously and transitions to
// [StatusProcessedOK] (200) once done. Poll [OnlineSession.Status] until
// you see StatusProcessedOK to retrieve the UPO.
func (s *OnlineSession) Terminate(ctx context.Context) (*SessionStatus, error) {
	headers := map[string]string{"Authorization": "Bearer " + s.accessToken}
	path := "/sessions/online/" + s.ReferenceNumber + "/close"

	if _, err := s.http.Post(ctx, path, nil, headers); err != nil {
		return nil, fmt.Errorf("session: terminate: %w", err)
	}

	status, err := s.Status(ctx)
	if err != nil {
		return nil, fmt.Errorf("session: terminate: %w", err)
	}
	return status, nil
}

// WaitUntilActive polls the session status endpoint with exponential backoff
// until the session reaches [StatusOpened] (100), indicating it is ready to
// accept invoice submissions. It returns an error if the session enters a
// terminal error state or the 10-second deadline is exceeded.
//
// OpenOnline calls this automatically; callers should not need to call it
// directly unless re-checking a session obtained from an external reference.
func (s *OnlineSession) WaitUntilActive(ctx context.Context) error {
	const (
		maxRetries   = 5
		baseDelay    = 200 * time.Millisecond
		totalTimeout = 10 * time.Second
	)

	ctx, cancel := context.WithTimeout(ctx, totalTimeout)
	defer cancel()

	delay := baseDelay
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return fmt.Errorf("session: wait until active: %w", ctx.Err())
			case <-time.After(delay):
			}
			delay *= 2
		}

		status, err := s.Status(ctx)
		if err != nil {
			return fmt.Errorf("session: wait until active: %w", err)
		}

		switch status.Status.Code {
		case StatusOpened:
			return nil
		case StatusDecryptionError, StatusCancelled, StatusValidationError:
			return fmt.Errorf("session: wait until active: terminal state %d: %s",
				status.Status.Code, status.Status.Description)
		}
	}

	return fmt.Errorf("session: wait until active: session not ready after %d retries", maxRetries)
}

// Status fetches and returns the current state of the session.
func (s *OnlineSession) Status(ctx context.Context) (*SessionStatus, error) {
	headers := map[string]string{"Authorization": "Bearer " + s.accessToken}
	raw, err := s.http.Get(ctx, "/sessions/"+s.ReferenceNumber, headers)
	if err != nil {
		return nil, fmt.Errorf("session: status: %w", err)
	}

	var resp sessionStatusResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("session: status: decode response: %w", err)
	}

	return resp.toSessionStatus(), nil
}

// ── request / response wire types ─────────────────────────────────────────────

type openOnlineRequest struct {
	FormCode   formCodeDef       `json:"formCode"`
	Encryption encryptionPayload `json:"encryption"`
}

type encryptionPayload struct {
	EncryptedSymmetricKey string `json:"encryptedSymmetricKey"`
	InitializationVector  string `json:"initializationVector"`
}

type openOnlineResponse struct {
	ReferenceNumber string    `json:"referenceNumber"`
	ValidUntil      time.Time `json:"validUntil"`
}

type statusInfoWire struct {
	Code        int32    `json:"code"`
	Description string   `json:"description"`
	Details     []string `json:"details"`
}

type upoPageWire struct {
	ReferenceNumber           string    `json:"referenceNumber"`
	DownloadURL               string    `json:"downloadUrl"`
	DownloadURLExpirationDate time.Time `json:"downloadUrlExpirationDate"`
}

type upoWire struct {
	Pages []upoPageWire `json:"pages"`
}

type sessionStatusResponse struct {
	Status                 statusInfoWire `json:"status"`
	DateCreated            time.Time      `json:"dateCreated"`
	DateUpdated            time.Time      `json:"dateUpdated"`
	ValidUntil             *time.Time     `json:"validUntil"`
	UPO                    *upoWire       `json:"upo"`
	InvoiceCount           *int32         `json:"invoiceCount"`
	SuccessfulInvoiceCount *int32         `json:"successfulInvoiceCount"`
	FailedInvoiceCount     *int32         `json:"failedInvoiceCount"`
}

func (r *sessionStatusResponse) toSessionStatus() *SessionStatus {
	s := &SessionStatus{
		Status: StatusInfo{
			Code:        StatusCode(r.Status.Code),
			Description: r.Status.Description,
			Details:     r.Status.Details,
		},
		DateCreated:            r.DateCreated,
		DateUpdated:            r.DateUpdated,
		ValidUntil:             r.ValidUntil,
		InvoiceCount:           r.InvoiceCount,
		SuccessfulInvoiceCount: r.SuccessfulInvoiceCount,
		FailedInvoiceCount:     r.FailedInvoiceCount,
	}
	if r.UPO != nil {
		upo := &UPO{Pages: make([]UPOPage, len(r.UPO.Pages))}
		for i, p := range r.UPO.Pages {
			upo.Pages[i] = UPOPage{
				ReferenceNumber: p.ReferenceNumber,
				DownloadURL:     p.DownloadURL,
				ExpiresAt:       p.DownloadURLExpirationDate,
			}
		}
		s.UPO = upo
	}
	return s
}
