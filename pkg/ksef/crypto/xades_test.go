package crypto

import (
	"bytes"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"strings"
	"testing"
)

// ── GenerateTestCertificate ───────────────────────────────────────────────────

func TestGenerateTestCertificate_ReturnsPEMBlocks(t *testing.T) {
	certPEM, keyPEM, err := GenerateTestCertificate("5265877635")
	if err != nil {
		t.Fatalf("GenerateTestCertificate: %v", err)
	}
	if certPEM == nil {
		t.Fatal("certPEM is nil")
	}
	if keyPEM == nil {
		t.Fatal("keyPEM is nil")
	}

	certBlock, _ := pem.Decode(certPEM)
	if certBlock == nil || certBlock.Type != "CERTIFICATE" {
		t.Fatalf("certPEM: expected CERTIFICATE block, got %v", certBlock)
	}

	keyBlock, _ := pem.Decode(keyPEM)
	if keyBlock == nil || keyBlock.Type != "RSA PRIVATE KEY" {
		t.Fatalf("keyPEM: expected RSA PRIVATE KEY block, got %v", keyBlock)
	}
}

func TestGenerateTestCertificate_SubjectContainsNIP(t *testing.T) {
	const nip = "5265877635"
	certPEM, _, err := GenerateTestCertificate(nip)
	if err != nil {
		t.Fatalf("GenerateTestCertificate: %v", err)
	}

	block, _ := pem.Decode(certPEM)
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("parse certificate: %v", err)
	}

	if cert.Subject.CommonName != nip {
		t.Errorf("CommonName = %q, want %q", cert.Subject.CommonName, nip)
	}
	if cert.Subject.SerialNumber != "NIP:"+nip {
		t.Errorf("SerialNumber = %q, want %q", cert.Subject.SerialNumber, "NIP:"+nip)
	}
}

func TestGenerateTestCertificate_RSAKeyPairConsistent(t *testing.T) {
	certPEM, keyPEM, err := GenerateTestCertificate("1234567890")
	if err != nil {
		t.Fatalf("GenerateTestCertificate: %v", err)
	}

	certBlock, _ := pem.Decode(certPEM)
	cert, _ := x509.ParseCertificate(certBlock.Bytes)

	keyBlock, _ := pem.Decode(keyPEM)
	privateKey, err := x509.ParsePKCS1PrivateKey(keyBlock.Bytes)
	if err != nil {
		t.Fatalf("parse private key: %v", err)
	}

	// Verify the public key in the cert matches the private key.
	certPub, ok := cert.PublicKey.(*rsa.PublicKey)
	if !ok {
		t.Fatal("certificate public key is not RSA")
	}
	if certPub.N.Cmp(privateKey.PublicKey.N) != 0 {
		t.Error("certificate public key does not match private key")
	}
}

// ── SignXML ───────────────────────────────────────────────────────────────────

// testFixture builds a cert+key pair once and reuses them across subtests.
type testFixture struct {
	cert       *x509.Certificate
	privateKey *rsa.PrivateKey
}

func newTestFixture(t *testing.T) *testFixture {
	t.Helper()
	certPEM, keyPEM, err := GenerateTestCertificate("5265877635")
	if err != nil {
		t.Fatalf("GenerateTestCertificate: %v", err)
	}
	certBlock, _ := pem.Decode(certPEM)
	cert, _ := x509.ParseCertificate(certBlock.Bytes)
	keyBlock, _ := pem.Decode(keyPEM)
	privateKey, _ := x509.ParsePKCS1PrivateKey(keyBlock.Bytes)
	return &testFixture{cert: cert, privateKey: privateKey}
}

// sampleDoc builds a canonical-form AuthTokenRequest (no XML declaration,
// no extra whitespace) matching the format expected by SignXML.
func sampleDoc(challenge, nip string) []byte {
	return []byte(
		`<AuthTokenRequest xmlns="http://ksef.mf.gov.pl/auth/token/2.0"` +
			` xmlns:xsd="http://www.w3.org/2001/XMLSchema"` +
			` xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">` +
			`<Challenge>` + challenge + `</Challenge>` +
			`<ContextIdentifier><Nip>` + nip + `</Nip></ContextIdentifier>` +
			`<SubjectIdentifierType>certificateSubject</SubjectIdentifierType>` +
			`</AuthTokenRequest>`,
	)
}

func TestSignXML_ReturnsXMLDeclaration(t *testing.T) {
	f := newTestFixture(t)
	doc := sampleDoc("20250625-CR-TESTCHALLENGE-46", "5265877635")

	signed, err := SignXML(doc, f.cert, f.privateKey)
	if err != nil {
		t.Fatalf("SignXML: %v", err)
	}
	if !bytes.HasPrefix(signed, []byte(`<?xml version="1.0" encoding="utf-8"?>`)) {
		t.Errorf("signed doc does not start with XML declaration, got: %.60s", signed)
	}
}

func TestSignXML_ContainsExpectedStructure(t *testing.T) {
	f := newTestFixture(t)
	doc := sampleDoc("CHALLENGE-001", "5265877635")

	signed, err := SignXML(doc, f.cert, f.privateKey)
	if err != nil {
		t.Fatalf("SignXML: %v", err)
	}
	s := string(signed)

	want := []string{
		`<ds:Signature xmlns:ds="http://www.w3.org/2000/09/xmldsig#"`,
		`<ds:SignedInfo`,
		`<ds:CanonicalizationMethod Algorithm="http://www.w3.org/TR/2001/REC-xml-c14n-20010315"`,
		`<ds:SignatureMethod Algorithm="http://www.w3.org/2001/04/xmldsig-more#rsa-sha256"`,
		`<ds:Reference Id="Reference-0" URI="">`,
		`Algorithm="http://www.w3.org/2000/09/xmldsig#enveloped-signature"`,
		`<ds:DigestValue>`,
		`<ds:SignatureValue`,
		`<ds:X509Certificate>`,
		`<xades:QualifyingProperties`,
		`<xades:SignedProperties`,
		`<xades:SigningTime>`,
		`<xades:SigningCertificateV2>`,
		`</AuthTokenRequest>`,
	}
	for _, fragment := range want {
		if !strings.Contains(s, fragment) {
			t.Errorf("signed doc missing expected fragment: %s", fragment)
		}
	}
}

func TestSignXML_OriginalContentPreserved(t *testing.T) {
	const challenge = "20250625-CR-20F5EE4000-DA48AE4124-46"
	const nip = "5265877635"
	f := newTestFixture(t)
	doc := sampleDoc(challenge, nip)

	signed, err := SignXML(doc, f.cert, f.privateKey)
	if err != nil {
		t.Fatalf("SignXML: %v", err)
	}
	s := string(signed)

	if !strings.Contains(s, "<Challenge>"+challenge+"</Challenge>") {
		t.Error("Challenge element not present in signed document")
	}
	if !strings.Contains(s, "<Nip>"+nip+"</Nip>") {
		t.Error("Nip element not present in signed document")
	}
	if !strings.Contains(s, "<SubjectIdentifierType>certificateSubject</SubjectIdentifierType>") {
		t.Error("SubjectIdentifierType not present in signed document")
	}
}

func TestSignXML_SignatureVerifies(t *testing.T) {
	f := newTestFixture(t)
	doc := sampleDoc("20250625-CR-VERIFY-TEST", "5265877635")

	signed, err := SignXML(doc, f.cert, f.privateKey)
	if err != nil {
		t.Fatalf("SignXML: %v", err)
	}

	// Extract the ds:SignedInfo canonical string and the ds:SignatureValue from
	// the signed document, then verify the RSA-SHA256 signature manually.
	signedInfoCanon := extractElement(t, signed, "<ds:SignedInfo", "</ds:SignedInfo>")
	sigValueRaw := extractElement(t, signed, "<ds:SignatureValue", "</ds:SignatureValue>")

	// Strip the opening tag (which contains the Id attribute) and closing tag.
	openEnd := strings.Index(sigValueRaw, ">") + 1
	closeStart := strings.LastIndex(sigValueRaw, "<")
	sigValueB64 := strings.TrimSpace(sigValueRaw[openEnd:closeStart])

	sigBytes, err := base64.StdEncoding.DecodeString(sigValueB64)
	if err != nil {
		t.Fatalf("base64 decode SignatureValue: %v", err)
	}

	h := sha256.New()
	h.Write([]byte(signedInfoCanon))
	digest := h.Sum(nil)

	pub, ok := f.cert.PublicKey.(*rsa.PublicKey)
	if !ok {
		t.Fatal("cert public key is not RSA")
	}
	if err := rsa.VerifyPKCS1v15(pub, crypto.SHA256, digest, sigBytes); err != nil {
		t.Errorf("signature verification failed: %v", err)
	}
}

func TestSignXML_ErrorOnMissingClosingTag(t *testing.T) {
	f := newTestFixture(t)
	malformed := []byte(`<AuthTokenRequest><Challenge>x</Challenge>`) // no closing tag
	_, err := SignXML(malformed, f.cert, f.privateKey)
	if err == nil {
		t.Fatal("expected error for missing closing tag, got nil")
	}
}

// ── helpers ───────────────────────────────────────────────────────────────────

// extractElement returns the substring of haystack from the first occurrence
// of openTag (inclusive) to the end of closeTag (inclusive).
func extractElement(t *testing.T, haystack []byte, openTag, closeTag string) string {
	t.Helper()
	s := string(haystack)
	start := strings.Index(s, openTag)
	if start < 0 {
		t.Fatalf("extractElement: openTag %q not found", openTag)
	}
	end := strings.Index(s[start:], closeTag)
	if end < 0 {
		t.Fatalf("extractElement: closeTag %q not found after openTag", closeTag)
	}
	return s[start : start+end+len(closeTag)]
}
