package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
)

// aesKeySize is the required key length for AES-256.
const aesKeySize = 32

// ErrInvalidPadding is returned when PKCS7 unpadding fails.
var ErrInvalidPadding = errors.New("crypto: invalid PKCS7 padding")

// GenerateAESKey generates a cryptographically random 256-bit AES key.
func GenerateAESKey() ([]byte, error) {
	key := make([]byte, aesKeySize)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, fmt.Errorf("crypto: generate AES key: %w", err)
	}
	return key, nil
}

// EncryptAESCBCWithIV encrypts plaintext with AES-256-CBC using the provided
// 32-byte key and a caller-supplied 16-byte IV. The IV is NOT prepended to the
// returned slice — the output is pure ciphertext (PKCS7-padded). Use this when
// the receiver already knows the IV (e.g. a KSeF session that was opened with
// a specific IV).
func EncryptAESCBCWithIV(plaintext, key, iv []byte) ([]byte, error) {
	if len(key) != aesKeySize {
		return nil, fmt.Errorf("crypto: AES key must be %d bytes, got %d", aesKeySize, len(key))
	}
	if len(iv) != aes.BlockSize {
		return nil, fmt.Errorf("crypto: IV must be %d bytes, got %d", aes.BlockSize, len(iv))
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("crypto: create AES cipher: %w", err)
	}

	padded := pkcs7Pad(plaintext, aes.BlockSize)
	out := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(out, padded)
	return out, nil
}

// EncryptAESCBC encrypts plaintext with AES-256-CBC using the provided 32-byte
// key. A random 16-byte IV is prepended to the returned ciphertext so the
// result has the layout: [ IV (16 bytes) | ciphertext ].
func EncryptAESCBC(plaintext, key []byte) ([]byte, error) {
	if len(key) != aesKeySize {
		return nil, fmt.Errorf("crypto: AES key must be %d bytes, got %d", aesKeySize, len(key))
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("crypto: create AES cipher: %w", err)
	}

	padded := pkcs7Pad(plaintext, aes.BlockSize)

	out := make([]byte, aes.BlockSize+len(padded))
	iv := out[:aes.BlockSize]
	if _, err := io.ReadFull(rand.Reader, iv); err != nil {
		return nil, fmt.Errorf("crypto: generate IV: %w", err)
	}

	cipher.NewCBCEncrypter(block, iv).CryptBlocks(out[aes.BlockSize:], padded)
	return out, nil
}

// DecryptAESCBC decrypts ciphertext produced by EncryptAESCBC. The input must
// have the layout [ IV (16 bytes) | ciphertext ] and the key must be 32 bytes.
func DecryptAESCBC(ciphertext, key []byte) ([]byte, error) {
	if len(key) != aesKeySize {
		return nil, fmt.Errorf("crypto: AES key must be %d bytes, got %d", aesKeySize, len(key))
	}
	if len(ciphertext) < aes.BlockSize {
		return nil, fmt.Errorf("crypto: ciphertext too short (minimum %d bytes)", aes.BlockSize)
	}
	if (len(ciphertext)-aes.BlockSize)%aes.BlockSize != 0 {
		return nil, fmt.Errorf("crypto: ciphertext length is not a multiple of the AES block size")
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("crypto: create AES cipher: %w", err)
	}

	iv := ciphertext[:aes.BlockSize]
	padded := make([]byte, len(ciphertext)-aes.BlockSize)
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(padded, ciphertext[aes.BlockSize:])

	return pkcs7Unpad(padded)
}

// pkcs7Pad appends PKCS7 padding to data so that its length is a multiple of
// blockSize. blockSize must be in the range [1, 255]. A full block of padding
// is added when data is already block-aligned, guaranteeing that unpadding is
// always unambiguous.
func pkcs7Pad(data []byte, blockSize int) []byte {
	padding := blockSize - len(data)%blockSize
	padded := make([]byte, len(data)+padding)
	copy(padded, data)
	for i := len(data); i < len(padded); i++ {
		padded[i] = byte(padding)
	}
	return padded
}

// pkcs7Unpad removes PKCS7 padding added by pkcs7Pad.
// It returns ErrInvalidPadding if the padding is malformed.
func pkcs7Unpad(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, ErrInvalidPadding
	}
	padding := int(data[len(data)-1])
	if padding == 0 || padding > aes.BlockSize || padding > len(data) {
		return nil, ErrInvalidPadding
	}
	for i := len(data) - padding; i < len(data); i++ {
		if data[i] != byte(padding) {
			return nil, ErrInvalidPadding
		}
	}
	return data[:len(data)-padding], nil
}
