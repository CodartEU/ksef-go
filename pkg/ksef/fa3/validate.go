package fa3

import (
	"fmt"
	"strings"
	"unicode"
)

// Validate performs basic structural validation on a Faktura.
// It checks required fields, NIP format, and line item integrity.
// It does NOT perform full XSD schema validation against the FA(3) schema.
func Validate(f *Faktura) error {
	var errs []string

	// Seller
	if f.Podmiot1.DaneIdentyfikacyjne.NIP == "" {
		errs = append(errs, "seller NIP is required")
	} else if err := validateNIP(f.Podmiot1.DaneIdentyfikacyjne.NIP); err != nil {
		errs = append(errs, "seller NIP: "+err.Error())
	}
	if f.Podmiot1.DaneIdentyfikacyjne.Nazwa == "" {
		errs = append(errs, "seller name (Podmiot1.Nazwa) is required")
	}

	// Invoice identification
	if f.Fa.P_1 == "" {
		errs = append(errs, "invoice issue date (P_1) is required")
	}
	if f.Fa.P_2 == "" {
		errs = append(errs, "invoice number (P_2) is required")
	}

	// Gross total
	if f.Fa.P_15 == "" {
		errs = append(errs, "total gross amount (P_15) is required")
	}

	// Line items
	if len(f.Fa.FaWiersz) == 0 {
		errs = append(errs, "at least one line item (FaWiersz) is required")
	}
	for i, w := range f.Fa.FaWiersz {
		prefix := fmt.Sprintf("line item %d", i+1)
		if w.P_7 == "" {
			errs = append(errs, prefix+": description (P_7) is required")
		}
		if w.P_12 == "" {
			errs = append(errs, prefix+": VAT rate (P_12) is required")
		}
		if w.P_11 == nil && w.P_11A == nil {
			errs = append(errs, prefix+": net value (P_11) or gross value (P_11A) is required")
		}
	}

	// Buyer (optional, but if present must be valid)
	if f.Podmiot2 != nil {
		if f.Podmiot2.DaneIdentyfikacyjne.Nazwa == "" {
			errs = append(errs, "buyer name (Podmiot2.Nazwa) is required when Podmiot2 is set")
		}
		if nip := f.Podmiot2.DaneIdentyfikacyjne.NIP; nip != nil && *nip != "" {
			if err := validateNIP(*nip); err != nil {
				errs = append(errs, "buyer NIP: "+err.Error())
			}
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("validation failed: %s", strings.Join(errs, "; "))
	}
	return nil
}

// validateNIP checks that nip consists of exactly 10 decimal digits.
func validateNIP(nip string) error {
	if len(nip) != 10 {
		return fmt.Errorf("must be exactly 10 digits, got %d characters", len(nip))
	}
	for _, c := range nip {
		if !unicode.IsDigit(c) {
			return fmt.Errorf("must contain only digits")
		}
	}
	return nil
}
