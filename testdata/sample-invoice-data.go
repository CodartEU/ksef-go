//go:build ignore

// sample-invoice-data.go is a standalone reference program that demonstrates
// how to build a valid FA(3) invoice using the fa3 builder. It is not compiled
// as part of the module (the //go:build ignore tag above excludes it). It can
// be run directly to print sample invoice XML:
//
//	KSEF_TEST_NIP=1234567890 go run testdata/sample-invoice-data.go
//
// The same helper logic is used in pkg/ksef/integration_test.go.
package main

import (
	"fmt"
	"os"
	"time"

	"github.com/CodartEU/ksef-go/pkg/ksef/fa3"
)

// SampleInvoice builds a minimal FA(3) VAT invoice for the given seller NIP.
// The buyer NIP is a fixed test value; replace it with a real NIP when
// testing against an environment that validates buyer identity.
func SampleInvoice(sellerNIP string) (*fa3.Faktura, error) {
	streetSeller := "ul. Testowa"
	streetBuyer := "ul. Przykładowa"

	sellerAddr := fa3.Adres{
		AdresPL: &fa3.AdresPL{
			KodKraju:    "PL",
			Ulica:       &streetSeller,
			NrDomu:      "1",
			Miejscowosc: "Warszawa",
			KodPocztowy: "00-001",
		},
	}
	buyerAddr := fa3.Adres{
		AdresPL: &fa3.AdresPL{
			KodKraju:    "PL",
			Ulica:       &streetBuyer,
			NrDomu:      "10",
			Miejscowosc: "Kraków",
			KodPocztowy: "30-001",
		},
	}

	now := time.Now()
	// Invoice number must be unique per seller per day.
	invoiceNum := fmt.Sprintf("TEST/%04d/%02d/%02d/%d",
		now.Year(), int(now.Month()), now.Day(), now.UnixNano()%100000)

	return fa3.NewInvoiceBuilder().
		SetSeller("Firma Testowa Sp. z o.o.", sellerNIP, sellerAddr).
		SetBuyer("Kontrahent Testowy Sp. z o.o.", "9999999999", buyerAddr).
		SetInvoiceNumber(invoiceNum).
		SetDates(now, now).
		SetPayment(
			fa3.PlatnoscPrzelew,
			now.Add(14*24*time.Hour),
			"PL61109010140000071219812874",
		).
		AddItem(fa3.LineItem{
			Description:  "Usługa doradcza",
			Unit:         "usł",
			Quantity:     "1",
			UnitNetPrice: "1000.00",
			NetValue:     "1000.00",
			VATRate:      fa3.Stawka23,
		}).
		Build()
}

func main() {
	nip := os.Getenv("KSEF_TEST_NIP")
	if nip == "" {
		fmt.Fprintln(os.Stderr, "error: KSEF_TEST_NIP is not set")
		os.Exit(1)
	}

	f, err := SampleInvoice(nip)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error building invoice: %v\n", err)
		os.Exit(1)
	}

	xmlBytes, err := fa3.MarshalXML(f)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error marshaling invoice: %v\n", err)
		os.Exit(1)
	}

	os.Stdout.Write(xmlBytes)
}
