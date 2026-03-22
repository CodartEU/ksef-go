// Package session provides KSeF session management for online and batch modes.
package session

import "time"

// StatusCode represents a KSeF session status code.
type StatusCode int32

const (
	// StatusOpened indicates an interactive session has been opened.
	StatusOpened StatusCode = 100
	// StatusClosed indicates the interactive session has been closed.
	StatusClosed StatusCode = 170
	// StatusProcessedOK indicates the session was processed successfully
	// and the UPO is available.
	StatusProcessedOK StatusCode = 200
	// StatusDecryptionError indicates the server failed to decrypt the
	// provided symmetric key.
	StatusDecryptionError StatusCode = 415
	// StatusCancelled indicates the session was cancelled — no invoices
	// were submitted before the timeout expired.
	StatusCancelled StatusCode = 440
	// StatusValidationError indicates that no invoice in the session
	// passed validation.
	StatusValidationError StatusCode = 445
)

// EncryptionInfo holds the pre-computed encryption parameters required
// when opening a KSeF session. Use the crypto package to generate these
// values from a random AES-256 key and the KSeF environment's RSA public key.
type EncryptionInfo struct {
	// EncryptedSymmetricKey is the 32-byte AES-256 key encrypted with
	// RSA-OAEP (SHA-256), using the KSeF environment's public key.
	EncryptedSymmetricKey []byte
	// InitializationVector is the 16-byte IV for AES-256-CBC encryption.
	InitializationVector []byte
	// SymmetricKey is the plaintext 32-byte AES-256 key. Set this so that
	// the resulting OnlineSession can encrypt invoices without the caller
	// having to track the key separately.
	SymmetricKey []byte
}

// StatusInfo describes the code and human-readable description of a session state.
type StatusInfo struct {
	// Code is the numeric KSeF status code.
	Code StatusCode
	// Description is the human-readable status message, typically in Polish.
	Description string
	// Details contains optional additional context provided by the API.
	Details []string
}

// UPOPage contains a single-page download URL for the UPO document.
type UPOPage struct {
	// ReferenceNumber is the UPO page reference number.
	ReferenceNumber string
	// DownloadURL is the URL to fetch the UPO page; no auth token is required.
	DownloadURL string
	// ExpiresAt is when the download URL becomes invalid.
	ExpiresAt time.Time
}

// UPO is the Urzędowe Potwierdzenie Odbioru — the official receipt issued by
// KSeF once a session has been successfully processed.
type UPO struct {
	// Pages contains one or more downloadable UPO pages.
	Pages []UPOPage
}

// SessionStatus describes the current or final state of a KSeF session.
type SessionStatus struct {
	// Status holds the numeric status code and its description.
	Status StatusInfo
	// DateCreated is when the session was created.
	DateCreated time.Time
	// DateUpdated is when the session state was last updated.
	DateUpdated time.Time
	// ValidUntil is the session expiry time; nil once the session is closed.
	ValidUntil *time.Time
	// UPO holds the official confirmation download information. It is
	// populated once the session reaches StatusProcessedOK.
	UPO *UPO
	// InvoiceCount is the total number of invoices submitted to the session.
	InvoiceCount *int32
	// SuccessfulInvoiceCount is the number of invoices that passed processing.
	SuccessfulInvoiceCount *int32
	// FailedInvoiceCount is the number of invoices that failed processing.
	FailedInvoiceCount *int32
}
