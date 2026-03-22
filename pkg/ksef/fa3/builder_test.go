package fa3

import (
	"bytes"
	"encoding/xml"
	"strings"
	"testing"
	"time"
)

// testSellerAddress returns a reusable seller address for tests.
func testSellerAddress() Adres {
	return Adres{
		KodKraju: "PL",
		AdresL1:  "ul. Marszałkowska 10",
		AdresL2:  strptr("00-001 Warszawa"),
	}
}

// testBuyerAddress returns a reusable buyer address for tests.
func testBuyerAddress() Adres {
	return Adres{
		KodKraju: "PL",
		AdresL1:  "ul. Nowy Świat 5A",
		AdresL2:  strptr("00-400 Warszawa"),
	}
}

func strptr(s string) *string { return &s }

// minimalBuilder returns a builder populated with the minimum valid set of fields.
func minimalBuilder() *InvoiceBuilder {
	issue := time.Date(2024, 3, 15, 0, 0, 0, 0, time.UTC)
	sale := time.Date(2024, 3, 15, 0, 0, 0, 0, time.UTC)
	return NewInvoiceBuilder().
		SetSeller("ACME Sp. z o.o.", "1234567890", testSellerAddress()).
		SetBuyer("Klient SA", "0987654321", testBuyerAddress()).
		SetInvoiceNumber("FV/2024/03/001").
		SetDates(issue, sale).
		AddItem(LineItem{
			Description:  "Usługa programistyczna",
			Unit:         "godz",
			Quantity:     "10.00",
			UnitNetPrice: "100.00",
			NetValue:     "1000.00",
			VATRate:      Stawka23,
		})
}

// --- Build ---

func TestBuild_MinimalValid(t *testing.T) {
	t.Helper()
	f, err := minimalBuilder().Build()
	if err != nil {
		t.Fatalf("Build() returned unexpected error: %v", err)
	}
	if f == nil {
		t.Fatal("Build() returned nil Faktura")
	}
}

func TestBuild_InvoiceNumber(t *testing.T) {
	f, err := minimalBuilder().Build()
	if err != nil {
		t.Fatal(err)
	}
	if f.Fa.P_2 != "FV/2024/03/001" {
		t.Errorf("P_2 = %q, want %q", f.Fa.P_2, "FV/2024/03/001")
	}
}

func TestBuild_IssueDate(t *testing.T) {
	f, err := minimalBuilder().Build()
	if err != nil {
		t.Fatal(err)
	}
	if f.Fa.P_1 != "2024-03-15" {
		t.Errorf("P_1 = %q, want %q", f.Fa.P_1, "2024-03-15")
	}
}

func TestBuild_SaleDate(t *testing.T) {
	f, err := minimalBuilder().Build()
	if err != nil {
		t.Fatal(err)
	}
	if f.Fa.P_6 == nil {
		t.Fatal("P_6 is nil, want 2024-03-15")
	}
	if *f.Fa.P_6 != "2024-03-15" {
		t.Errorf("P_6 = %q, want %q", *f.Fa.P_6, "2024-03-15")
	}
}

func TestBuild_DefaultCurrencyPLN(t *testing.T) {
	f, err := minimalBuilder().Build()
	if err != nil {
		t.Fatal(err)
	}
	if f.Fa.KodWaluty != "PLN" {
		t.Errorf("KodWaluty = %q, want %q", f.Fa.KodWaluty, "PLN")
	}
}

func TestBuild_CustomCurrency(t *testing.T) {
	f, err := minimalBuilder().SetCurrency("EUR").Build()
	if err != nil {
		t.Fatal(err)
	}
	if f.Fa.KodWaluty != "EUR" {
		t.Errorf("KodWaluty = %q, want %q", f.Fa.KodWaluty, "EUR")
	}
}

func TestBuild_SellerFields(t *testing.T) {
	f, err := minimalBuilder().Build()
	if err != nil {
		t.Fatal(err)
	}
	got := f.Podmiot1.DaneIdentyfikacyjne
	if got.NIP != "1234567890" {
		t.Errorf("seller NIP = %q, want %q", got.NIP, "1234567890")
	}
	if got.Nazwa != "ACME Sp. z o.o." {
		t.Errorf("seller Nazwa = %q, want %q", got.Nazwa, "ACME Sp. z o.o.")
	}
}

func TestBuild_BuyerFields(t *testing.T) {
	f, err := minimalBuilder().Build()
	if err != nil {
		t.Fatal(err)
	}
	if f.Podmiot2 == nil {
		t.Fatal("Podmiot2 is nil")
	}
	if f.Podmiot2.DaneIdentyfikacyjne.NIP == nil {
		t.Fatal("buyer NIP is nil")
	}
	if *f.Podmiot2.DaneIdentyfikacyjne.NIP != "0987654321" {
		t.Errorf("buyer NIP = %q, want %q", *f.Podmiot2.DaneIdentyfikacyjne.NIP, "0987654321")
	}
	if f.Podmiot2.DaneIdentyfikacyjne.Nazwa != "Klient SA" {
		t.Errorf("buyer Nazwa = %q, want %q", f.Podmiot2.DaneIdentyfikacyjne.Nazwa, "Klient SA")
	}
}

func TestBuild_LineItemCount(t *testing.T) {
	b := minimalBuilder().AddItem(LineItem{
		Description: "Produkt B",
		NetValue:    "500.00",
		VATRate:     Stawka8,
	})
	f, err := b.Build()
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Fa.FaWiersz) != 2 {
		t.Errorf("FaWiersz count = %d, want 2", len(f.Fa.FaWiersz))
	}
}

func TestBuild_LineItemSequentialNumbers(t *testing.T) {
	b := minimalBuilder().AddItem(LineItem{
		Description: "Pozycja 2",
		NetValue:    "200.00",
		VATRate:     Stawka23,
	})
	f, err := b.Build()
	if err != nil {
		t.Fatal(err)
	}
	for i, w := range f.Fa.FaWiersz {
		want := uint32(i + 1)
		if w.NrWierszaFa != want {
			t.Errorf("line %d: NrWierszaFa = %d, want %d", i, w.NrWierszaFa, want)
		}
	}
}

func TestBuild_VATSummary23Percent(t *testing.T) {
	f, err := minimalBuilder().Build()
	if err != nil {
		t.Fatal(err)
	}
	if f.Fa.P_13_1 == nil {
		t.Fatal("P_13_1 is nil, want 1000.00")
	}
	if *f.Fa.P_13_1 != "1000.00" {
		t.Errorf("P_13_1 = %q, want %q", *f.Fa.P_13_1, "1000.00")
	}
	if f.Fa.P_14_1 == nil {
		t.Fatal("P_14_1 is nil, want 230.00")
	}
	if *f.Fa.P_14_1 != "230.00" {
		t.Errorf("P_14_1 = %q, want %q", *f.Fa.P_14_1, "230.00")
	}
}

func TestBuild_GrossTotal(t *testing.T) {
	// 1000 net + 23% VAT (230) = 1230 gross
	f, err := minimalBuilder().Build()
	if err != nil {
		t.Fatal(err)
	}
	if f.Fa.P_15 != "1230.00" {
		t.Errorf("P_15 = %q, want %q", f.Fa.P_15, "1230.00")
	}
}

func TestBuild_Payment(t *testing.T) {
	due := time.Date(2024, 4, 15, 0, 0, 0, 0, time.UTC)
	f, err := minimalBuilder().
		SetPayment(PlatnoscPrzelew, due, "PL61109010140000071219812874").
		Build()
	if err != nil {
		t.Fatal(err)
	}
	if f.Fa.Platnosc == nil || len(f.Fa.Platnosc.RachunekBankowy) == 0 {
		t.Fatal("RachunekBankowy is nil or empty")
	}
	if f.Fa.Platnosc.RachunekBankowy[0].NrRB != "PL61109010140000071219812874" {
		t.Errorf("NrRB = %q, want %q", f.Fa.Platnosc.RachunekBankowy[0].NrRB, "PL61109010140000071219812874")
	}
	if f.Fa.Platnosc == nil {
		t.Fatal("Platnosc is nil")
	}
	if f.Fa.Platnosc.FormaPlatnosci == nil {
		t.Fatal("FormaPlatnosci is nil")
	}
	if *f.Fa.Platnosc.FormaPlatnosci != PlatnoscPrzelew {
		t.Errorf("FormaPlatnosci = %q, want %q", *f.Fa.Platnosc.FormaPlatnosci, PlatnoscPrzelew)
	}
	if len(f.Fa.Platnosc.TerminPlatnosci) == 0 {
		t.Fatal("TerminPlatnosci is empty")
	}
	if f.Fa.Platnosc.TerminPlatnosci[0].Termin != "2024-04-15" {
		t.Errorf("Termin = %q, want %q", f.Fa.Platnosc.TerminPlatnosci[0].Termin, "2024-04-15")
	}
}

func TestBuild_HeaderConstants(t *testing.T) {
	f, err := minimalBuilder().Build()
	if err != nil {
		t.Fatal(err)
	}
	h := f.Naglowek
	if h.KodFormularza.Value != "FA" {
		t.Errorf("KodFormularza.Value = %q, want %q", h.KodFormularza.Value, "FA")
	}
	if h.KodFormularza.KodSystemowy != "FA (3)" {
		t.Errorf("KodSystemowy = %q, want %q", h.KodFormularza.KodSystemowy, "FA (3)")
	}
	if h.KodFormularza.WersjaSchemy != "1-0E" {
		t.Errorf("WersjaSchemy = %q, want %q", h.KodFormularza.WersjaSchemy, "1-0E")
	}
	if h.WariantFormularza != 3 {
		t.Errorf("WariantFormularza = %d, want 3", h.WariantFormularza)
	}
}

func TestBuild_DefaultAnnotationsAreNie(t *testing.T) {
	f, err := minimalBuilder().Build()
	if err != nil {
		t.Fatal(err)
	}
	a := f.Fa.Adnotacje
	for field, val := range map[string]string{
		"P_16": a.P_16, "P_17": a.P_17, "P_18": a.P_18, "P_18A": a.P_18A,
	} {
		if val != Nie {
			t.Errorf("Adnotacje.%s = %q, want %q (Nie)", field, val, Nie)
		}
	}
}

// --- Marshal ---

func TestMarshalXML_XMLDeclaration(t *testing.T) {
	data, err := minimalBuilder().BuildXML()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(data, []byte("<?xml version")) {
		t.Errorf("output does not start with XML declaration, got: %s", data[:min(len(data), 40)])
	}
}

func TestMarshalXML_RootElement(t *testing.T) {
	data, err := minimalBuilder().BuildXML()
	if err != nil {
		t.Fatal(err)
	}
	var root struct {
		XMLName xml.Name
	}
	// Strip declaration before unmarshaling.
	xmlBody := stripXMLDeclaration(data)
	if err := xml.Unmarshal(xmlBody, &root); err != nil {
		t.Fatalf("xml.Unmarshal failed: %v", err)
	}
	if root.XMLName.Local != "Faktura" {
		t.Errorf("root element = %q, want %q", root.XMLName.Local, "Faktura")
	}
	if root.XMLName.Space != Namespace {
		t.Errorf("namespace = %q, want %q", root.XMLName.Space, Namespace)
	}
}

func TestMarshalXML_RoundTrip(t *testing.T) {
	f, err := minimalBuilder().Build()
	if err != nil {
		t.Fatal(err)
	}
	data, err := MarshalXML(f)
	if err != nil {
		t.Fatal(err)
	}
	var got Faktura
	if err := xml.Unmarshal(stripXMLDeclaration(data), &got); err != nil {
		t.Fatalf("xml.Unmarshal failed: %v", err)
	}
	if got.Fa.P_1 != f.Fa.P_1 {
		t.Errorf("P_1 after round-trip = %q, want %q", got.Fa.P_1, f.Fa.P_1)
	}
	if got.Podmiot1.DaneIdentyfikacyjne.NIP != f.Podmiot1.DaneIdentyfikacyjne.NIP {
		t.Errorf("seller NIP after round-trip = %q, want %q",
			got.Podmiot1.DaneIdentyfikacyjne.NIP, f.Podmiot1.DaneIdentyfikacyjne.NIP)
	}
	if len(got.Fa.FaWiersz) != len(f.Fa.FaWiersz) {
		t.Errorf("FaWiersz count after round-trip = %d, want %d",
			len(got.Fa.FaWiersz), len(f.Fa.FaWiersz))
	}
}

func TestMarshalXML_ContainsRequiredFields(t *testing.T) {
	data, err := minimalBuilder().BuildXML()
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	checks := []struct {
		name    string
		snippet string
	}{
		{"issue date", "<P_1>2024-03-15</P_1>"},
		{"invoice number", "<P_2>FV/2024/03/001</P_2>"},
		{"currency", "<KodWaluty>PLN</KodWaluty>"},
		{"seller NIP", "<NIP>1234567890</NIP>"},
		{"line description", "<P_7>Usługa programistyczna</P_7>"},
		{"VAT rate", "<P_12>23</P_12>"},
		{"gross total", "<P_15>1230.00</P_15>"},
	}
	for _, c := range checks {
		if !strings.Contains(s, c.snippet) {
			t.Errorf("XML missing %s: expected to find %q", c.name, c.snippet)
		}
	}
}

func TestMarshalXML_NamespaceInOutput(t *testing.T) {
	data, err := minimalBuilder().BuildXML()
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	if !strings.Contains(s, Namespace) {
		t.Errorf("XML output does not contain namespace %q", Namespace)
	}
}

// --- Validate ---

func TestValidate_Valid(t *testing.T) {
	f := minimalBuilder().assemble()
	if err := Validate(f); err != nil {
		t.Errorf("Validate() returned unexpected error: %v", err)
	}
}

func TestValidate_MissingSellerNIP(t *testing.T) {
	b := minimalBuilder()
	b.sellerNIP = ""
	f := b.assemble()
	err := Validate(f)
	if err == nil {
		t.Fatal("Validate() returned nil, want error for missing seller NIP")
	}
	if !strings.Contains(err.Error(), "seller NIP") {
		t.Errorf("error message %q does not mention 'seller NIP'", err.Error())
	}
}

func TestValidate_InvalidSellerNIP(t *testing.T) {
	cases := []struct {
		name string
		nip  string
	}{
		{"too short", "123456789"},
		{"too long", "12345678901"},
		{"non-digit", "123456789A"},
		{"empty", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := minimalBuilder()
			b.sellerNIP = tc.nip
			if err := Validate(b.assemble()); err == nil {
				t.Errorf("Validate() returned nil for NIP=%q, want error", tc.nip)
			}
		})
	}
}

func TestValidate_InvalidBuyerNIP(t *testing.T) {
	b := minimalBuilder()
	b.buyerNIP = "ABCDEF1234"
	if err := Validate(b.assemble()); err == nil {
		t.Fatal("Validate() returned nil, want error for non-digit buyer NIP")
	}
}

func TestValidate_MissingInvoiceNumber(t *testing.T) {
	b := minimalBuilder()
	b.number = ""
	err := Validate(b.assemble())
	if err == nil {
		t.Fatal("Validate() returned nil, want error for missing invoice number")
	}
	if !strings.Contains(err.Error(), "P_2") {
		t.Errorf("error message %q does not mention 'P_2'", err.Error())
	}
}

func TestValidate_MissingIssueDate(t *testing.T) {
	b := minimalBuilder()
	b.issueDate = time.Time{}
	err := Validate(b.assemble())
	if err == nil {
		t.Fatal("Validate() returned nil, want error for missing issue date")
	}
	if !strings.Contains(err.Error(), "P_1") {
		t.Errorf("error message %q does not mention 'P_1'", err.Error())
	}
}

func TestValidate_NoLineItems(t *testing.T) {
	b := minimalBuilder()
	b.items = nil
	err := Validate(b.assemble())
	if err == nil {
		t.Fatal("Validate() returned nil, want error for no line items")
	}
	if !strings.Contains(err.Error(), "FaWiersz") {
		t.Errorf("error message %q does not mention 'FaWiersz'", err.Error())
	}
}

func TestValidate_LineItemMissingDescription(t *testing.T) {
	b := minimalBuilder()
	b.items = []LineItem{{VATRate: Stawka23, NetValue: "100.00"}}
	err := Validate(b.assemble())
	if err == nil {
		t.Fatal("Validate() returned nil, want error for line item missing description")
	}
	if !strings.Contains(err.Error(), "P_7") {
		t.Errorf("error message %q does not mention 'P_7'", err.Error())
	}
}

func TestValidate_LineItemMissingNetValue(t *testing.T) {
	b := minimalBuilder()
	b.items = []LineItem{{Description: "Test", VATRate: Stawka23}}
	err := Validate(b.assemble())
	if err == nil {
		t.Fatal("Validate() returned nil, want error for line item missing value")
	}
	if !strings.Contains(err.Error(), "P_11") {
		t.Errorf("error message %q does not mention 'P_11'", err.Error())
	}
}

func TestValidate_MultipleErrors(t *testing.T) {
	b := NewInvoiceBuilder() // nothing set
	err := Validate(b.assemble())
	if err == nil {
		t.Fatal("Validate() returned nil on empty builder, want errors")
	}
	// Expect multiple issues reported at once.
	errStr := err.Error()
	for _, keyword := range []string{"NIP", "P_1", "FaWiersz"} {
		if !strings.Contains(errStr, keyword) {
			t.Errorf("expected error to contain %q, full error: %v", keyword, errStr)
		}
	}
}

func TestValidate_NIPExactly10Digits(t *testing.T) {
	if err := validateNIP("1234567890"); err != nil {
		t.Errorf("validateNIP(%q) = %v, want nil", "1234567890", err)
	}
}

// --- helpers ---

// stripXMLDeclaration removes a leading <?xml...?> declaration from XML bytes.
func stripXMLDeclaration(data []byte) []byte {
	s := string(data)
	if idx := strings.Index(s, "\n"); idx != -1 {
		return []byte(s[idx+1:])
	}
	return data
}

// min returns the smaller of a and b.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
