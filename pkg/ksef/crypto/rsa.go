package crypto

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/pem"
	"fmt"
)

// EncryptInvoice encrypts invoiceXML for submission to KSeF.
//
// It generates a random AES-256 key, encrypts the invoice XML with
// AES-256-CBC (IV prepended), and wraps the AES key with RSA-OAEP using
// ksefPublicKey. Both blobs are returned so the caller can include them in the
// KSeF submission request.
func EncryptInvoice(invoiceXML []byte, ksefPublicKey *rsa.PublicKey) (encryptedInvoice, encryptedKey []byte, err error) {
	aesKey, err := GenerateAESKey()
	if err != nil {
		return nil, nil, err
	}

	encryptedInvoice, err = EncryptAESCBC(invoiceXML, aesKey)
	if err != nil {
		return nil, nil, fmt.Errorf("crypto: encrypt invoice: %w", err)
	}

	encryptedKey, err = encryptAESKey(aesKey, ksefPublicKey)
	if err != nil {
		return nil, nil, fmt.Errorf("crypto: wrap AES key: %w", err)
	}

	return encryptedInvoice, encryptedKey, nil
}

// DecryptInvoice reverses EncryptInvoice. It unwraps the AES key using
// privateKey (RSA-OAEP / SHA-256) and then decrypts encryptedInvoice with
// AES-256-CBC, returning the original invoice XML.
func DecryptInvoice(encryptedInvoice, encryptedKey []byte, privateKey *rsa.PrivateKey) ([]byte, error) {
	aesKey, err := decryptAESKey(encryptedKey, privateKey)
	if err != nil {
		return nil, fmt.Errorf("crypto: unwrap AES key: %w", err)
	}

	plaintext, err := DecryptAESCBC(encryptedInvoice, aesKey)
	if err != nil {
		return nil, fmt.Errorf("crypto: decrypt invoice: %w", err)
	}

	return plaintext, nil
}

// LoadPublicKeyFromPEM parses a PEM-encoded RSA public key. Three formats are
// accepted:
//   - "BEGIN CERTIFICATE"    — X.509 certificate; the RSA public key is
//     extracted from the certificate's SubjectPublicKeyInfo.
//   - "BEGIN PUBLIC KEY"     — PKIX/SPKI-encoded RSA public key.
//   - "BEGIN RSA PUBLIC KEY" — PKCS#1-encoded RSA public key.
//
// The KSeF API's /security/public-key-certificates endpoint returns the public
// key as a DER-encoded X.509 certificate (base64), so callers that persist the
// certificate to PEM should use the CERTIFICATE type.
func LoadPublicKeyFromPEM(pemData []byte) (*rsa.PublicKey, error) {
	block, _ := pem.Decode(pemData)
	if block == nil {
		return nil, fmt.Errorf("crypto: no PEM block found")
	}

	switch block.Type {
	case "CERTIFICATE":
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("crypto: parse X.509 certificate: %w", err)
		}
		rsaKey, ok := cert.PublicKey.(*rsa.PublicKey)
		if !ok {
			return nil, fmt.Errorf("crypto: certificate does not contain an RSA public key")
		}
		return rsaKey, nil

	case "PUBLIC KEY":
		key, err := x509.ParsePKIXPublicKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("crypto: parse PKIX public key: %w", err)
		}
		rsaKey, ok := key.(*rsa.PublicKey)
		if !ok {
			return nil, fmt.Errorf("crypto: PEM block is not an RSA public key")
		}
		return rsaKey, nil

	case "RSA PUBLIC KEY":
		key, err := x509.ParsePKCS1PublicKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("crypto: parse PKCS1 public key: %w", err)
		}
		return key, nil

	default:
		return nil, fmt.Errorf("crypto: unsupported PEM block type %q (want \"CERTIFICATE\", \"PUBLIC KEY\", or \"RSA PUBLIC KEY\")", block.Type)
	}
}

// encryptAESKey wraps aesKey using RSA-OAEP with SHA-256.
func encryptAESKey(aesKey []byte, pub *rsa.PublicKey) ([]byte, error) {
	ct, err := rsa.EncryptOAEP(sha256.New(), rand.Reader, pub, aesKey, nil)
	if err != nil {
		return nil, fmt.Errorf("crypto: RSA-OAEP encrypt: %w", err)
	}
	return ct, nil
}

// decryptAESKey unwraps an RSA-OAEP / SHA-256 encrypted AES key.
func decryptAESKey(encryptedKey []byte, priv *rsa.PrivateKey) ([]byte, error) {
	key, err := rsa.DecryptOAEP(sha256.New(), rand.Reader, priv, encryptedKey, nil)
	if err != nil {
		return nil, fmt.Errorf("crypto: RSA-OAEP decrypt: %w", err)
	}
	return key, nil
}
