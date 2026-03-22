// Package fa3 provides Go types for the FA(3) e-invoice XML schema used by the
// Polish National e-Invoicing System (KSeF). The schema is published by the
// Ministry of Finance at:
//
//	http://crd.gov.pl/wzor/2025/06/25/13775/
//
// FA(3) is the third revision of the structured invoice format (Faktura)
// mandatory for VAT-registered entities in Poland. All monetary values are
// represented as strings with up to two decimal places (e.g. "1234.56") to
// preserve precision across serialisation boundaries.
package fa3

import "encoding/xml"

// Namespace is the XML namespace for the FA(3) invoice schema.
const Namespace = "http://crd.gov.pl/wzor/2025/06/25/13775/"

// Invoice type (RodzajFaktury) constants.
const (
	// RodzajVAT is a standard VAT invoice.
	RodzajVAT = "VAT"
	// RodzajZAL is an advance payment invoice.
	RodzajZAL = "ZAL"
	// RodzajROZ is a settlement invoice for a prior advance.
	RodzajROZ = "ROZ"
	// RodzajKOR is a corrective invoice.
	RodzajKOR = "KOR"
	// RodzajKORZAL is a corrective advance payment invoice.
	RodzajKORZAL = "KOR_ZAL"
	// RodzajKORROZ is a corrective settlement invoice.
	RodzajKORROZ = "KOR_ROZ"
	// RodzajUPR is a simplified invoice (uproszczona).
	RodzajUPR = "UPR"
)

// VAT rate (P_12) constants used on line items and in summary fields.
const (
	// Stawka23 is the standard 23% VAT rate.
	Stawka23 = "23"
	// Stawka8 is the reduced 8% VAT rate.
	Stawka8 = "8"
	// Stawka5 is the reduced 5% VAT rate.
	Stawka5 = "5"
	// Stawka0 is the zero-rate (0%) VAT.
	Stawka0 = "0"
	// StawkaZW indicates a VAT-exempt supply (zwolniony).
	StawkaZW = "ZW"
	// StawkaOO indicates a supply outside VAT scope (poza VAT).
	StawkaOO = "OO"
	// StawkaNP indicates a non-taxable supply (niepodlegający).
	StawkaNP = "NP"
	// StawkaNN indicates the rate is not applicable (nie dotyczy).
	StawkaNN = "NN"
)

// Payment method (FormaPlatnosci) constants.
const (
	// PlatnoscGotowka is cash payment.
	PlatnoscGotowka = "1"
	// PlatnoscKarta is card payment.
	PlatnoscKarta = "2"
	// PlatnoscPrzelew is bank transfer.
	PlatnoscPrzelew = "3"
	// PlatnoscCzek is cheque payment.
	PlatnoscCzek = "4"
	// PlatnoscBarter is barter exchange.
	PlatnoscBarter = "5"
	// PlatnoscInna is any other payment method.
	PlatnoscInna = "6"
)

// Annotation flag values used in Adnotacje fields (P_16–P_23).
const (
	// Tak indicates "yes" for a boolean annotation flag.
	Tak = "1"
	// Nie indicates "no" for a boolean annotation flag.
	Nie = "2"
)

// -------------------------------------------------------------------------
// Root element
// -------------------------------------------------------------------------

// Faktura is the root element of an FA(3) e-invoice document.
// It maps directly to the <Faktura> element in the KSeF XML schema.
type Faktura struct {
	XMLName xml.Name `xml:"http://crd.gov.pl/wzor/2025/06/25/13775/ Faktura"`

	// Naglowek contains document header metadata (form code, creation time).
	Naglowek Naglowek `xml:"Naglowek"`

	// Podmiot1 identifies the seller (vendor / issuer of the invoice).
	Podmiot1 Podmiot1 `xml:"Podmiot1"`

	// Podmiot2 identifies the buyer. Optional for B2C anonymous invoices.
	Podmiot2 *Podmiot2 `xml:"Podmiot2,omitempty"`

	// Podmiot3 identifies an optional third party such as a factoring company,
	// ordering party, or delivery recipient.
	Podmiot3 *Podmiot3 `xml:"Podmiot3,omitempty"`

	// Fa contains the core invoice data: type, dates, line items, and VAT summary.
	Fa Fa `xml:"Fa"`

	// Stopka holds optional footer information such as notes or registry entries.
	Stopka *Stopka `xml:"Stopka,omitempty"`
}

// -------------------------------------------------------------------------
// Header
// -------------------------------------------------------------------------

// Naglowek contains invoice header metadata required by the FA(3) schema.
type Naglowek struct {
	// KodFormularza identifies the form type and schema version.
	// The element text must be "FA"; set KodSystemowy="FA (3)" and WersjaSchemy="1-0E".
	KodFormularza KodFormularza `xml:"KodFormularza"`

	// WariantFormularza is the schema variant number; must be 3 for FA(3).
	WariantFormularza uint8 `xml:"WariantFormularza"`

	// DataWytworzeniaFa is the timestamp at which the invoice file was created,
	// formatted as ISO 8601 datetime: YYYY-MM-DDTHH:MM:SS (local or with offset).
	DataWytworzeniaFa string `xml:"DataWytworzeniaFa"`

	// SystemInfo optionally names the software system that generated the invoice
	// (max 256 characters).
	SystemInfo *string `xml:"SystemInfo,omitempty"`
}

// KodFormularza holds the form identifier along with its schema attributes.
type KodFormularza struct {
	// KodSystemowy is the system-level form code, e.g. "FA (3)".
	KodSystemowy string `xml:"kodSystemowy,attr"`

	// WersjaSchemy is the schema version string, e.g. "1-0E".
	WersjaSchemy string `xml:"wersjaSchemy,attr"`

	// Value is the element text content; must be "FA" for all FA(3) documents.
	Value string `xml:",chardata"`
}

// -------------------------------------------------------------------------
// Parties
// -------------------------------------------------------------------------

// Podmiot1 identifies the seller (vendor) who issues the invoice.
type Podmiot1 struct {
	// DaneIdentyfikacyjne contains the seller's NIP and name.
	DaneIdentyfikacyjne DaneIdentyfikacyjne `xml:"DaneIdentyfikacyjne"`

	// Adres is the seller's registered office address.
	Adres Adres `xml:"Adres"`

	// DaneKontaktowe holds optional contact details (email, phone).
	DaneKontaktowe *DaneKontaktowe `xml:"DaneKontaktowe,omitempty"`

	// StatusInfoPodatnika is an optional field indicating the seller's VAT taxpayer
	// status (e.g. "1" for active VAT taxpayer).
	StatusInfoPodatnika *string `xml:"StatusInfoPodatnika,omitempty"`
}

// Podmiot2 identifies the buyer (customer / recipient of the invoice).
type Podmiot2 struct {
	// DaneIdentyfikacyjne contains the buyer's identification data.
	// NIP is used for domestic Polish buyers; foreign buyers use NrVatUE or NrId.
	DaneIdentyfikacyjne DaneIdentyfikacyjnePodmiot2 `xml:"DaneIdentyfikacyjne"`

	// Adres is the buyer's address (optional for anonymous B2C invoices).
	Adres *Adres `xml:"Adres,omitempty"`

	// AdresKoresp is the buyer's correspondence address (optional).
	AdresKoresp *Adres `xml:"AdresKoresp,omitempty"`

	// DaneKontaktowe holds optional contact details for the buyer.
	DaneKontaktowe *DaneKontaktowe `xml:"DaneKontaktowe,omitempty"`

	// JST indicates whether this invoice relates to a JST (local government) subunit.
	// Use JSTNie (2) for regular invoices, JSTTak (1) for JST subunit invoices.
	// This field is required by the FA(3) schema.
	JST int `xml:"JST"`

	// GV indicates whether the buyer is a VAT group member.
	// Use GVNie (2) for regular invoices, GVTak (1) for VAT group member invoices.
	// This field is required by the FA(3) schema.
	GV int `xml:"GV"`
}

// JST flag values for Podmiot2.JST.
const (
	JSTTak = 1 // Invoice relates to a JST (local government) subunit.
	JSTNie = 2 // Invoice does not relate to a JST subunit (default for regular invoices).
)

// GV flag values for Podmiot2.GV.
const (
	GVTak = 1 // Buyer is a VAT group member.
	GVNie = 2 // Buyer is not a VAT group member (default for regular invoices).
)

// Podmiot3 represents an optional third party connected to the transaction,
// such as a factoring company (Faktor), ordering party, or delivery address holder.
type Podmiot3 struct {
	// DaneIdentyfikacyjne identifies the third party by NIP and name.
	DaneIdentyfikacyjne DaneIdentyfikacyjne `xml:"DaneIdentyfikacyjne"`

	// Adres is the third party's address (optional).
	Adres *Adres `xml:"Adres,omitempty"`

	// DaneKontaktowe holds optional contact details.
	DaneKontaktowe *DaneKontaktowe `xml:"DaneKontaktowe,omitempty"`

	// Rola describes the role of the third party.
	// Common values: "Faktor" (factoring company), "Odbiorca" (delivery recipient),
	// "Zamawiajacy" (ordering party), "Inny" (other — describe in RolaInna).
	Rola string `xml:"Rola"`

	// RolaInna provides a free-text description when Rola is "Inny".
	RolaInna *string `xml:"RolaInna,omitempty"`
}

// DaneIdentyfikacyjne holds taxpayer identity data for Podmiot1 and Podmiot3,
// where a Polish NIP is always present.
type DaneIdentyfikacyjne struct {
	// NIP is the Polish Tax Identification Number (Numer Identyfikacji Podatkowej),
	// exactly 10 digits, no separators.
	NIP string `xml:"NIP"`

	// Nazwa is the full legal name of the entity (max 256 characters).
	Nazwa string `xml:"Nazwa"`
}

// DaneIdentyfikacyjnePodmiot2 holds buyer identity data, supporting multiple
// identification schemes to accommodate domestic and foreign buyers.
// Exactly one identification field (NIP, NrVatUE, NrId/KodUrzedowy, or BrakID)
// must be populated.
type DaneIdentyfikacyjnePodmiot2 struct {
	// NIP is the Polish Tax Identification Number for domestic buyers.
	NIP *string `xml:"NIP,omitempty"`

	// NrVatUE is the EU VAT registration number (e.g. "DE123456789")
	// for intra-community transactions.
	NrVatUE *string `xml:"NrVatUE,omitempty"`

	// KodUrzedowy is the ISO 3166-1 alpha-2 country code used together with NrId
	// to identify foreign entities that are not EU VAT-registered.
	KodUrzedowy *string `xml:"KodUrzedowy,omitempty"`

	// NrId is the foreign tax identification number used when KodUrzedowy is set.
	NrId *string `xml:"NrId,omitempty"`

	// BrakID indicates that the buyer has no tax identification number available
	// (typically used for anonymous B2C sales). Set to "1" when applicable.
	BrakID *string `xml:"BrakID,omitempty"`

	// Nazwa is the full legal name of the buyer (max 256 characters).
	Nazwa string `xml:"Nazwa"`
}

// -------------------------------------------------------------------------
// Address types
// -------------------------------------------------------------------------

// Adres represents a postal address. The FA(3) 2025/06/25 schema uses a flat
// structure with free-form address lines instead of the earlier nested
// AdresPL / AdresZagraniczny sub-elements.
type Adres struct {
	// KodKraju is the ISO 3166-1 alpha-2 country code (e.g. "PL", "DE").
	KodKraju string `xml:"KodKraju"`

	// AdresL1 is the first address line (street, building number, etc.).
	AdresL1 string `xml:"AdresL1"`

	// AdresL2 is the optional second address line (city, postal code, etc.).
	AdresL2 *string `xml:"AdresL2,omitempty"`

	// GLN is the optional GS1 Global Location Number.
	GLN *string `xml:"GLN,omitempty"`
}

// -------------------------------------------------------------------------
// Contact and bank account
// -------------------------------------------------------------------------

// DaneKontaktowe holds optional contact information for an invoice party.
type DaneKontaktowe struct {
	// Email is the contact email address.
	Email *string `xml:"Email,omitempty"`

	// Telefon is the contact telephone number (including country code, optional).
	Telefon *string `xml:"Telefon,omitempty"`
}

// RachunekBankowy holds a single bank account entry used in payment terms.
// In the FA(3) 2025/06/25 schema bank accounts moved from Podmiot1 into the
// Platnosc (payment) section as RachunekBankowy elements.
type RachunekBankowy struct {
	// NrRB is the bank account number in IBAN format
	// (e.g. "PL61109010140000071219812874").
	NrRB string `xml:"NrRB"`

	// SWIFT is the bank's BIC/SWIFT code (optional; required for foreign accounts).
	SWIFT *string `xml:"SWIFT,omitempty"`

	// RachunekWlasnyBanku indicates that the account belongs to the bank itself
	// rather than to a factoring company. Omit when not applicable.
	RachunekWlasnyBanku *string `xml:"RachunekWlasnyBanku,omitempty"`

	// NazwaBanku is the name of the bank holding the account (optional).
	NazwaBanku *string `xml:"NazwaBanku,omitempty"`
}

// -------------------------------------------------------------------------
// Invoice data (Fa)
// -------------------------------------------------------------------------

// Fa contains the core body of the invoice: identification, dates, type,
// payment terms, line items, and the mandatory VAT summary.
// Fields are ordered to match the XSD sequence for the FA(3) v1-0E schema.
type Fa struct {
	// KodWaluty is the ISO 4217 currency code for the invoice
	// (e.g. "PLN", "EUR", "USD").
	KodWaluty string `xml:"KodWaluty"`

	// P_1 is the invoice issue date in YYYY-MM-DD format (e.g. "2026-03-22").
	P_1 string `xml:"P_1"`

	// P_1M is the place of invoice issue (optional free text, e.g. city name).
	P_1M *string `xml:"P_1M,omitempty"`

	// P_2 is the unique invoice number assigned by the seller
	// (e.g. "FV/2024/01/001"). Required.
	P_2 string `xml:"P_2"`

	// WZ lists dispatch / delivery note numbers associated with this invoice
	// (optional, multiple allowed).
	WZ []string `xml:"WZ,omitempty"`

	// P_6 is the date of the taxable supply (dostawa / usługa) in YYYY-MM-DD
	// format, when it differs from the issue date. Optional.
	P_6 *string `xml:"P_6,omitempty"`

	// VAT summary — net tax base per rate (P_13_x) and VAT amount per rate (P_14_x).
	// Populate only the fields that correspond to rates present on line items.
	// All amounts are decimal strings with up to two decimal places.
	// Fields must appear in the order prescribed by the XSD sequence.

	// P_13_1/P_14_1 — 23% standard rate.
	P_13_1  *string `xml:"P_13_1,omitempty"`
	P_14_1  *string `xml:"P_14_1,omitempty"`
	P_14_1W *string `xml:"P_14_1W,omitempty"`

	// P_13_2/P_14_2 — 8% reduced rate.
	P_13_2  *string `xml:"P_13_2,omitempty"`
	P_14_2  *string `xml:"P_14_2,omitempty"`
	P_14_2W *string `xml:"P_14_2W,omitempty"`

	// P_13_3/P_14_3 — 5% reduced rate.
	P_13_3  *string `xml:"P_13_3,omitempty"`
	P_14_3  *string `xml:"P_14_3,omitempty"`
	P_14_3W *string `xml:"P_14_3W,omitempty"`

	// P_13_4/P_14_4 — taxi flat-rate (ryczałt taksówkowy).
	P_13_4  *string `xml:"P_13_4,omitempty"`
	P_14_4  *string `xml:"P_14_4,omitempty"`
	P_14_4W *string `xml:"P_14_4W,omitempty"`

	// P_13_5/P_14_5 — special procedure (art. 130a–137, chapter 6a).
	P_13_5 *string `xml:"P_13_5,omitempty"`
	P_14_5 *string `xml:"P_14_5,omitempty"`

	// P_13_6_1 — 0% rate: domestic supplies (other than WDT/export).
	P_13_6_1 *string `xml:"P_13_6_1,omitempty"`
	// P_13_6_2 — 0% rate: intra-EU supply of goods (WDT).
	P_13_6_2 *string `xml:"P_13_6_2,omitempty"`
	// P_13_6_3 — 0% rate: export of goods.
	P_13_6_3 *string `xml:"P_13_6_3,omitempty"`

	// P_13_7 — net value of VAT-exempt supplies (zwolnione z VAT, art. 43/113).
	P_13_7 *string `xml:"P_13_7,omitempty"`
	// P_13_8 — net value of supplies outside Poland territory (poza terytorium kraju).
	P_13_8 *string `xml:"P_13_8,omitempty"`
	// P_13_9 — net value of intra-EU services (art. 100 ust. 1 pkt 4).
	P_13_9 *string `xml:"P_13_9,omitempty"`
	// P_13_10 — net value of domestic reverse-charge supplies (art. 17 ust. 1 pkt 7/8).
	P_13_10 *string `xml:"P_13_10,omitempty"`
	// P_13_11 — net value of margin-scheme supplies (art. 119 / art. 120).
	P_13_11 *string `xml:"P_13_11,omitempty"`

	// P_15 is the total gross amount of the invoice (required).
	// For PLN invoices this equals the sum of net values plus VAT.
	// For foreign-currency invoices this is the gross amount in that currency.
	P_15 string `xml:"P_15"`

	// Adnotacje holds mandatory annotation flags required by the schema.
	Adnotacje Adnotacje `xml:"Adnotacje"`

	// RodzajFaktury is the invoice type. Use the Rodzaj* constants.
	// Must appear after Adnotacje per the XSD sequence.
	RodzajFaktury string `xml:"RodzajFaktury"`

	// PrzyczynaKorekty is a free-text description of why a corrective invoice
	// is being issued. Required for KOR, KOR_ZAL, and KOR_ROZ types.
	PrzyczynaKorekty *string `xml:"PrzyczynaKorekty,omitempty"`

	// TypKorekty specifies the correction type for KOR invoices:
	// "1" in-period, "2" out-of-period, "3" bad-debt relief.
	TypKorekty *string `xml:"TypKorekty,omitempty"`

	// DaneFaKorygowanej references the invoice being corrected.
	// Required for corrective invoice types.
	DaneFaKorygowanej *DaneFaKorygowanej `xml:"DaneFaKorygowanej,omitempty"`

	// FaWiersz contains the invoice line items. At least one line item is required
	// for standard VAT invoices.
	FaWiersz []FaWiersz `xml:"FaWiersz"`

	// Platnosc holds payment terms, method, due date, and bank account (optional).
	Platnosc *Platnosc `xml:"Platnosc,omitempty"`

	// Zamowienie holds order-reference data for advance invoices (ZAL type).
	Zamowienie *Zamowienie `xml:"Zamowienie,omitempty"`
}

// -------------------------------------------------------------------------
// Line item
// -------------------------------------------------------------------------

// FaWiersz represents a single line item (position) on the invoice.
type FaWiersz struct {
	// NrWierszaFa is the sequential 1-based line number.
	NrWierszaFa uint32 `xml:"NrWierszaFa"`

	// UU_ID is an optional UUID identifying this specific line item
	// (useful for referencing lines in corrective invoices).
	UU_ID *string `xml:"UU_ID,omitempty"`

	// P_6A is an optional delivery or service date specific to this line,
	// in YYYY-MM-DD format, when it differs from the invoice-level P_2.
	P_6A *string `xml:"P_6A,omitempty"`

	// P_7 is the name or description of the goods or services supplied
	// (max 256 characters).
	P_7 string `xml:"P_7"`

	// P_8A is the unit of measurement (e.g. "szt" for pieces, "kg", "godz" for hours,
	// "usł" for service). Optional when quantity is not applicable.
	P_8A *string `xml:"P_8A,omitempty"`

	// P_8B is the quantity of goods or services, as a decimal string.
	P_8B *string `xml:"P_8B,omitempty"`

	// P_9A is the unit net price (price per unit excluding VAT), decimal string.
	// Required for net-based (netto) invoice calculation method.
	P_9A *string `xml:"P_9A,omitempty"`

	// P_9B is the unit gross price (price per unit including VAT), decimal string.
	// Required for gross-based (brutto) invoice calculation method.
	P_9B *string `xml:"P_9B,omitempty"`

	// P_10 is the discount or markup amount applied to this line, decimal string.
	// Positive value is a discount; the schema may use a dedicated sign convention.
	P_10 *string `xml:"P_10,omitempty"`

	// P_11 is the net value of this line item (quantity × unit net price, after
	// discounts), decimal string. Required for net-based invoices.
	P_11 *string `xml:"P_11,omitempty"`

	// P_11A is the gross value of this line item (quantity × unit gross price),
	// decimal string. Required for gross-based invoices.
	P_11A *string `xml:"P_11A,omitempty"`

	// P_11Vat is the VAT amount attributed to this line item, decimal string.
	// Used in gross-based invoices where P_11A and P_11 are both present.
	P_11Vat *string `xml:"P_11Vat,omitempty"`

	// P_12 is the VAT rate applicable to this line. Use the Stawka* constants.
	// For non-standard rates, provide the numeric string (e.g. "3").
	P_12 string `xml:"P_12"`

	// P_12_XII is the flat-rate percentage for agricultural flat-rate invoices
	// (faktura RR), decimal string. Used only when P_12 is a flat-rate value.
	P_12_XII *string `xml:"P_12_XII,omitempty"`

	// P_12_Zal_Vat is the VAT rate applicable to the advance payment on this line,
	// used on advance invoices (ZAL type).
	P_12_Zal_Vat *string `xml:"P_12_Zal_Vat,omitempty"`

	// GTU is the Goods and Services Classification code (optional).
	// Values: "GTU_01" through "GTU_13" as defined by the Polish VAT Act Annex 15.
	GTU *string `xml:"GTU,omitempty"`

	// PKWiU is the Polish Classification of Products and Services code (optional).
	PKWiU *string `xml:"PKWiU,omitempty"`

	// CN is the Combined Nomenclature (CN/TARIC) customs tariff code (optional).
	CN *string `xml:"CN,omitempty"`

	// PKOB is the Polish Building Classification (PKOB) code (optional).
	PKOB *string `xml:"PKOB,omitempty"`

	// StanPrzed is the pre-correction value for this line, decimal string.
	// Required on corrective invoice (KOR) line items to show the original value.
	StanPrzed *string `xml:"StanPrzed,omitempty"`
}

// -------------------------------------------------------------------------
// Annotations
// -------------------------------------------------------------------------

// Adnotacje holds mandatory annotation flags that declare special VAT treatments
// and procedures applicable to the invoice. All fields are required per the
// FA(3) v1-0E schema. Use the Tak / Nie constants for P_16..P_18A and P_23.
type Adnotacje struct {
	// P_16 declares that cash-accounting method applies (art. 19a ust. 5 pkt 1 / art. 21 ust. 1).
	// Use Tak ("1") or Nie ("2").
	P_16 string `xml:"P_16"`

	// P_17 declares that this is a self-billed invoice (samofakturowanie, art. 106d).
	// Use Tak ("1") or Nie ("2").
	P_17 string `xml:"P_17"`

	// P_18 declares that the invoice is subject to a VAT margin scheme (art. 119 / 120).
	// Use Tak ("1") or Nie ("2").
	P_18 string `xml:"P_18"`

	// P_18A declares that the mandatory split-payment mechanism applies (art. 106e ust. 1 pkt 18a).
	// Use Tak ("1") or Nie ("2").
	P_18A string `xml:"P_18A"`

	// Zwolnienie specifies VAT-exemption status. Required — always present.
	// For regular taxable invoices set P_19N to Tak ("1").
	Zwolnienie Zwolnienie `xml:"Zwolnienie"`

	// NoweSrodkiTransportu specifies new-means-of-transport status. Required.
	// For regular invoices set P_22N to Tak ("1").
	NoweSrodkiTransportu NoweSrodkiTransportu `xml:"NoweSrodkiTransportu"`

	// P_23 declares simplified triangular procedure (art. 135 ust. 1 pkt 4 lit. b/c).
	// Use Tak ("1") or Nie ("2"). Required.
	P_23 string `xml:"P_23"`

	// PMarzy specifies VAT margin-scheme indicator. Required.
	// For regular invoices set P_PMarzyN to Tak ("1").
	PMarzy PMarzy `xml:"PMarzy"`
}

// Zwolnienie declares VAT-exemption status (art. 43/113 of the Polish VAT Act).
// Exactly one variant must be set:
//   - P_19N = "1" for invoices with no VAT exemption (regular taxable supplies).
//   - P_19 = "1" together with one of P_19A / P_19B / P_19C for exempt supplies.
type Zwolnienie struct {
	// P_19N indicates no VAT exemption applies. Set to "1" for regular invoices.
	P_19N *string `xml:"P_19N,omitempty"`

	// P_19 indicates VAT exemption applies. Set to "1", then also set P_19A, P_19B, or P_19C.
	P_19 *string `xml:"P_19,omitempty"`

	// P_19A references Art. 113 of the VAT Act (subjective small-taxpayer exemption).
	P_19A *string `xml:"P_19A,omitempty"`

	// P_19B references Art. 43(1) of the VAT Act (objective exemption).
	P_19B *string `xml:"P_19B,omitempty"`

	// P_19C references any other statutory VAT-exemption provision.
	P_19C *string `xml:"P_19C,omitempty"`
}

// NoweSrodkiTransportu declares intra-EU supply of new means of transport status.
// Exactly one variant must be set:
//   - P_22N = "1" for invoices that do NOT involve new means of transport (regular).
//   - P_22 = "1" (with P_42_5 and NowySrodekTransportu details) for transport invoices.
type NoweSrodkiTransportu struct {
	// P_22N indicates no intra-EU supply of new means of transport. Set to "1" for regular invoices.
	P_22N *string `xml:"P_22N,omitempty"`
}

// PMarzy declares VAT margin-scheme status (art. 119 / art. 120 of the Polish VAT Act).
// Exactly one variant must be set:
//   - P_PMarzyN = "1" for invoices with no margin scheme (regular).
//   - P_PMarzy = "1" together with one of P_PMarzy_2 / P_PMarzy_3_1 / P_PMarzy_3_2 / P_PMarzy_3_3 for margin-scheme invoices.
type PMarzy struct {
	// P_PMarzyN indicates no margin scheme applies. Set to "1" for regular invoices.
	P_PMarzyN *string `xml:"P_PMarzyN,omitempty"`

	// P_PMarzy indicates a margin scheme applies. Set to "1", then also set one of the scheme variants.
	P_PMarzy *string `xml:"P_PMarzy,omitempty"`

	// P_PMarzy_2 indicates tourist-services margin scheme (art. 119).
	P_PMarzy_2 *string `xml:"P_PMarzy_2,omitempty"`

	// P_PMarzy_3_1 indicates used-goods margin scheme (art. 120).
	P_PMarzy_3_1 *string `xml:"P_PMarzy_3_1,omitempty"`

	// P_PMarzy_3_2 indicates works-of-art margin scheme (art. 120).
	P_PMarzy_3_2 *string `xml:"P_PMarzy_3_2,omitempty"`

	// P_PMarzy_3_3 indicates collectibles-and-antiques margin scheme (art. 120).
	P_PMarzy_3_3 *string `xml:"P_PMarzy_3_3,omitempty"`
}

// -------------------------------------------------------------------------
// Payment
// -------------------------------------------------------------------------

// Platnosc holds payment terms and method information for the invoice.
// Field order matches the FA(3) 2025/06/25 XSD sequence.
type Platnosc struct {
	// Zaplacono is "1" when the invoice has been fully paid at the time of
	// issuance (cash invoice or advance fully settled). Mutually exclusive
	// with ZnacznikZaplatyCzesciowej.
	Zaplacono *string `xml:"Zaplacono,omitempty"`

	// DataZaplaty is the date on which full payment was made (YYYY-MM-DD).
	// Required when Zaplacono is set.
	DataZaplaty *string `xml:"DataZaplaty,omitempty"`

	// ZnacznikZaplatyCzesciowej is "1" (partial) or "2" (final instalment).
	// Mutually exclusive with Zaplacono.
	ZnacznikZaplatyCzesciowej *string `xml:"ZnacznikZaplatyCzesciowej,omitempty"`

	// TerminPlatnosci is the payment due date (optional; repeat for instalment
	// schedules).
	TerminPlatnosci []TerminPlatnosci `xml:"TerminPlatnosci,omitempty"`

	// FormaPlatnosci is the payment method code. Use the Platnosc* constants.
	FormaPlatnosci *string `xml:"FormaPlatnosci,omitempty"`

	// RachunekBankowy lists the seller's bank accounts for this payment.
	RachunekBankowy []RachunekBankowy `xml:"RachunekBankowy,omitempty"`

	// RachunekBankowyFaktora lists the factoring company's bank accounts
	// (optional, used when the invoice is factored).
	RachunekBankowyFaktora []RachunekBankowy `xml:"RachunekBankowyFaktora,omitempty"`
}

// TerminPlatnosci holds a single payment due date for the invoice.
type TerminPlatnosci struct {
	// Termin is the payment due date in YYYY-MM-DD format.
	Termin string `xml:"Termin"`
}

// -------------------------------------------------------------------------
// Corrective invoice references
// -------------------------------------------------------------------------

// DaneFaKorygowanej references the original invoice being corrected.
// Required on KOR, KOR_ZAL, and KOR_ROZ invoices.
type DaneFaKorygowanej struct {
	// DataWystFaKorygowanej is the issue date of the original invoice (YYYY-MM-DD).
	DataWystFaKorygowanej string `xml:"DataWystFaKorygowanej"`

	// NrFaKorygowanej is the invoice number of the original invoice.
	NrFaKorygowanej string `xml:"NrFaKorygowanej"`

	// NrKSeF is the KSeF system reference number of the original invoice when it
	// was previously issued through KSeF.
	NrKSeF *string `xml:"NrKSeF,omitempty"`

	// NrKSeFN is used instead of NrKSeF when the original invoice was not issued
	// through KSeF (non-KSeF reference).
	NrKSeFN *string `xml:"NrKSeFN,omitempty"`
}

// -------------------------------------------------------------------------
// Accounting period
// -------------------------------------------------------------------------

// OkresRozliczeniowy defines an accounting or billing period covered by the invoice.
type OkresRozliczeniowy struct {
	// OkresOd is the start date of the period (YYYY-MM-DD).
	OkresOd string `xml:"OkresOd"`

	// OkresDo is the end date of the period (YYYY-MM-DD).
	OkresDo string `xml:"OkresDo"`
}

// -------------------------------------------------------------------------
// Advance invoice order reference
// -------------------------------------------------------------------------

// Zamowienie contains the underlying purchase order details for advance
// payment invoices (RodzajFaktury = ZAL). It shows what goods or services
// the advance payment relates to.
type Zamowienie struct {
	// ZamowienieWiersz lists the individual order lines the advance covers.
	ZamowienieWiersz []ZamowienieWiersz `xml:"ZamowienieWiersz"`

	// WartoscZamowienia is the total value of the order, decimal string.
	WartoscZamowienia string `xml:"WartoscZamowienia"`
}

// ZamowienieWiersz represents one line of the purchase order referenced by
// an advance payment invoice.
type ZamowienieWiersz struct {
	// NrWierszaZamowienia is the sequential 1-based line number.
	NrWierszaZamowienia uint32 `xml:"NrWierszaZamowienia"`

	// UU_IDZ is an optional unique identifier for this order line.
	UU_IDZ *string `xml:"UU_IDZ,omitempty"`

	// P_7Z is the name or description of the ordered goods or services.
	P_7Z string `xml:"P_7Z"`

	// P_8AZ is the unit of measurement for this order line (optional).
	P_8AZ *string `xml:"P_8AZ,omitempty"`

	// P_8BZ is the ordered quantity, decimal string (optional).
	P_8BZ *string `xml:"P_8BZ,omitempty"`

	// P_9AZ is the unit net price for this order line, decimal string (optional).
	P_9AZ *string `xml:"P_9AZ,omitempty"`

	// P_11NetZ is the net value of this order line, decimal string (optional).
	P_11NetZ *string `xml:"P_11NetZ,omitempty"`

	// P_12Z is the applicable VAT rate for this order line. Use the Stawka* constants.
	P_12Z string `xml:"P_12Z"`
}

// -------------------------------------------------------------------------
// Footer
// -------------------------------------------------------------------------

// Stopka holds optional footer data appended after the main invoice body.
type Stopka struct {
	// Informacje holds optional free-text notes visible on the invoice.
	Informacje *Informacje `xml:"Informacje,omitempty"`

	// Rejestry holds optional accounting registry classification entries.
	Rejestry *Rejestry `xml:"Rejestry,omitempty"`
}

// Informacje holds optional free-text annotations placed in the invoice footer.
type Informacje struct {
	// StopkaFaktury is general footer text (max 3500 characters), such as
	// payment instructions, legal notices, or contact details.
	StopkaFaktury *string `xml:"StopkaFaktury,omitempty"`
}

// Rejestry holds a list of accounting register classification entries.
type Rejestry struct {
	// Rejestr is the list of individual register entries.
	Rejestr []Rejestr `xml:"Rejestr,omitempty"`
}

// Rejestr represents a single accounting register entry attached to the invoice.
type Rejestr struct {
	// NazwaRejestru is the name of the accounting register or sub-ledger.
	NazwaRejestru string `xml:"NazwaRejestru"`

	// InfoRejestr is optional additional information about this register entry.
	InfoRejestr *string `xml:"InfoRejestr,omitempty"`
}
