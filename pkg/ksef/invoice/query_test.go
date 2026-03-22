package invoice_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/CodartEU/ksef-go/pkg/ksef/invoice"
)

// baseFilters returns a minimal valid InvoiceQueryFilters for use in tests.
func baseFilters() invoice.InvoiceQueryFilters {
	return invoice.InvoiceQueryFilters{
		SubjectType: invoice.SubjectSeller,
		DateRange: invoice.QueryDateRange{
			DateType: invoice.DateTypeInvoicing,
			From:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		},
	}
}

// queryResponse is the JSON shape returned by POST /invoices/query/metadata.
type queryResponse struct {
	HasMore                 bool                  `json:"hasMore"`
	IsTruncated             bool                  `json:"isTruncated"`
	PermanentStorageHwmDate *time.Time            `json:"permanentStorageHwmDate,omitempty"`
	Invoices                []invoiceMetadataJSON `json:"invoices"`
}

type invoiceMetadataJSON struct {
	KsefNumber           string       `json:"ksefNumber"`
	InvoiceNumber        string       `json:"invoiceNumber"`
	IssueDate            string       `json:"issueDate"`
	InvoicingDate        time.Time    `json:"invoicingDate"`
	AcquisitionDate      time.Time    `json:"acquisitionDate"`
	PermanentStorageDate time.Time    `json:"permanentStorageDate"`
	Seller               sellerJSON   `json:"seller"`
	Buyer                buyerJSON    `json:"buyer"`
	NetAmount            float64      `json:"netAmount"`
	GrossAmount          float64      `json:"grossAmount"`
	VatAmount            float64      `json:"vatAmount"`
	Currency             string       `json:"currency"`
	InvoicingMode        string       `json:"invoicingMode"`
	InvoiceType          string       `json:"invoiceType"`
	FormCode             formCodeJSON `json:"formCode"`
	IsSelfInvoicing      bool         `json:"isSelfInvoicing"`
	HasAttachment        bool         `json:"hasAttachment"`
	InvoiceHash          string       `json:"invoiceHash"`
}

type sellerJSON struct {
	Nip  string `json:"nip"`
	Name string `json:"name"`
}

type buyerJSON struct {
	Identifier buyerIdentifierJSON `json:"identifier"`
	Name       string              `json:"name"`
}

type buyerIdentifierJSON struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

type formCodeJSON struct {
	SystemCode    string `json:"systemCode"`
	SchemaVersion string `json:"schemaVersion"`
	Value         string `json:"value"`
}

// sampleInvoice returns a populated invoiceMetadataJSON for test assertions.
func sampleInvoice() invoiceMetadataJSON {
	now := time.Now().UTC().Truncate(time.Second)
	return invoiceMetadataJSON{
		KsefNumber:           testKSeFNumber,
		InvoiceNumber:        "FV/2026/001",
		IssueDate:            "2026-01-15",
		InvoicingDate:        now,
		AcquisitionDate:      now,
		PermanentStorageDate: now,
		Seller:               sellerJSON{Nip: "5265877635", Name: "Firma Sp. z o.o."},
		Buyer:                buyerJSON{Identifier: buyerIdentifierJSON{Type: "Nip", Value: "9999999999"}, Name: "Nabywca S.A."},
		NetAmount:            1000.00,
		GrossAmount:          1230.00,
		VatAmount:            230.00,
		Currency:             "PLN",
		InvoicingMode:        "Online",
		InvoiceType:          "Vat",
		FormCode:             formCodeJSON{SystemCode: "FA (3)", SchemaVersion: "1-0E", Value: "FA"},
		InvoiceHash:          "abc123base64==",
	}
}

func TestQueryInvoices_Success(t *testing.T) {
	inv := sampleInvoice()
	resp := queryResponse{
		HasMore:     false,
		IsTruncated: false,
		Invoices:    []invoiceMetadataJSON{inv},
	}
	body, _ := json.Marshal(resp)

	routes := map[string]http.HandlerFunc{
		"POST /invoices/query/metadata": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(body)
		},
	}

	mgr, s := testEnv(t, routes)

	result, err := mgr.QueryInvoices(context.Background(), s, baseFilters())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.HasMore {
		t.Error("HasMore should be false")
	}
	if result.IsTruncated {
		t.Error("IsTruncated should be false")
	}
	if len(result.Invoices) != 1 {
		t.Fatalf("want 1 invoice, got %d", len(result.Invoices))
	}

	got := result.Invoices[0]
	if got.KSeFNumber != testKSeFNumber {
		t.Errorf("KSeFNumber = %q, want %q", got.KSeFNumber, testKSeFNumber)
	}
	if got.InvoiceNumber != inv.InvoiceNumber {
		t.Errorf("InvoiceNumber = %q, want %q", got.InvoiceNumber, inv.InvoiceNumber)
	}
	if got.IssueDate.Format("2006-01-02") != inv.IssueDate {
		t.Errorf("IssueDate = %q, want %q", got.IssueDate.Format("2006-01-02"), inv.IssueDate)
	}
	if got.Seller.NIP != inv.Seller.Nip {
		t.Errorf("Seller.NIP = %q, want %q", got.Seller.NIP, inv.Seller.Nip)
	}
	if got.Buyer.Identifier.Type != invoice.BuyerIdentifierNIP {
		t.Errorf("Buyer.Identifier.Type = %q, want %q", got.Buyer.Identifier.Type, invoice.BuyerIdentifierNIP)
	}
	if got.NetAmount != inv.NetAmount {
		t.Errorf("NetAmount = %v, want %v", got.NetAmount, inv.NetAmount)
	}
	if got.GrossAmount != inv.GrossAmount {
		t.Errorf("GrossAmount = %v, want %v", got.GrossAmount, inv.GrossAmount)
	}
	if got.VATAmount != inv.VatAmount {
		t.Errorf("VATAmount = %v, want %v", got.VATAmount, inv.VatAmount)
	}
	if got.Currency != inv.Currency {
		t.Errorf("Currency = %q, want %q", got.Currency, inv.Currency)
	}
	if got.InvoicingMode != invoice.InvoicingModeOnline {
		t.Errorf("InvoicingMode = %q, want %q", got.InvoicingMode, invoice.InvoicingModeOnline)
	}
	if got.InvoiceType != invoice.InvoiceTypeVat {
		t.Errorf("InvoiceType = %q, want %q", got.InvoiceType, invoice.InvoiceTypeVat)
	}
	if got.FormCode.SystemCode != inv.FormCode.SystemCode {
		t.Errorf("FormCode.SystemCode = %q, want %q", got.FormCode.SystemCode, inv.FormCode.SystemCode)
	}
	if got.InvoiceHashBase64 != inv.InvoiceHash {
		t.Errorf("InvoiceHashBase64 = %q, want %q", got.InvoiceHashBase64, inv.InvoiceHash)
	}
}

func TestQueryInvoices_EmptyResults(t *testing.T) {
	resp := queryResponse{HasMore: false, IsTruncated: false, Invoices: []invoiceMetadataJSON{}}
	body, _ := json.Marshal(resp)

	routes := map[string]http.HandlerFunc{
		"POST /invoices/query/metadata": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(body)
		},
	}

	mgr, s := testEnv(t, routes)

	result, err := mgr.QueryInvoices(context.Background(), s, baseFilters())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Invoices) != 0 {
		t.Errorf("want 0 invoices, got %d", len(result.Invoices))
	}
	if result.NextPageOffset != 0 {
		t.Errorf("NextPageOffset = %d, want 0", result.NextPageOffset)
	}
}

func TestQueryInvoices_Pagination(t *testing.T) {
	inv := sampleInvoice()
	pageSize := 10

	// Build a page of 10 invoices with HasMore=true.
	invoices := make([]invoiceMetadataJSON, pageSize)
	for i := range invoices {
		invoices[i] = inv
	}
	resp := queryResponse{HasMore: true, IsTruncated: false, Invoices: invoices}
	body, _ := json.Marshal(resp)

	routes := map[string]http.HandlerFunc{
		"POST /invoices/query/metadata": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(body)
		},
	}

	mgr, s := testEnv(t, routes)

	filters := baseFilters()
	filters.PageOffset = 0
	filters.PageSize = 10

	result, err := mgr.QueryInvoices(context.Background(), s, filters)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.HasMore {
		t.Error("HasMore should be true")
	}
	if result.NextPageOffset != int32(pageSize) {
		t.Errorf("NextPageOffset = %d, want %d", result.NextPageOffset, pageSize)
	}
}

func TestQueryInvoices_IsTruncated(t *testing.T) {
	hwm := time.Date(2026, 2, 15, 12, 0, 0, 0, time.UTC)
	resp := queryResponse{
		HasMore:                 false,
		IsTruncated:             true,
		PermanentStorageHwmDate: &hwm,
		Invoices:                []invoiceMetadataJSON{sampleInvoice()},
	}
	body, _ := json.Marshal(resp)

	routes := map[string]http.HandlerFunc{
		"POST /invoices/query/metadata": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(body)
		},
	}

	mgr, s := testEnv(t, routes)

	result, err := mgr.QueryInvoices(context.Background(), s, baseFilters())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsTruncated {
		t.Error("IsTruncated should be true")
	}
	if result.PermanentStorageHwmDate == nil {
		t.Fatal("PermanentStorageHwmDate should be set")
	}
	if !result.PermanentStorageHwmDate.Equal(hwm) {
		t.Errorf("PermanentStorageHwmDate = %v, want %v", result.PermanentStorageHwmDate, hwm)
	}
}

func TestQueryInvoices_RequestBody(t *testing.T) {
	to := time.Date(2026, 3, 31, 23, 59, 59, 0, time.UTC)

	filters := invoice.InvoiceQueryFilters{
		SubjectType: invoice.SubjectBuyer,
		DateRange: invoice.QueryDateRange{
			DateType: invoice.DateTypePermanentStorage,
			From:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			To:       to,
		},
		KSeFNumber:    testKSeFNumber,
		InvoiceNumber: "FV/2026/001",
		SellerNIP:     "5265877635",
		CurrencyCodes: []string{"PLN", "EUR"},
		InvoiceTypes:  []invoice.InvoiceType{invoice.InvoiceTypeVat, invoice.InvoiceTypeKor},
		FormType:      invoice.FormTypeFA,
	}

	var capturedBody map[string]any

	routes := map[string]http.HandlerFunc{
		"POST /invoices/query/metadata": func(w http.ResponseWriter, r *http.Request) {
			if err := json.NewDecoder(r.Body).Decode(&capturedBody); err != nil {
				t.Errorf("decode request body: %v", err)
			}
			resp := queryResponse{Invoices: []invoiceMetadataJSON{}}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
		},
	}

	mgr, s := testEnv(t, routes)
	_, err := mgr.QueryInvoices(context.Background(), s, filters)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if capturedBody["subjectType"] != "Subject2" {
		t.Errorf("subjectType = %v, want Subject2", capturedBody["subjectType"])
	}

	dr, _ := capturedBody["dateRange"].(map[string]any)
	if dr == nil {
		t.Fatal("dateRange must be set in request body")
	}
	if dr["dateType"] != "PermanentStorage" {
		t.Errorf("dateRange.dateType = %v, want PermanentStorage", dr["dateType"])
	}
	if dr["from"] == "" {
		t.Error("dateRange.from must not be empty")
	}
	if dr["to"] == nil {
		t.Error("dateRange.to must be set when To is non-zero")
	}

	if capturedBody["ksefNumber"] != testKSeFNumber {
		t.Errorf("ksefNumber = %v, want %q", capturedBody["ksefNumber"], testKSeFNumber)
	}
	if capturedBody["invoiceNumber"] != "FV/2026/001" {
		t.Errorf("invoiceNumber = %v, want FV/2026/001", capturedBody["invoiceNumber"])
	}
	if capturedBody["sellerNip"] != "5265877635" {
		t.Errorf("sellerNip = %v, want 5265877635", capturedBody["sellerNip"])
	}
	if capturedBody["formType"] != "FA" {
		t.Errorf("formType = %v, want FA", capturedBody["formType"])
	}

	// currencyCodes
	cc, _ := capturedBody["currencyCodes"].([]any)
	if len(cc) != 2 {
		t.Errorf("currencyCodes len = %d, want 2", len(cc))
	}

	// invoiceTypes
	it, _ := capturedBody["invoiceTypes"].([]any)
	if len(it) != 2 {
		t.Errorf("invoiceTypes len = %d, want 2", len(it))
	}
}

func TestQueryInvoices_QueryParams(t *testing.T) {
	var capturedURL string

	routes := map[string]http.HandlerFunc{
		"POST /invoices/query/metadata": func(w http.ResponseWriter, r *http.Request) {
			capturedURL = r.URL.String()
			resp := queryResponse{Invoices: []invoiceMetadataJSON{}}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
		},
	}

	mgr, s := testEnv(t, routes)

	filters := baseFilters()
	filters.SortOrder = invoice.SortDesc
	filters.PageOffset = 20
	filters.PageSize = 50

	_, err := mgr.QueryInvoices(context.Background(), s, filters)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(capturedURL, "sortOrder=Desc") {
		t.Errorf("URL %q missing sortOrder=Desc", capturedURL)
	}
	if !strings.Contains(capturedURL, "pageOffset=20") {
		t.Errorf("URL %q missing pageOffset=20", capturedURL)
	}
	if !strings.Contains(capturedURL, "pageSize=50") {
		t.Errorf("URL %q missing pageSize=50", capturedURL)
	}
}

func TestQueryInvoices_NoQueryParamsWhenDefaults(t *testing.T) {
	var capturedURL string

	routes := map[string]http.HandlerFunc{
		"POST /invoices/query/metadata": func(w http.ResponseWriter, r *http.Request) {
			capturedURL = r.URL.String()
			resp := queryResponse{Invoices: []invoiceMetadataJSON{}}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
		},
	}

	mgr, s := testEnv(t, routes)

	_, err := mgr.QueryInvoices(context.Background(), s, baseFilters())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if capturedURL != "/invoices/query/metadata" {
		t.Errorf("URL = %q, want plain path without query string", capturedURL)
	}
}

func TestQueryInvoices_OptionalFieldsOmitted(t *testing.T) {
	var capturedBody map[string]any

	routes := map[string]http.HandlerFunc{
		"POST /invoices/query/metadata": func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewDecoder(r.Body).Decode(&capturedBody)
			resp := queryResponse{Invoices: []invoiceMetadataJSON{}}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
		},
	}

	mgr, s := testEnv(t, routes)
	_, err := mgr.QueryInvoices(context.Background(), s, baseFilters())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, field := range []string{
		"ksefNumber", "invoiceNumber", "sellerNip", "buyerIdentifier",
		"currencyCodes", "invoiceTypes", "formType", "isSelfInvoicing", "hasAttachment", "amount",
	} {
		if _, present := capturedBody[field]; present {
			t.Errorf("optional field %q should be omitted when not set", field)
		}
	}
}

func TestQueryInvoices_Error(t *testing.T) {
	routes := map[string]http.HandlerFunc{
		"POST /invoices/query/metadata": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"exception":{"exceptionDetailList":[{"exceptionCode":21405,"exceptionDescription":"Błąd walidacji."}]}}`))
		},
	}

	mgr, s := testEnv(t, routes)

	_, err := mgr.QueryInvoices(context.Background(), s, baseFilters())
	if err == nil {
		t.Fatal("want error for 400 response, got nil")
	}
	if !strings.Contains(err.Error(), "invoice: query") {
		t.Errorf("error %q should contain %q", err.Error(), "invoice: query")
	}
}

func TestQueryInvoices_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	mgr, s := testEnv(t, nil)
	_, err := mgr.QueryInvoices(ctx, s, baseFilters())
	if err == nil {
		t.Fatal("want error for cancelled context, got nil")
	}
}

func TestQueryInvoices_NextPageOffsetWithInitialOffset(t *testing.T) {
	// When starting from pageOffset=30 and receiving 5 results, NextPageOffset should be 35.
	invoices := make([]invoiceMetadataJSON, 5)
	for i := range invoices {
		invoices[i] = sampleInvoice()
	}
	resp := queryResponse{HasMore: true, Invoices: invoices}
	body, _ := json.Marshal(resp)

	routes := map[string]http.HandlerFunc{
		"POST /invoices/query/metadata": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(body)
		},
	}

	mgr, s := testEnv(t, routes)

	filters := baseFilters()
	filters.PageOffset = 30

	result, err := mgr.QueryInvoices(context.Background(), s, filters)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.NextPageOffset != 35 {
		t.Errorf("NextPageOffset = %d, want 35", result.NextPageOffset)
	}
}

func TestQueryInvoices_ThirdSubjectsAndAuthorizedSubject(t *testing.T) {
	inv := sampleInvoice()
	respBody := map[string]any{
		"hasMore":     false,
		"isTruncated": false,
		"invoices": []map[string]any{
			{
				"ksefNumber":           inv.KsefNumber,
				"invoiceNumber":        inv.InvoiceNumber,
				"issueDate":            inv.IssueDate,
				"invoicingDate":        inv.InvoicingDate,
				"acquisitionDate":      inv.AcquisitionDate,
				"permanentStorageDate": inv.PermanentStorageDate,
				"seller":               inv.Seller,
				"buyer":                inv.Buyer,
				"netAmount":            inv.NetAmount,
				"grossAmount":          inv.GrossAmount,
				"vatAmount":            inv.VatAmount,
				"currency":             inv.Currency,
				"invoicingMode":        inv.InvoicingMode,
				"invoiceType":          inv.InvoiceType,
				"formCode":             inv.FormCode,
				"isSelfInvoicing":      inv.IsSelfInvoicing,
				"hasAttachment":        inv.HasAttachment,
				"invoiceHash":          inv.InvoiceHash,
				"thirdSubjects": []map[string]any{
					{
						"identifier": map[string]any{"type": "Nip", "value": "1111111111"},
						"name":       "Faktor Sp. z o.o.",
						"role":       1,
					},
				},
				"authorizedSubject": map[string]any{
					"nip":  "2222222222",
					"name": "Przedstawiciel",
					"role": 3,
				},
			},
		},
	}
	body, _ := json.Marshal(respBody)

	routes := map[string]http.HandlerFunc{
		"POST /invoices/query/metadata": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(body)
		},
	}

	mgr, s := testEnv(t, routes)

	result, err := mgr.QueryInvoices(context.Background(), s, baseFilters())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Invoices) != 1 {
		t.Fatalf("want 1 invoice, got %d", len(result.Invoices))
	}

	got := result.Invoices[0]
	if len(got.ThirdSubjects) != 1 {
		t.Fatalf("ThirdSubjects len = %d, want 1", len(got.ThirdSubjects))
	}
	ts := got.ThirdSubjects[0]
	if ts.IdentifierType != "Nip" {
		t.Errorf("ThirdSubject.IdentifierType = %q, want Nip", ts.IdentifierType)
	}
	if ts.IdentifierValue != "1111111111" {
		t.Errorf("ThirdSubject.IdentifierValue = %q, want 1111111111", ts.IdentifierValue)
	}
	if ts.Role != 1 {
		t.Errorf("ThirdSubject.Role = %d, want 1", ts.Role)
	}

	if got.AuthorizedSubject == nil {
		t.Fatal("AuthorizedSubject should be set")
	}
	if got.AuthorizedSubject.NIP != "2222222222" {
		t.Errorf("AuthorizedSubject.NIP = %q, want 2222222222", got.AuthorizedSubject.NIP)
	}
	if got.AuthorizedSubject.Role != 3 {
		t.Errorf("AuthorizedSubject.Role = %d, want 3", got.AuthorizedSubject.Role)
	}
}
