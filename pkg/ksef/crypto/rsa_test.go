package crypto

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"strings"
	"testing"
)

// generateTestKeyPair creates a fresh RSA key pair for testing.
func generateTestKeyPair(t *testing.T, bits int) (*rsa.PrivateKey, *rsa.PublicKey) {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, bits)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}
	return priv, &priv.PublicKey
}

// marshalPKIXPublicKey encodes a public key as a PKIX "PUBLIC KEY" PEM block.
func marshalPKIXPublicKey(t *testing.T, pub *rsa.PublicKey) []byte {
	t.Helper()
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		t.Fatalf("marshal PKIX public key: %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})
}

// marshalPKCS1PublicKey encodes a public key as a PKCS#1 "RSA PUBLIC KEY" PEM block.
func marshalPKCS1PublicKey(pub *rsa.PublicKey) []byte {
	return pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PUBLIC KEY",
		Bytes: x509.MarshalPKCS1PublicKey(pub),
	})
}

// ── LoadPublicKeyFromPEM ───────────────────────────────────────────────────────

func TestLoadPublicKeyFromPEM_PKIX(t *testing.T) {
	_, pub := generateTestKeyPair(t, 2048)
	pemData := marshalPKIXPublicKey(t, pub)

	got, err := LoadPublicKeyFromPEM(pemData)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.N.Cmp(pub.N) != 0 || got.E != pub.E {
		t.Error("loaded public key does not match original")
	}
}

func TestLoadPublicKeyFromPEM_PKCS1(t *testing.T) {
	_, pub := generateTestKeyPair(t, 2048)
	pemData := marshalPKCS1PublicKey(pub)

	got, err := LoadPublicKeyFromPEM(pemData)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.N.Cmp(pub.N) != 0 || got.E != pub.E {
		t.Error("loaded public key does not match original")
	}
}

func TestLoadPublicKeyFromPEM_InvalidInput(t *testing.T) {
	tests := []struct {
		name    string
		pemData []byte
	}{
		{"empty", []byte{}},
		{"not PEM", []byte("this is not PEM data")},
		{"wrong block type", pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: []byte("junk")})},
		{"garbage PKIX DER", pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: []byte("not valid DER")})},
		{"garbage PKCS1 DER", pem.EncodeToMemory(&pem.Block{Type: "RSA PUBLIC KEY", Bytes: []byte("not valid DER")})},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := LoadPublicKeyFromPEM(tt.pemData)
			if err == nil {
				t.Error("expected error, got nil")
			}
		})
	}
}

func TestLoadPublicKeyFromPEM_NonRSAKey(t *testing.T) {
	// A PKIX block that parses fine but is not RSA (we encode junk ECDSA-like
	// DER that actually fails to parse — the important path is the type assertion
	// failure, but x509.ParsePKIXPublicKey will error first for truly invalid DER).
	// Instead, build a proper PKIX encoding of an EC key.
	import_note := "This test exercises the non-RSA key type-assertion branch."
	_ = import_note

	// We cannot easily import ecdsa here without adding imports — use reflection
	// shortcut: craft a PKIX "PUBLIC KEY" PEM whose parsed key is not *rsa.PublicKey.
	// The simplest approach is generating an ECDSA key via the x509 package.
	// crypto/ecdsa is part of stdlib and available in tests; we use it inline.
	ecKey := generateECPublicKey(t)
	der, err := x509.MarshalPKIXPublicKey(ecKey)
	if err != nil {
		t.Skipf("cannot marshal EC key: %v", err)
	}
	pemData := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})

	_, err = LoadPublicKeyFromPEM(pemData)
	if err == nil {
		t.Error("expected error for non-RSA public key, got nil")
	}
}

// ── RSA-OAEP key wrapping ─────────────────────────────────────────────────────

func TestEncryptDecryptAESKey(t *testing.T) {
	priv, pub := generateTestKeyPair(t, 2048)
	aesKey, _ := GenerateAESKey()

	ct, err := encryptAESKey(aesKey, pub)
	if err != nil {
		t.Fatalf("encrypt AES key: %v", err)
	}

	got, err := decryptAESKey(ct, priv)
	if err != nil {
		t.Fatalf("decrypt AES key: %v", err)
	}

	if !bytes.Equal(got, aesKey) {
		t.Error("decrypted AES key does not match original")
	}
}

func TestEncryptAESKey_RandomOutput(t *testing.T) {
	// RSA-OAEP is probabilistic; two encryptions of the same key differ.
	_, pub := generateTestKeyPair(t, 2048)
	aesKey, _ := GenerateAESKey()

	ct1, _ := encryptAESKey(aesKey, pub)
	ct2, _ := encryptAESKey(aesKey, pub)
	if bytes.Equal(ct1, ct2) {
		t.Error("two RSA-OAEP encryptions of the same key are identical — OAEP randomisation broken")
	}
}

func TestDecryptAESKey_WrongKey(t *testing.T) {
	_, pub := generateTestKeyPair(t, 2048)
	wrongPriv, _ := generateTestKeyPair(t, 2048)
	aesKey, _ := GenerateAESKey()

	ct, _ := encryptAESKey(aesKey, pub)

	_, err := decryptAESKey(ct, wrongPriv)
	if err == nil {
		t.Error("expected error when decrypting with wrong private key")
	}
}

func TestDecryptAESKey_TamperedCiphertext(t *testing.T) {
	priv, pub := generateTestKeyPair(t, 2048)
	aesKey, _ := GenerateAESKey()
	ct, _ := encryptAESKey(aesKey, pub)

	ct[0] ^= 0xFF

	_, err := decryptAESKey(ct, priv)
	if err == nil {
		t.Error("expected error for tampered ciphertext")
	}
}

// ── EncryptInvoice / DecryptInvoice ───────────────────────────────────────────

func TestEncryptDecryptInvoice_RoundTrip(t *testing.T) {
	priv, pub := generateTestKeyPair(t, 2048)

	invoices := []struct {
		name string
		xml  []byte
	}{
		{"minimal", []byte(`<Faktura/>`)},
		{"typical", []byte(`<?xml version="1.0" encoding="utf-8"?><Faktura xmlns="http://crd.gov.pl/wzor/2023/06/29/12648/"><Naglowek><KodFormularza>FA</KodFormularza><WersjaSchemy>1-0E</WersjaSchemy></Naglowek></Faktura>`)},
		{"large", []byte(strings.Repeat("<Pozycja>towar</Pozycja>", 2000))},
		{"binary-safe unicode", []byte("<Invoice>Złoty zł żółw 🧾</Invoice>")},
	}

	for _, tt := range invoices {
		t.Run(tt.name, func(t *testing.T) {
			encInvoice, encKey, err := EncryptInvoice(tt.xml, pub)
			if err != nil {
				t.Fatalf("EncryptInvoice: %v", err)
			}

			if len(encInvoice) == 0 {
				t.Error("encrypted invoice is empty")
			}
			if len(encKey) == 0 {
				t.Error("encrypted key is empty")
			}

			got, err := DecryptInvoice(encInvoice, encKey, priv)
			if err != nil {
				t.Fatalf("DecryptInvoice: %v", err)
			}

			if !bytes.Equal(got, tt.xml) {
				t.Errorf("round-trip mismatch:\ngot  %q\nwant %q", got, tt.xml)
			}
		})
	}
}

func TestEncryptInvoice_DifferentEachTime(t *testing.T) {
	_, pub := generateTestKeyPair(t, 2048)
	xml := []byte("<Faktura>test</Faktura>")

	ei1, ek1, _ := EncryptInvoice(xml, pub)
	ei2, ek2, _ := EncryptInvoice(xml, pub)

	if bytes.Equal(ei1, ei2) {
		t.Error("encrypted invoices are identical — IV randomisation broken")
	}
	if bytes.Equal(ek1, ek2) {
		t.Error("encrypted keys are identical — OAEP or AES key randomisation broken")
	}
}

func TestDecryptInvoice_WrongPrivateKey(t *testing.T) {
	_, pub := generateTestKeyPair(t, 2048)
	wrongPriv, _ := generateTestKeyPair(t, 2048)

	ei, ek, _ := EncryptInvoice([]byte("<Faktura/>"), pub)

	_, err := DecryptInvoice(ei, ek, wrongPriv)
	if err == nil {
		t.Error("expected error when decrypting with wrong private key")
	}
}

func TestDecryptInvoice_TamperedEncryptedKey(t *testing.T) {
	priv, pub := generateTestKeyPair(t, 2048)
	ei, ek, _ := EncryptInvoice([]byte("<Faktura/>"), pub)

	ek[0] ^= 0xFF

	_, err := DecryptInvoice(ei, ek, priv)
	if err == nil {
		t.Error("expected error for tampered encrypted key")
	}
}

func TestDecryptInvoice_TamperedEncryptedInvoice(t *testing.T) {
	priv, pub := generateTestKeyPair(t, 2048)
	ei, ek, _ := EncryptInvoice([]byte("<Faktura/>"), pub)

	// Tamper with the ciphertext body (after the IV).
	ei[len(ei)-1] ^= 0xFF

	_, err := DecryptInvoice(ei, ek, priv)
	if err == nil {
		t.Error("expected error for tampered encrypted invoice")
	}
}

func TestEncryptInvoice_InvalidPublicKey(t *testing.T) {
	// Build a syntactically valid but cryptographically tiny RSA public key
	// whose modulus is too small to hold an RSA-OAEP-SHA256 ciphertext.
	// We do this by manipulating a freshly generated key rather than calling
	// rsa.GenerateKey with a forbidden size (Go 1.20+ enforces minimums).
	priv, pub := generateTestKeyPair(t, 2048)
	_ = priv

	// Shrink the public key's modulus to 64 bytes (512 bits). rsa.EncryptOAEP
	// will reject it because max plaintext = modLen - 2*hashLen - 2 < 0.
	tiny := *pub
	tiny.N.SetInt64(1<<62 - 1) // 62-bit modulus, far below the OAEP minimum

	_, _, err := EncryptInvoice([]byte("<Faktura/>"), &tiny)
	if err == nil {
		t.Error("expected error for undersized RSA key")
	}
}

// ── helpers ───────────────────────────────────────────────────────────────────

// generateECPublicKey returns an EC public key to exercise the non-RSA type
// assertion branch inside LoadPublicKeyFromPEM.
func generateECPublicKey(t *testing.T) *ecdsa.PublicKey {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate EC key: %v", err)
	}
	return &priv.PublicKey
}
