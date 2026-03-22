package fa3

import (
	"fmt"
	"strconv"
	"time"
)

// LineItem represents a single invoice line item in the builder API.
// It maps to FaWiersz in the FA(3) schema.
type LineItem struct {
	// Description is the name or description of the goods/services supplied (P_7).
	Description string

	// Unit is the unit of measurement (P_8A), e.g. "szt", "kg", "godz", "usł".
	Unit string

	// Quantity is the number of units supplied (P_8B), as a decimal string.
	Quantity string

	// UnitNetPrice is the net price per unit excluding VAT (P_9A), decimal string.
	UnitNetPrice string

	// NetValue is the total net value of the line after discounts (P_11), decimal string.
	NetValue string

	// VATRate is the VAT rate code (P_12). Use the Stawka* constants.
	VATRate string
}

// InvoiceBuilder provides a fluent interface for constructing a Faktura.
// Use NewInvoiceBuilder to create an instance, then chain setter calls before
// calling Build or BuildXML.
type InvoiceBuilder struct {
	sellerName    string
	sellerNIP     string
	sellerAddress Adres
	buyerName     string
	buyerNIP      string
	buyerAddress  *Adres
	number        string
	issueDate     time.Time
	saleDate      time.Time
	paymentMethod string
	paymentDue    time.Time
	bankAccount   string
	currency      string
	items         []LineItem
}

// NewInvoiceBuilder creates a new InvoiceBuilder with PLN as the default currency.
func NewInvoiceBuilder() *InvoiceBuilder {
	return &InvoiceBuilder{currency: "PLN"}
}

// SetSeller sets the seller (Podmiot1) identity and address.
func (b *InvoiceBuilder) SetSeller(name, nip string, address Adres) *InvoiceBuilder {
	b.sellerName = name
	b.sellerNIP = nip
	b.sellerAddress = address
	return b
}

// SetBuyer sets the buyer (Podmiot2) identity and address.
func (b *InvoiceBuilder) SetBuyer(name, nip string, address Adres) *InvoiceBuilder {
	b.buyerName = name
	b.buyerNIP = nip
	a := address
	b.buyerAddress = &a
	return b
}

// SetInvoiceNumber sets the unique invoice number (P_2).
func (b *InvoiceBuilder) SetInvoiceNumber(number string) *InvoiceBuilder {
	b.number = number
	return b
}

// SetDates sets the invoice issue date (P_1) and the date of supply (P_6).
func (b *InvoiceBuilder) SetDates(issue, sale time.Time) *InvoiceBuilder {
	b.issueDate = issue
	b.saleDate = sale
	return b
}

// SetPayment configures payment terms: method code, due date, and the seller's
// bank account number. Use the Platnosc* constants for the method parameter.
// Pass a zero time.Time to omit the due date.
func (b *InvoiceBuilder) SetPayment(method string, dueDate time.Time, bankAccount string) *InvoiceBuilder {
	b.paymentMethod = method
	b.paymentDue = dueDate
	b.bankAccount = bankAccount
	return b
}

// SetCurrency sets the ISO 4217 currency code for the invoice (default: "PLN").
func (b *InvoiceBuilder) SetCurrency(code string) *InvoiceBuilder {
	b.currency = code
	return b
}

// AddItem appends a line item to the invoice.
func (b *InvoiceBuilder) AddItem(item LineItem) *InvoiceBuilder {
	b.items = append(b.items, item)
	return b
}

// Build validates the accumulated data and returns a *Faktura ready for marshaling.
// Returns an error if validation fails.
func (b *InvoiceBuilder) Build() (*Faktura, error) {
	f := b.assemble()
	if err := Validate(f); err != nil {
		return nil, fmt.Errorf("build: %w", err)
	}
	return f, nil
}

// BuildXML validates, builds, and marshals the invoice to FA(3) XML bytes.
func (b *InvoiceBuilder) BuildXML() ([]byte, error) {
	f, err := b.Build()
	if err != nil {
		return nil, err
	}
	return MarshalXML(f)
}

// assemble constructs the Faktura struct from builder fields without validation.
func (b *InvoiceBuilder) assemble() *Faktura {
	now := time.Now()

	// Build line items, computing VAT summary in the same pass.
	vatBases := make(map[string]float64)   // rate key -> net base sum
	vatAmounts := make(map[string]float64) // rate key -> VAT amount sum
	var grossTotal float64

	wierszeList := make([]FaWiersz, len(b.items))
	for i, item := range b.items {
		w := FaWiersz{
			NrWierszaFa: uint32(i + 1),
			P_7:         item.Description,
			P_12:        item.VATRate,
		}
		if item.Unit != "" {
			w.P_8A = ptr(item.Unit)
		}
		if item.Quantity != "" {
			w.P_8B = ptr(item.Quantity)
		}
		if item.UnitNetPrice != "" {
			w.P_9A = ptr(item.UnitNetPrice)
		}
		if item.NetValue != "" {
			w.P_11 = ptr(item.NetValue)
		}
		wierszeList[i] = w

		// Accumulate VAT summary.
		netVal, _ := strconv.ParseFloat(item.NetValue, 64)
		rateKey, vatPct := vatRatePercent(item.VATRate)
		vatAmt := netVal * vatPct / 100
		vatBases[rateKey] += netVal
		vatAmounts[rateKey] += vatAmt
		grossTotal += netVal + vatAmt
	}

	fa := Fa{
		KodWaluty: b.currency,
		P_1:       formatDate(b.issueDate),
		P_2:       b.number,
		P_15:      fmt.Sprintf("%.2f", grossTotal),
		Adnotacje: Adnotacje{
			P_16:                 Nie,
			P_17:                 Nie,
			P_18:                 Nie,
			P_18A:                Nie,
			Zwolnienie:           Zwolnienie{P_19N: ptr(Tak)},
			NoweSrodkiTransportu: NoweSrodkiTransportu{P_22N: ptr(Tak)},
			P_23:                 Nie,
			PMarzy:               PMarzy{P_PMarzyN: ptr(Tak)},
		},
		RodzajFaktury: RodzajVAT,
		FaWiersz:      wierszeList,
	}

	if !b.saleDate.IsZero() {
		fa.P_6 = ptr(formatDate(b.saleDate))
	}

	populateVATSummary(&fa, vatBases, vatAmounts)

	if b.paymentMethod != "" || !b.paymentDue.IsZero() || b.bankAccount != "" {
		platnosc := &Platnosc{}
		if !b.paymentDue.IsZero() {
			platnosc.TerminPlatnosci = []TerminPlatnosci{
				{Termin: formatDate(b.paymentDue)},
			}
		}
		if b.paymentMethod != "" {
			platnosc.FormaPlatnosci = ptr(b.paymentMethod)
		}
		if b.bankAccount != "" {
			platnosc.RachunekBankowy = []RachunekBankowy{{NrRB: b.bankAccount}}
		}
		fa.Platnosc = platnosc
	}

	f := &Faktura{
		Naglowek: Naglowek{
			KodFormularza: KodFormularza{
				KodSystemowy: "FA (3)",
				WersjaSchemy: "1-0E",
				Value:        "FA",
			},
			WariantFormularza: 3,
			DataWytworzeniaFa: now.Format("2006-01-02T15:04:05"),
		},
		Podmiot1: Podmiot1{
			DaneIdentyfikacyjne: DaneIdentyfikacyjne{
				NIP:   b.sellerNIP,
				Nazwa: b.sellerName,
			},
			Adres: b.sellerAddress,
		},
		Fa: fa,
	}

	if b.buyerName != "" || b.buyerNIP != "" {
		nip := b.buyerNIP
		f.Podmiot2 = &Podmiot2{
			DaneIdentyfikacyjne: DaneIdentyfikacyjnePodmiot2{
				NIP:   &nip,
				Nazwa: b.buyerName,
			},
			Adres: b.buyerAddress,
			JST:   JSTNie,
			GV:    GVNie,
		}
	}

	return f
}

// vatRatePercent returns a canonical rate key and the numeric VAT percentage
// for the given rate code. Non-numeric/exempt rates return 0 as percentage.
func vatRatePercent(rate string) (key string, pct float64) {
	switch rate {
	case Stawka23:
		return "23", 23
	case Stawka8:
		return "8", 8
	case Stawka5:
		return "5", 5
	case Stawka0:
		return "0", 0
	case StawkaZW:
		return "ZW", 0
	case StawkaOO:
		return "OO", 0
	case StawkaNP:
		return "NP", 0
	case StawkaNN:
		return "NN", 0
	default:
		pct, _ = strconv.ParseFloat(rate, 64)
		return rate, pct
	}
}

// populateVATSummary sets the per-rate summary fields (P_13_x, P_14_x) on fa.
// Field assignments match the FA(3) v1-0E schema sequence.
func populateVATSummary(fa *Fa, bases, amounts map[string]float64) {
	type rateMapping struct {
		key    string
		base   **string
		amount **string
	}
	mappings := []rateMapping{
		{"23", &fa.P_13_1, &fa.P_14_1},
		{"8", &fa.P_13_2, &fa.P_14_2},
		{"5", &fa.P_13_3, &fa.P_14_3},
	}
	for _, m := range mappings {
		if v, ok := bases[m.key]; ok {
			*m.base = ptr(fmt.Sprintf("%.2f", v))
			*m.amount = ptr(fmt.Sprintf("%.2f", amounts[m.key]))
		}
	}
	// 0% domestic supplies go into P_13_6_1 (no VAT amount field for 0% rate).
	if v, ok := bases["0"]; ok {
		fa.P_13_6_1 = ptr(fmt.Sprintf("%.2f", v))
	}
	// VAT-exempt (ZW) → P_13_7; outside-territory (OO/NP) → P_13_8.
	if v, ok := bases["ZW"]; ok {
		fa.P_13_7 = ptr(fmt.Sprintf("%.2f", v))
	}
	if v, ok := bases["OO"]; ok {
		fa.P_13_8 = ptr(fmt.Sprintf("%.2f", v))
	}
	if v, ok := bases["NP"]; ok {
		fa.P_13_8 = ptr(fmt.Sprintf("%.2f", v))
	}
}

// ptr returns a pointer to a copy of s.
func ptr(s string) *string { return &s }

// formatDate formats t as YYYY-MM-DD. Returns an empty string for zero time.
func formatDate(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format("2006-01-02")
}
