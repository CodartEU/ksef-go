// Package invoice provides invoice submission and status operations for KSeF
// online sessions.
package invoice

import "time"

// ProcessingStatus represents the numeric status code of an invoice being
// processed by KSeF.
type ProcessingStatus int32

const (
	// StatusReceived indicates the invoice has been received and queued for
	// processing.
	StatusReceived ProcessingStatus = 100
	// StatusProcessing indicates KSeF is actively processing the invoice.
	StatusProcessing ProcessingStatus = 150
	// StatusAccepted indicates the invoice was accepted and a KSeF number has
	// been assigned. This is the only successful terminal status.
	StatusAccepted ProcessingStatus = 200
	// StatusSessionError indicates processing was cancelled because of a
	// session-level error.
	StatusSessionError ProcessingStatus = 405
	// StatusPermissionError indicates the caller lacks the required permissions
	// for this invoice.
	StatusPermissionError ProcessingStatus = 410
	// StatusAttachmentError indicates the invoice cannot carry attachments.
	StatusAttachmentError ProcessingStatus = 415
	// StatusFileVerificationError indicates the invoice file failed integrity
	// verification.
	StatusFileVerificationError ProcessingStatus = 430
	// StatusDecryptionError indicates KSeF failed to decrypt the invoice
	// payload.
	StatusDecryptionError ProcessingStatus = 435
	// StatusDuplicate indicates the invoice is a duplicate of one already
	// accepted by KSeF. The Extensions map may contain the original KSeF number
	// and session reference.
	StatusDuplicate ProcessingStatus = 440
	// StatusSemanticError indicates the invoice failed semantic validation.
	StatusSemanticError ProcessingStatus = 450
	// StatusUnknownError indicates an unspecified server-side error.
	StatusUnknownError ProcessingStatus = 500
	// StatusCancelledBySystem indicates KSeF cancelled the operation
	// internally.
	StatusCancelledBySystem ProcessingStatus = 550
)

// IsTerminal reports whether s represents a final state — one in which no
// further status changes will occur. Callers should stop polling once a
// terminal status is observed.
func (s ProcessingStatus) IsTerminal() bool {
	return s != StatusReceived && s != StatusProcessing
}

// IsAccepted reports whether s represents a successful invoice acceptance.
func (s ProcessingStatus) IsAccepted() bool { return s == StatusAccepted }

// StatusInfo describes the processing status of an invoice.
type StatusInfo struct {
	// Code is the numeric KSeF processing status.
	Code ProcessingStatus
	// Description is the human-readable status message, typically in Polish.
	Description string
	// Details contains optional additional context provided by the API.
	Details []string
	// Extensions holds additional key-value metadata associated with specific
	// statuses, e.g. "originalKsefNumber" for StatusDuplicate.
	Extensions map[string]string
}

// SubmitResult holds the outcome of a successful invoice submission.
type SubmitResult struct {
	// ReferenceNumber is the KSeF-assigned invoice reference number. Pass it to
	// [Manager.GetInvoiceStatus] or [Manager.PollUntilProcessed] to track
	// processing.
	ReferenceNumber string
}

// InvoiceStatus holds the current or final processing state of a submitted
// invoice.
type InvoiceStatus struct {
	// OrdinalNumber is the sequential index of this invoice within its session.
	OrdinalNumber int32
	// ReferenceNumber is the invoice reference number within the session.
	ReferenceNumber string
	// KSeFNumber is the permanent KSeF identifier assigned on acceptance. It is
	// empty until the invoice reaches [StatusAccepted].
	KSeFNumber string
	// InvoiceNumber is the invoice number from the FA(3) document.
	InvoiceNumber string
	// InvoicingDate is when KSeF accepted the invoice for further processing.
	InvoicingDate time.Time
	// AcquisitionDate is when the KSeF number was assigned; populated once the
	// invoice reaches [StatusAccepted].
	AcquisitionDate *time.Time
	// UPODownloadURL is a short-lived URL to download the individual UPO for
	// this invoice. It is populated after acceptance.
	UPODownloadURL string
	// UPODownloadURLExpiresAt is when UPODownloadURL becomes invalid.
	UPODownloadURLExpiresAt *time.Time
	// Status describes the current processing state and any associated error
	// details.
	Status StatusInfo
}
