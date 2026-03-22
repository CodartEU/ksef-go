package fa3

import (
	"bytes"
	"encoding/xml"
	"fmt"
)

const xmlDeclaration = `<?xml version="1.0" encoding="UTF-8"?>`

// MarshalXML serialises a Faktura to FA(3) compliant XML. The output includes
// the XML declaration on the first line and the default namespace declaration
// on the root <Faktura> element, as required by the FA(3) schema.
func MarshalXML(f *Faktura) ([]byte, error) {
	out, err := xml.MarshalIndent(f, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal xml: %w", err)
	}
	var buf bytes.Buffer
	buf.WriteString(xmlDeclaration)
	buf.WriteByte('\n')
	buf.Write(out)
	return buf.Bytes(), nil
}
