// Package crypto provides cryptographic helpers for the KSeF SDK.
package crypto

import (
	"bytes"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"math/big"
	"time"
)

// XAdES-BES algorithm identifiers and XML namespace URIs used in KSeF
// authentication requests.
const (
	AlgC14N       = "http://www.w3.org/TR/2001/REC-xml-c14n-20010315"
	AlgRSASHA256  = "http://www.w3.org/2001/04/xmldsig-more#rsa-sha256"
	AlgSHA256     = "http://www.w3.org/2001/04/xmlenc#sha256"
	AlgEnveloped  = "http://www.w3.org/2000/09/xmldsig#enveloped-signature"
	NSXMLDSig     = "http://www.w3.org/2000/09/xmldsig#"
	NSXAdES       = "http://uri.etsi.org/01903/v1.3.2#"
	NSKSeFAuth    = "http://ksef.mf.gov.pl/auth/token/2.0"
	XAdESPropsRef = "http://uri.etsi.org/01903#SignedProperties"
)

// GenerateTestCertificate creates a self-signed RSA-2048 certificate suitable
// for the KSeF test environment (where certificate chain verification can be
// disabled via the verifyCertificateChain query parameter).
//
// The Subject CommonName and SerialNumber are set from nip so KSeF can match
// the certificate subject against the authenticating entity.
// Returns PEM-encoded certificate and private key.
func GenerateTestCertificate(nip string) (certPEM, keyPEM []byte, err error) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, fmt.Errorf("generate RSA key: %w", err)
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, nil, fmt.Errorf("generate serial: %w", err)
	}

	template := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			Country:      []string{"PL"},
			Organization: []string{"KSeF Test"},
			CommonName:   nip,
			// SerialNumber in Polish qualified certificates conventionally
			// holds "NIP:<nip>" so KSeF can extract the tax identifier.
			SerialNumber: "NIP:" + nip,
		},
		NotBefore:             time.Now().Add(-24 * time.Hour),
		NotAfter:              time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &privateKey.PublicKey, privateKey)
	if err != nil {
		return nil, nil, fmt.Errorf("create certificate: %w", err)
	}

	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyPEM = pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(privateKey),
	})
	return certPEM, keyPEM, nil
}

// SignXML generates an enveloped XAdES-BES signature for a KSeF
// AuthTokenRequest and returns the complete signed document.
//
// doc must be the canonical form of the AuthTokenRequest element — no XML
// declaration, compact (no extra whitespace), namespace declarations in
// Canonical XML 1.0 order:
//
//	xmlns="http://ksef.mf.gov.pl/auth/token/2.0" xmlns:xsd="..." xmlns:xsi="..."
//
// The returned bytes are a UTF-8 XML document with an XML declaration and
// an embedded ds:Signature element inside AuthTokenRequest.
func SignXML(doc []byte, cert *x509.Certificate, key *rsa.PrivateKey) ([]byte, error) {
	id := newSignatureID()

	signedPropsID := "SignedProperties-" + id
	sigValueID := "SignatureValue-" + id
	keyInfoID := "KeyInfo-" + id
	objectID := "Object-" + id
	signatureID := "Signature-" + id

	// Step 1: Digest the pre-signature document (already in canonical form).
	docDigest := sha256Sum(doc)

	// Step 2: Build the canonical form of xades:SignedProperties (which
	// includes all ancestor namespace bindings as required by C14N 1.0 when
	// processing a subtree), then digest it.
	signingTime := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	certDigest := sha256Sum(cert.Raw)
	signedPropsCanon := buildSignedPropsCanon(
		signedPropsID, signingTime,
		base64.StdEncoding.EncodeToString(certDigest),
	)
	signedPropsDigest := sha256Sum([]byte(signedPropsCanon))

	// Step 3: Build canonical ds:SignedInfo (with ancestor namespace bindings)
	// and compute the RSA-SHA256 signature over it.
	signedInfoCanon := buildSignedInfoCanon(
		base64.StdEncoding.EncodeToString(docDigest),
		signedPropsID,
		base64.StdEncoding.EncodeToString(signedPropsDigest),
	)
	h := sha256.New()
	h.Write([]byte(signedInfoCanon))
	sigBytes, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, h.Sum(nil))
	if err != nil {
		return nil, fmt.Errorf("xades: sign: %w", err)
	}
	sigValueB64 := base64.StdEncoding.EncodeToString(sigBytes)
	certB64 := base64.StdEncoding.EncodeToString(cert.Raw)

	// Step 4: Assemble the final document by injecting ds:Signature before
	// the closing </AuthTokenRequest> tag.
	sig := buildSignatureElement(
		signatureID, signedInfoCanon, sigValueID, sigValueB64,
		keyInfoID, certB64, objectID, signedPropsID, signedPropsCanon,
	)

	const closingTag = "</AuthTokenRequest>"
	idx := bytes.LastIndex(doc, []byte(closingTag))
	if idx < 0 {
		return nil, fmt.Errorf("xades: document missing </AuthTokenRequest> closing tag")
	}

	var out bytes.Buffer
	out.WriteString(`<?xml version="1.0" encoding="utf-8"?>`)
	out.Write(doc[:idx])
	out.WriteString(sig)
	out.WriteString(closingTag)
	return out.Bytes(), nil
}

// ── private builders ──────────────────────────────────────────────────────────

// buildSignedPropsCanon returns the Canonical XML 1.0 form of
// xades:SignedProperties for a KSeF AuthTokenRequest context.
//
// The element carries all five namespace bindings that would be in scope from
// its ancestor elements (AuthTokenRequest, ds:Signature, xades:QualifyingProperties)
// so that computing SHA-256 of this string equals what a conformant verifier
// would compute when canonicalising the subtree in document context.
//
// Namespace sort order (C14N 1.0: default first, then alphabetical by prefix):
//
//	xmlns="" xmlns:ds xmlns:xades xmlns:xsd xmlns:xsi
func buildSignedPropsCanon(id, signingTime, certDigestB64 string) string {
	return `<xades:SignedProperties` +
		` xmlns="` + NSKSeFAuth + `"` +
		` xmlns:ds="` + NSXMLDSig + `"` +
		` xmlns:xades="` + NSXAdES + `"` +
		` xmlns:xsd="http://www.w3.org/2001/XMLSchema"` +
		` xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance"` +
		` Id="` + id + `">` +
		`<xades:SignedSignatureProperties>` +
		`<xades:SigningTime>` + signingTime + `</xades:SigningTime>` +
		`<xades:SigningCertificateV2>` +
		`<xades:Cert>` +
		`<xades:CertDigest>` +
		`<ds:DigestMethod Algorithm="` + AlgSHA256 + `"></ds:DigestMethod>` +
		`<ds:DigestValue>` + certDigestB64 + `</ds:DigestValue>` +
		`</xades:CertDigest>` +
		`</xades:Cert>` +
		`</xades:SigningCertificateV2>` +
		`</xades:SignedSignatureProperties>` +
		`</xades:SignedProperties>`
}

// buildSignedInfoCanon returns the Canonical XML 1.0 form of ds:SignedInfo for
// a KSeF AuthTokenRequest context.
//
// The element carries the four namespace bindings in scope from its ancestor
// elements (AuthTokenRequest, ds:Signature) — excluding xmlns:xades which is
// only introduced inside ds:Object, a sibling of ds:SignedInfo.
//
// Namespace sort order: xmlns="" xmlns:ds xmlns:xsd xmlns:xsi
//
// Reference[0] covers the whole document (URI="") with the enveloped-signature
// transform (removes ds:Signature) followed by C14N.
// Reference[1] covers xades:SignedProperties by ID.
func buildSignedInfoCanon(docDigestB64, signedPropsID, signedPropsDigestB64 string) string {
	return `<ds:SignedInfo` +
		` xmlns="` + NSKSeFAuth + `"` +
		` xmlns:ds="` + NSXMLDSig + `"` +
		` xmlns:xsd="http://www.w3.org/2001/XMLSchema"` +
		` xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">` +
		`<ds:CanonicalizationMethod Algorithm="` + AlgC14N + `"></ds:CanonicalizationMethod>` +
		`<ds:SignatureMethod Algorithm="` + AlgRSASHA256 + `"></ds:SignatureMethod>` +
		// Reference 0: whole document minus Signature (enveloped transform + C14N).
		`<ds:Reference Id="Reference-0" URI="">` +
		`<ds:Transforms>` +
		`<ds:Transform Algorithm="` + AlgEnveloped + `"></ds:Transform>` +
		`<ds:Transform Algorithm="` + AlgC14N + `"></ds:Transform>` +
		`</ds:Transforms>` +
		`<ds:DigestMethod Algorithm="` + AlgSHA256 + `"></ds:DigestMethod>` +
		`<ds:DigestValue>` + docDigestB64 + `</ds:DigestValue>` +
		`</ds:Reference>` +
		// Reference 1: xades:SignedProperties element by ID.
		`<ds:Reference Type="` + XAdESPropsRef + `" URI="#` + signedPropsID + `">` +
		`<ds:Transforms>` +
		`<ds:Transform Algorithm="` + AlgC14N + `"></ds:Transform>` +
		`</ds:Transforms>` +
		`<ds:DigestMethod Algorithm="` + AlgSHA256 + `"></ds:DigestMethod>` +
		`<ds:DigestValue>` + signedPropsDigestB64 + `</ds:DigestValue>` +
		`</ds:Reference>` +
		`</ds:SignedInfo>`
}

// buildSignatureElement assembles the complete ds:Signature element. The
// signedInfoCanon string is embedded verbatim so that it is byte-identical to
// what was signed; the xades:SignedProperties canonical form is likewise
// embedded verbatim for consistent digest verification.
func buildSignatureElement(
	signatureID, signedInfoCanon, sigValueID, sigValueB64,
	keyInfoID, certB64, objectID, signedPropsID, signedPropsCanon string,
) string {
	return `<ds:Signature xmlns:ds="` + NSXMLDSig + `" Id="` + signatureID + `">` +
		signedInfoCanon +
		`<ds:SignatureValue Id="` + sigValueID + `">` + sigValueB64 + `</ds:SignatureValue>` +
		`<ds:KeyInfo Id="` + keyInfoID + `">` +
		`<ds:X509Data>` +
		`<ds:X509Certificate>` + certB64 + `</ds:X509Certificate>` +
		`</ds:X509Data>` +
		`</ds:KeyInfo>` +
		`<ds:Object Id="` + objectID + `">` +
		`<xades:QualifyingProperties xmlns:xades="` + NSXAdES + `" Target="#` + signatureID + `">` +
		signedPropsCanon +
		`</xades:QualifyingProperties>` +
		`</ds:Object>` +
		`</ds:Signature>`
}

// sha256Sum returns the SHA-256 digest of data as a byte slice.
func sha256Sum(data []byte) []byte {
	h := sha256.Sum256(data)
	return h[:]
}

// newSignatureID generates a random 8-hex-digit string used to make
// signature element IDs unique (e.g. "Signature-A1B2C3D4").
func newSignatureID() string {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand failure is unrecoverable in a library context.
		panic("xades: generate signature ID: " + err.Error())
	}
	return fmt.Sprintf("%08X", uint32(b[0])<<24|uint32(b[1])<<16|uint32(b[2])<<8|uint32(b[3]))
}
