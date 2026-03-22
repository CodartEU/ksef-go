package invoice

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"time"

	"github.com/CodartEU/ksef-go/pkg/ksef/session"
)

// SubjectType identifies the caller's role in the invoices being queried.
type SubjectType string

const (
	// SubjectSeller queries invoices where the caller is the seller (Podmiot 1).
	SubjectSeller SubjectType = "Subject1"
	// SubjectBuyer queries invoices where the caller is the buyer (Podmiot 2).
	SubjectBuyer SubjectType = "Subject2"
	// SubjectThird queries invoices where the caller is a third subject (Podmiot 3).
	SubjectThird SubjectType = "Subject3"
	// SubjectAuthorized queries invoices accessible via an authorisation grant.
	SubjectAuthorized SubjectType = "SubjectAuthorized"
)

// DateType selects which invoice date field a [QueryDateRange] filters on.
type DateType string

const (
	// DateTypeIssue filters by the date the invoice was issued.
	DateTypeIssue DateType = "Issue"
	// DateTypeInvoicing filters by the date KSeF accepted the invoice for processing.
	DateTypeInvoicing DateType = "Invoicing"
	// DateTypePermanentStorage filters by the date the invoice was permanently
	// stored in the KSeF repository. Recommended for incremental synchronisation.
	DateTypePermanentStorage DateType = "PermanentStorage"
)

// SortOrder controls the ordering of query results.
type SortOrder string

const (
	// SortAsc returns results in ascending order (default when unset).
	SortAsc SortOrder = "Asc"
	// SortDesc returns results in descending order.
	SortDesc SortOrder = "Desc"
)

// InvoicingMode describes how an invoice was submitted to KSeF.
type InvoicingMode string

const (
	// InvoicingModeOnline identifies invoices submitted via an online session.
	InvoicingModeOnline InvoicingMode = "Online"
	// InvoicingModeOffline identifies invoices submitted in offline mode.
	InvoicingModeOffline InvoicingMode = "Offline"
)

// InvoiceType identifies the form variant of an FA(3) invoice.
type InvoiceType string

const (
	InvoiceTypeVat      InvoiceType = "Vat"
	InvoiceTypeZal      InvoiceType = "Zal"
	InvoiceTypeKor      InvoiceType = "Kor"
	InvoiceTypeRoz      InvoiceType = "Roz"
	InvoiceTypeUpr      InvoiceType = "Upr"
	InvoiceTypeKorZal   InvoiceType = "KorZal"
	InvoiceTypeKorRoz   InvoiceType = "KorRoz"
	InvoiceTypeVatPef   InvoiceType = "VatPef"
	InvoiceTypeVatPefSp InvoiceType = "VatPefSp"
	InvoiceTypeKorPef   InvoiceType = "KorPef"
	InvoiceTypeVatRr    InvoiceType = "VatRr"
	InvoiceTypeKorVatRr InvoiceType = "KorVatRr"
)

// FormType identifies the top-level document family.
type FormType string

const (
	FormTypeFA  FormType = "FA"
	FormTypePEF FormType = "PEF"
	FormTypeRR  FormType = "RR"
)

// AmountType selects which amount field a [QueryAmountFilter] is applied to.
type AmountType string

const (
	AmountTypeBrutto AmountType = "Brutto"
	AmountTypeNetto  AmountType = "Netto"
	AmountTypeVat    AmountType = "Vat"
)

// BuyerIdentifierType classifies the kind of buyer identifier.
type BuyerIdentifierType string

const (
	BuyerIdentifierNIP   BuyerIdentifierType = "Nip"
	BuyerIdentifierVatUE BuyerIdentifierType = "VatUe"
	BuyerIdentifierOther BuyerIdentifierType = "Other"
	BuyerIdentifierNone  BuyerIdentifierType = "None"
)

// QueryDateRange specifies the time window for an invoice query.
// The maximum allowed window is 3 months.
type QueryDateRange struct {
	// DateType selects which date field on the invoice is filtered. Required.
	DateType DateType
	// From is the start of the range (inclusive). Required.
	From time.Time
	// To is the end of the range (inclusive). If zero the API defaults to now.
	To time.Time
	// RestrictToPermanentStorageHwmDate caps the upper bound at
	// PermanentStorageHwmDate from a prior query. Only applicable when
	// DateType is [DateTypePermanentStorage].
	RestrictToPermanentStorageHwmDate *bool
}

// QueryBuyerIdentifier filters results by buyer identity.
type QueryBuyerIdentifier struct {
	// Type classifies the identifier.
	Type BuyerIdentifierType
	// Value is the identifier string (exact match). Leave empty when Type is
	// [BuyerIdentifierNone].
	Value string
}

// QueryAmountFilter restricts results by an amount range.
type QueryAmountFilter struct {
	// Type selects which amount field (gross, net, or VAT) to filter on.
	Type AmountType
	// From is the minimum value (inclusive). Nil means no lower bound.
	From *float64
	// To is the maximum value (inclusive). Nil means no upper bound.
	To *float64
}

// InvoiceQueryFilters holds all criteria for [Manager.QueryInvoices].
// SubjectType and DateRange are required; all other fields are optional.
type InvoiceQueryFilters struct {
	// SubjectType identifies the caller's role in the queried invoices. Required.
	SubjectType SubjectType
	// DateRange specifies the time window to search. Required.
	DateRange QueryDateRange

	// KSeFNumber restricts results to a specific KSeF number (exact match).
	KSeFNumber string
	// InvoiceNumber restricts results to a specific issuer-assigned number (exact match).
	InvoiceNumber string
	// SellerNIP restricts results to invoices issued by this NIP.
	SellerNIP string
	// BuyerIdentifier restricts results by buyer identity.
	BuyerIdentifier *QueryBuyerIdentifier
	// CurrencyCodes restricts results to invoices in any of these ISO 4217 codes.
	CurrencyCodes []string
	// InvoiceTypes restricts results to the listed invoice variants.
	InvoiceTypes []InvoiceType
	// FormType restricts results to FA, PEF, or RR invoices.
	FormType FormType
	// IsSelfInvoicing filters by self-invoicing flag. Nil means no filter.
	IsSelfInvoicing *bool
	// HasAttachment filters by attachment presence. Nil means no filter.
	HasAttachment *bool
	// Amount filters by amount range.
	Amount *QueryAmountFilter

	// SortOrder controls the sort direction of results. Defaults to [SortAsc].
	SortOrder SortOrder
	// PageOffset is the zero-based index of the first result page. Use
	// [QueryResult.NextPageOffset] for subsequent pages.
	PageOffset int32
	// PageSize is the number of results per page (1–250; server default 10
	// when zero).
	PageSize int32
}

// ── result types ───────────────────────────────────────────────────────────────

// MetadataSeller holds the seller fields returned in invoice metadata.
type MetadataSeller struct {
	// NIP is the seller's Polish tax identification number.
	NIP string
	// Name is the seller's name, if provided by the API.
	Name string
}

// MetadataBuyerIdentifier holds the buyer's identity as returned by the API.
type MetadataBuyerIdentifier struct {
	// Type classifies the identifier.
	Type BuyerIdentifierType
	// Value is the identifier string.
	Value string
}

// MetadataBuyer holds the buyer fields returned in invoice metadata.
type MetadataBuyer struct {
	// Identifier identifies the buyer.
	Identifier MetadataBuyerIdentifier
	// Name is the buyer's name, if provided by the API.
	Name string
}

// MetadataFormCode identifies the invoice schema used for a specific invoice.
type MetadataFormCode struct {
	// SystemCode is the human-readable schema code, e.g. "FA (3)".
	SystemCode string
	// SchemaVersion is the schema version string, e.g. "1-0E".
	SchemaVersion string
	// Value is the schema value, e.g. "FA".
	Value string
}

// MetadataThirdSubject describes a third-party subject listed on an invoice.
type MetadataThirdSubject struct {
	// IdentifierType classifies the identifier (e.g. "Nip", "VatUe").
	IdentifierType string
	// IdentifierValue is the identifier string.
	IdentifierValue string
	// Name is the subject's name, if provided.
	Name string
	// Role is the numeric role code assigned to this third subject.
	Role int32
}

// MetadataAuthorizedSubject describes the authorised subject on an invoice.
type MetadataAuthorizedSubject struct {
	// NIP is the authorised subject's Polish tax identification number.
	NIP string
	// Name is the subject's name, if provided.
	Name string
	// Role is the numeric role code.
	Role int32
}

// InvoiceMetadata holds the metadata for a single invoice as returned by
// [Manager.QueryInvoices].
type InvoiceMetadata struct {
	// KSeFNumber is the KSeF-assigned unique identifier.
	KSeFNumber string
	// InvoiceNumber is the issuer-assigned invoice number.
	InvoiceNumber string
	// IssueDate is the date the invoice was issued (date only, no time component).
	IssueDate time.Time
	// InvoicingDate is when KSeF accepted the invoice for processing.
	InvoicingDate time.Time
	// AcquisitionDate is when the KSeF number was assigned.
	AcquisitionDate time.Time
	// PermanentStorageDate is when the invoice was permanently stored in KSeF.
	PermanentStorageDate time.Time
	// Seller identifies the invoice seller.
	Seller MetadataSeller
	// Buyer identifies the invoice buyer.
	Buyer MetadataBuyer
	// NetAmount is the total net value.
	NetAmount float64
	// GrossAmount is the total gross value.
	GrossAmount float64
	// VATAmount is the total VAT value.
	VATAmount float64
	// Currency is the ISO 4217 currency code.
	Currency string
	// InvoicingMode indicates whether the invoice was submitted online or offline.
	InvoicingMode InvoicingMode
	// InvoiceType is the invoice form variant.
	InvoiceType InvoiceType
	// FormCode identifies the schema used.
	FormCode MetadataFormCode
	// IsSelfInvoicing indicates the invoice was issued in self-invoicing mode.
	IsSelfInvoicing bool
	// HasAttachment indicates whether the invoice carries an attachment.
	HasAttachment bool
	// InvoiceHashBase64 is the SHA-256 hash of the invoice, base64-encoded.
	InvoiceHashBase64 string
	// HashOfCorrectedInvoiceBase64 is the SHA-256 hash of the corrected invoice,
	// base64-encoded. Non-empty only for corrective invoices.
	HashOfCorrectedInvoiceBase64 string
	// ThirdSubjects lists any third-party subjects recorded on the invoice.
	ThirdSubjects []MetadataThirdSubject
	// AuthorizedSubject is the authorised subject, if present.
	AuthorizedSubject *MetadataAuthorizedSubject
}

// QueryResult holds the outcome of a [Manager.QueryInvoices] call.
type QueryResult struct {
	// HasMore indicates that additional result pages are available.
	// Fetch the next page by setting PageOffset to [NextPageOffset].
	HasMore bool
	// IsTruncated indicates the result set was capped at 10,000 records.
	// When true, narrow DateRange and reset PageOffset to retrieve all
	// matching invoices without hitting the limit.
	IsTruncated bool
	// PermanentStorageHwmDate is the permanent-storage high-water-mark date.
	// Use it as DateRange.To with [DateTypePermanentStorage] to perform
	// incremental synchronisation without missing or duplicating records.
	PermanentStorageHwmDate *time.Time
	// Invoices is the page of invoice metadata returned by this query.
	Invoices []InvoiceMetadata
	// NextPageOffset is the PageOffset value to use in the next call when
	// HasMore is true.
	NextPageOffset int32
}

// ── public API ─────────────────────────────────────────────────────────────────

// QueryInvoices retrieves a page of invoice metadata matching filters.
//
// SubjectType and DateRange are required. Use [QueryResult.HasMore] and
// [QueryResult.NextPageOffset] to iterate through all result pages. When
// [QueryResult.IsTruncated] is true, narrow the DateRange and reset PageOffset
// to avoid the 10,000-record server limit.
func (m *Manager) QueryInvoices(ctx context.Context, s *session.OnlineSession, filters InvoiceQueryFilters) (*QueryResult, error) {
	path := buildQueryPath(filters)
	body := buildQueryBody(filters)
	headers := map[string]string{"Authorization": "Bearer " + s.AccessToken()}

	raw, err := m.http.Post(ctx, path, body, headers)
	if err != nil {
		return nil, fmt.Errorf("invoice: query: %w", err)
	}

	var resp queryMetadataResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("invoice: query: decode response: %w", err)
	}

	return resp.toQueryResult(filters.PageOffset), nil
}

// ── path builder ───────────────────────────────────────────────────────────────

func buildQueryPath(f InvoiceQueryFilters) string {
	q := url.Values{}
	if f.SortOrder != "" {
		q.Set("sortOrder", string(f.SortOrder))
	}
	if f.PageOffset > 0 {
		q.Set("pageOffset", strconv.FormatInt(int64(f.PageOffset), 10))
	}
	if f.PageSize > 0 {
		q.Set("pageSize", strconv.FormatInt(int64(f.PageSize), 10))
	}
	if len(q) == 0 {
		return "/invoices/query/metadata"
	}
	return "/invoices/query/metadata?" + q.Encode()
}

// ── wire types for request ─────────────────────────────────────────────────────

type queryFiltersWire struct {
	SubjectType     string               `json:"subjectType"`
	DateRange       queryDateRangeWire   `json:"dateRange"`
	KsefNumber      string               `json:"ksefNumber,omitempty"`
	InvoiceNumber   string               `json:"invoiceNumber,omitempty"`
	SellerNip       string               `json:"sellerNip,omitempty"`
	BuyerIdentifier *buyerIdentifierWire `json:"buyerIdentifier,omitempty"`
	CurrencyCodes   []string             `json:"currencyCodes,omitempty"`
	InvoiceTypes    []string             `json:"invoiceTypes,omitempty"`
	FormType        string               `json:"formType,omitempty"`
	IsSelfInvoicing *bool                `json:"isSelfInvoicing,omitempty"`
	HasAttachment   *bool                `json:"hasAttachment,omitempty"`
	Amount          *amountFilterWire    `json:"amount,omitempty"`
}

type queryDateRangeWire struct {
	DateType                          string  `json:"dateType"`
	From                              string  `json:"from"`
	To                                *string `json:"to,omitempty"`
	RestrictToPermanentStorageHwmDate *bool   `json:"restrictToPermanentStorageHwmDate,omitempty"`
}

type buyerIdentifierWire struct {
	Type  string `json:"type"`
	Value string `json:"value,omitempty"`
}

type amountFilterWire struct {
	Type string   `json:"type"`
	From *float64 `json:"from,omitempty"`
	To   *float64 `json:"to,omitempty"`
}

func buildQueryBody(f InvoiceQueryFilters) queryFiltersWire {
	dr := queryDateRangeWire{
		DateType:                          string(f.DateRange.DateType),
		From:                              f.DateRange.From.Format(time.RFC3339),
		RestrictToPermanentStorageHwmDate: f.DateRange.RestrictToPermanentStorageHwmDate,
	}
	if !f.DateRange.To.IsZero() {
		s := f.DateRange.To.Format(time.RFC3339)
		dr.To = &s
	}

	w := queryFiltersWire{
		SubjectType:     string(f.SubjectType),
		DateRange:       dr,
		KsefNumber:      f.KSeFNumber,
		InvoiceNumber:   f.InvoiceNumber,
		SellerNip:       f.SellerNIP,
		FormType:        string(f.FormType),
		IsSelfInvoicing: f.IsSelfInvoicing,
		HasAttachment:   f.HasAttachment,
	}
	if f.BuyerIdentifier != nil {
		w.BuyerIdentifier = &buyerIdentifierWire{
			Type:  string(f.BuyerIdentifier.Type),
			Value: f.BuyerIdentifier.Value,
		}
	}
	if len(f.CurrencyCodes) > 0 {
		w.CurrencyCodes = f.CurrencyCodes
	}
	if len(f.InvoiceTypes) > 0 {
		types := make([]string, len(f.InvoiceTypes))
		for i, t := range f.InvoiceTypes {
			types[i] = string(t)
		}
		w.InvoiceTypes = types
	}
	if f.Amount != nil {
		w.Amount = &amountFilterWire{
			Type: string(f.Amount.Type),
			From: f.Amount.From,
			To:   f.Amount.To,
		}
	}
	return w
}

// ── wire types for response ────────────────────────────────────────────────────

type queryMetadataResponse struct {
	HasMore                 bool                  `json:"hasMore"`
	IsTruncated             bool                  `json:"isTruncated"`
	PermanentStorageHwmDate *time.Time            `json:"permanentStorageHwmDate"`
	Invoices                []invoiceMetadataWire `json:"invoices"`
}

type invoiceMetadataWire struct {
	KsefNumber             string                 `json:"ksefNumber"`
	InvoiceNumber          string                 `json:"invoiceNumber"`
	IssueDate              string                 `json:"issueDate"` // "YYYY-MM-DD"
	InvoicingDate          time.Time              `json:"invoicingDate"`
	AcquisitionDate        time.Time              `json:"acquisitionDate"`
	PermanentStorageDate   time.Time              `json:"permanentStorageDate"`
	Seller                 metadataSellerWire     `json:"seller"`
	Buyer                  metadataBuyerWire      `json:"buyer"`
	NetAmount              float64                `json:"netAmount"`
	GrossAmount            float64                `json:"grossAmount"`
	VatAmount              float64                `json:"vatAmount"`
	Currency               string                 `json:"currency"`
	InvoicingMode          string                 `json:"invoicingMode"`
	InvoiceType            string                 `json:"invoiceType"`
	FormCode               formCodeWire           `json:"formCode"`
	IsSelfInvoicing        bool                   `json:"isSelfInvoicing"`
	HasAttachment          bool                   `json:"hasAttachment"`
	InvoiceHash            string                 `json:"invoiceHash"`
	HashOfCorrectedInvoice *string                `json:"hashOfCorrectedInvoice"`
	ThirdSubjects          []thirdSubjectWire     `json:"thirdSubjects"`
	AuthorizedSubject      *authorizedSubjectWire `json:"authorizedSubject"`
}

type metadataSellerWire struct {
	Nip  string `json:"nip"`
	Name string `json:"name"`
}

type metadataBuyerWire struct {
	Identifier buyerIdentifierResponseWire `json:"identifier"`
	Name       string                      `json:"name"`
}

type buyerIdentifierResponseWire struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

type formCodeWire struct {
	SystemCode    string `json:"systemCode"`
	SchemaVersion string `json:"schemaVersion"`
	Value         string `json:"value"`
}

type thirdSubjectWire struct {
	Identifier thirdSubjectIdentifierWire `json:"identifier"`
	Name       string                     `json:"name"`
	Role       int32                      `json:"role"`
}

type thirdSubjectIdentifierWire struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

type authorizedSubjectWire struct {
	Nip  string `json:"nip"`
	Name string `json:"name"`
	Role int32  `json:"role"`
}

func (r *queryMetadataResponse) toQueryResult(pageOffset int32) *QueryResult {
	invoices := make([]InvoiceMetadata, len(r.Invoices))
	for i, w := range r.Invoices {
		invoices[i] = w.toInvoiceMetadata()
	}
	return &QueryResult{
		HasMore:                 r.HasMore,
		IsTruncated:             r.IsTruncated,
		PermanentStorageHwmDate: r.PermanentStorageHwmDate,
		Invoices:                invoices,
		NextPageOffset:          pageOffset + int32(len(invoices)),
	}
}

func (w *invoiceMetadataWire) toInvoiceMetadata() InvoiceMetadata {
	issueDate, _ := time.Parse("2006-01-02", w.IssueDate)

	m := InvoiceMetadata{
		KSeFNumber:           w.KsefNumber,
		InvoiceNumber:        w.InvoiceNumber,
		IssueDate:            issueDate,
		InvoicingDate:        w.InvoicingDate,
		AcquisitionDate:      w.AcquisitionDate,
		PermanentStorageDate: w.PermanentStorageDate,
		Seller: MetadataSeller{
			NIP:  w.Seller.Nip,
			Name: w.Seller.Name,
		},
		Buyer: MetadataBuyer{
			Identifier: MetadataBuyerIdentifier{
				Type:  BuyerIdentifierType(w.Buyer.Identifier.Type),
				Value: w.Buyer.Identifier.Value,
			},
			Name: w.Buyer.Name,
		},
		NetAmount:     w.NetAmount,
		GrossAmount:   w.GrossAmount,
		VATAmount:     w.VatAmount,
		Currency:      w.Currency,
		InvoicingMode: InvoicingMode(w.InvoicingMode),
		InvoiceType:   InvoiceType(w.InvoiceType),
		FormCode: MetadataFormCode{
			SystemCode:    w.FormCode.SystemCode,
			SchemaVersion: w.FormCode.SchemaVersion,
			Value:         w.FormCode.Value,
		},
		IsSelfInvoicing:   w.IsSelfInvoicing,
		HasAttachment:     w.HasAttachment,
		InvoiceHashBase64: w.InvoiceHash,
	}
	if w.HashOfCorrectedInvoice != nil {
		m.HashOfCorrectedInvoiceBase64 = *w.HashOfCorrectedInvoice
	}
	for _, ts := range w.ThirdSubjects {
		m.ThirdSubjects = append(m.ThirdSubjects, MetadataThirdSubject{
			IdentifierType:  ts.Identifier.Type,
			IdentifierValue: ts.Identifier.Value,
			Name:            ts.Name,
			Role:            ts.Role,
		})
	}
	if w.AuthorizedSubject != nil {
		m.AuthorizedSubject = &MetadataAuthorizedSubject{
			NIP:  w.AuthorizedSubject.Nip,
			Name: w.AuthorizedSubject.Name,
			Role: w.AuthorizedSubject.Role,
		}
	}
	return m
}
