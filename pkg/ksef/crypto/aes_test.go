package crypto

import (
	"bytes"
	"crypto/aes"
	"strings"
	"testing"
)

// ── PKCS7 padding ─────────────────────────────────────────────────────────────

func TestPKCS7Pad(t *testing.T) {
	t.Helper()
	tests := []struct {
		name      string
		input     []byte
		blockSize int
		wantLen   int
		wantByte  byte
	}{
		{
			name:      "empty input",
			input:     []byte{},
			blockSize: 16,
			wantLen:   16,
			wantByte:  16,
		},
		{
			name:      "already block-aligned adds full block",
			input:     bytes.Repeat([]byte("A"), 16),
			blockSize: 16,
			wantLen:   32,
			wantByte:  16,
		},
		{
			name:      "one byte short of block",
			input:     bytes.Repeat([]byte("A"), 15),
			blockSize: 16,
			wantLen:   16,
			wantByte:  1,
		},
		{
			name:      "arbitrary length",
			input:     bytes.Repeat([]byte("X"), 17),
			blockSize: 16,
			wantLen:   32,
			wantByte:  15,
		},
		{
			name:      "large input",
			input:     bytes.Repeat([]byte("Z"), 1024),
			blockSize: 16,
			wantLen:   1040, // 1024 + 16 (full padding block)
			wantByte:  16,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := pkcs7Pad(tt.input, tt.blockSize)

			if len(got) != tt.wantLen {
				t.Errorf("len = %d, want %d", len(got), tt.wantLen)
			}
			if got[len(got)-1] != tt.wantByte {
				t.Errorf("last byte = %d, want %d", got[len(got)-1], tt.wantByte)
			}
			// Original data must be preserved.
			if !bytes.Equal(got[:len(tt.input)], tt.input) {
				t.Error("original data corrupted after padding")
			}
		})
	}
}

func TestPKCS7Unpad(t *testing.T) {
	t.Run("round-trip", func(t *testing.T) {
		for _, size := range []int{0, 1, 15, 16, 17, 100, 1024} {
			input := bytes.Repeat([]byte{0x42}, size)
			padded := pkcs7Pad(input, aes.BlockSize)
			got, err := pkcs7Unpad(padded)
			if err != nil {
				t.Fatalf("size=%d: unexpected error: %v", size, err)
			}
			if !bytes.Equal(got, input) {
				t.Fatalf("size=%d: round-trip mismatch", size)
			}
		}
	})

	t.Run("invalid padding byte zero", func(t *testing.T) {
		_, err := pkcs7Unpad([]byte{
			0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
			0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		})
		if err == nil {
			t.Error("expected error for zero padding byte")
		}
	})

	t.Run("padding byte exceeds block size", func(t *testing.T) {
		data := bytes.Repeat([]byte{0x11}, 16)
		_, err := pkcs7Unpad(data) // 0x11 = 17, > 16
		if err == nil {
			t.Error("expected error when padding byte exceeds block size")
		}
	})

	t.Run("inconsistent padding bytes", func(t *testing.T) {
		data := []byte{
			0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
			0x09, 0x0A, 0x0B, 0x0C, 0x0D, 0x0E, 0x0F, 0x03,
		} // last byte says 3 but prior 2 don't match
		_, err := pkcs7Unpad(data)
		if err == nil {
			t.Error("expected error for inconsistent padding")
		}
	})

	t.Run("empty input", func(t *testing.T) {
		_, err := pkcs7Unpad([]byte{})
		if err == nil {
			t.Error("expected error for empty input")
		}
	})
}

// ── AES-256-CBC encrypt / decrypt ─────────────────────────────────────────────

func TestGenerateAESKey(t *testing.T) {
	key, err := GenerateAESKey()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(key) != 32 {
		t.Errorf("key length = %d, want 32", len(key))
	}

	// Two generated keys must differ (with overwhelming probability).
	key2, _ := GenerateAESKey()
	if bytes.Equal(key, key2) {
		t.Error("two generated keys are identical — RNG may be broken")
	}
}

func TestEncryptDecryptAESCBC(t *testing.T) {
	key, err := GenerateAESKey()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	plaintexts := []struct {
		name string
		data []byte
	}{
		{"empty", []byte{}},
		{"one byte", []byte{0x42}},
		{"block minus one", bytes.Repeat([]byte("A"), 15)},
		{"exact block", bytes.Repeat([]byte("B"), 16)},
		{"block plus one", bytes.Repeat([]byte("C"), 17)},
		{"large invoice", bytes.Repeat([]byte("<invoice/>"), 500)},
		{"xml content", []byte(`<?xml version="1.0"?><Faktura xmlns="http://crd.gov.pl/wzor/2023/06/29/12648/"><Naglowek><KodFormularza>FA</KodFormularza></Naglowek></Faktura>`)},
	}

	for _, tt := range plaintexts {
		t.Run(tt.name, func(t *testing.T) {
			ct, err := EncryptAESCBC(tt.data, key)
			if err != nil {
				t.Fatalf("encrypt: %v", err)
			}

			// Ciphertext must be IV + at least one padded block.
			minLen := aes.BlockSize + aes.BlockSize
			if len(ct) < minLen {
				t.Errorf("ciphertext length %d < minimum %d", len(ct), minLen)
			}
			if (len(ct)-aes.BlockSize)%aes.BlockSize != 0 {
				t.Error("ciphertext body is not block-aligned")
			}

			pt, err := DecryptAESCBC(ct, key)
			if err != nil {
				t.Fatalf("decrypt: %v", err)
			}
			if !bytes.Equal(pt, tt.data) {
				t.Errorf("round-trip mismatch: got %q, want %q", pt, tt.data)
			}
		})
	}
}

func TestEncryptAESCBC_RandomIV(t *testing.T) {
	// Encrypting the same plaintext twice must yield different ciphertexts
	// because the IV is random.
	key, _ := GenerateAESKey()
	plaintext := []byte("determinism test")

	ct1, err := EncryptAESCBC(plaintext, key)
	if err != nil {
		t.Fatal(err)
	}
	ct2, err := EncryptAESCBC(plaintext, key)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(ct1, ct2) {
		t.Error("two encryptions of the same plaintext produced identical ciphertext — IV is not random")
	}
}

func TestEncryptAESCBC_WrongKeyLength(t *testing.T) {
	for _, size := range []int{0, 16, 24, 31, 33, 64} {
		_, err := EncryptAESCBC([]byte("data"), make([]byte, size))
		if err == nil {
			t.Errorf("key size %d: expected error, got nil", size)
		}
	}
}

func TestDecryptAESCBC_WrongKeyLength(t *testing.T) {
	key, _ := GenerateAESKey()
	ct, _ := EncryptAESCBC([]byte("hello"), key)

	for _, size := range []int{0, 16, 24, 31, 33} {
		_, err := DecryptAESCBC(ct, make([]byte, size))
		if err == nil {
			t.Errorf("key size %d: expected error, got nil", size)
		}
	}
}

func TestDecryptAESCBC_TruncatedCiphertext(t *testing.T) {
	key, _ := GenerateAESKey()

	// Shorter than one IV block.
	_, err := DecryptAESCBC(make([]byte, 10), key)
	if err == nil {
		t.Error("expected error for ciphertext shorter than IV block")
	}

	// IV only, no actual ciphertext body (body length 0 is OK mod 16 but body == 0 bytes).
	_, err = DecryptAESCBC(make([]byte, aes.BlockSize), key)
	// Length check passes (0 % 16 == 0), but unpadding should fail.
	if err == nil {
		t.Error("expected error for ciphertext with no body")
	}

	// Not block-aligned.
	_, err = DecryptAESCBC(make([]byte, aes.BlockSize+7), key)
	if err == nil {
		t.Error("expected error for non-block-aligned ciphertext")
	}
}

func TestDecryptAESCBC_WrongKey(t *testing.T) {
	key, _ := GenerateAESKey()
	wrongKey, _ := GenerateAESKey()
	ct, _ := EncryptAESCBC([]byte("secret invoice"), key)

	_, err := DecryptAESCBC(ct, wrongKey)
	if err == nil {
		t.Error("expected error when decrypting with wrong key")
	}
}

func TestDecryptAESCBC_TamperedCiphertext(t *testing.T) {
	key, _ := GenerateAESKey()
	ct, _ := EncryptAESCBC([]byte("tamper test"), key)

	// Flip a byte in the ciphertext body (after IV).
	ct[aes.BlockSize] ^= 0xFF

	_, err := DecryptAESCBC(ct, key)
	if err == nil {
		t.Error("expected error when ciphertext is tampered")
	}
}

func TestEncryptAESCBC_LargePayload(t *testing.T) {
	// Simulate a large invoice XML (~100 KB).
	large := []byte(strings.Repeat("<Invoice>data</Invoice>", 5000))
	key, _ := GenerateAESKey()

	ct, err := EncryptAESCBC(large, key)
	if err != nil {
		t.Fatalf("encrypt large payload: %v", err)
	}

	pt, err := DecryptAESCBC(ct, key)
	if err != nil {
		t.Fatalf("decrypt large payload: %v", err)
	}

	if !bytes.Equal(pt, large) {
		t.Error("round-trip mismatch for large payload")
	}
}
