package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/CodartEU/ksef-go/internal/httpclient"
)

// ── shared test fixtures ──────────────────────────────────────────────────────

// testKey is a 2048-bit RSA key generated once for the whole test binary via
// TestMain to amortise the cost of key generation across all tests.
var testKey *rsa.PrivateKey

func TestMain(m *testing.M) {
	var err error
	testKey, err = rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic("auth_test: generate RSA key: " + err.Error())
	}
	os.Exit(m.Run())
}

// fixedTime is a stable timestamp used in test response fixtures.
var fixedTime = time.Date(2025, 7, 11, 12, 23, 56, 0, time.UTC)

// ── response builders ─────────────────────────────────────────────────────────

func makeChallengeJSON(challenge string, tsMs int64) []byte {
	b, _ := json.Marshal(map[string]any{
		"challenge":   challenge,
		"timestamp":   fixedTime.Format(time.RFC3339),
		"timestampMs": tsMs,
	})
	return b
}

func makeInitRespJSON(refNum, opToken string) []byte {
	b, _ := json.Marshal(map[string]any{
		"referenceNumber": refNum,
		"authenticationToken": map[string]any{
			"token":      opToken,
			"validUntil": fixedTime.Format(time.RFC3339),
		},
	})
	return b
}

func makeStatusJSON(code int, desc string) []byte {
	b, _ := json.Marshal(map[string]any{
		"status": map[string]any{"code": code, "description": desc},
	})
	return b
}

func makeRedeemRespJSON(accessToken, refreshToken string) []byte {
	b, _ := json.Marshal(map[string]any{
		"accessToken": map[string]any{
			"token":      accessToken,
			"validUntil": fixedTime.Format(time.RFC3339),
		},
		"refreshToken": map[string]any{
			"token":      refreshToken,
			"validUntil": fixedTime.Add(30 * 24 * time.Hour).Format(time.RFC3339),
		},
	})
	return b
}

// ── test helpers ──────────────────────────────────────────────────────────────

// newTestAuthenticator spins up an httptest.Server driven by handler and
// returns a TokenAuthenticator pointed at it. The authenticator is configured
// with a short poll interval / timeout suitable for unit tests.
func newTestAuthenticator(t *testing.T, handler http.Handler) *TokenAuthenticator {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	hc := httpclient.New(srv.URL, srv.Client(), nil, httpclient.RetryConfig{MaxRetries: 0})
	a := NewTokenAuthenticator(hc, &testKey.PublicKey)
	a.pollInterval = time.Millisecond
	a.pollTimeout = 200 * time.Millisecond
	return a
}

// writeJSON writes v as JSON with the given HTTP status code.
func writeJSON(w http.ResponseWriter, status int, body []byte) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

// ── tests ─────────────────────────────────────────────────────────────────────

func TestAuthenticate_HappyPath(t *testing.T) {
	const (
		nip         = "5265877635"
		ksefToken   = "test-ksef-token"
		refNum      = "20250514-AU-AABBCC0000-DDEEFF1122-A1"
		opToken     = "op.jwt.token"
		accessTok   = "access.jwt.token"
		refreshTok  = "refresh.jwt.token"
		challengeID = "20250625-CR-2FDC223000-C2BFC98A9C-4E"
		tsMs        = int64(1752236636015)
	)

	mux := http.NewServeMux()

	mux.HandleFunc("POST /auth/challenge", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, makeChallengeJSON(challengeID, tsMs))
	})

	mux.HandleFunc("POST /auth/ksef-token", func(w http.ResponseWriter, r *http.Request) {
		var body initTokenRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if body.Challenge != challengeID {
			http.Error(w, "wrong challenge", http.StatusBadRequest)
			return
		}
		if body.ContextIdentifier.Type != "Nip" || body.ContextIdentifier.Value != nip {
			http.Error(w, "wrong context identifier", http.StatusBadRequest)
			return
		}
		if body.EncryptedToken == "" {
			http.Error(w, "missing encrypted token", http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusAccepted, makeInitRespJSON(refNum, opToken))
	})

	mux.HandleFunc("GET /auth/{referenceNumber}", func(w http.ResponseWriter, r *http.Request) {
		if r.PathValue("referenceNumber") != refNum {
			http.Error(w, "unknown ref", http.StatusNotFound)
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+opToken {
			http.Error(w, "bad auth", http.StatusUnauthorized)
			return
		}
		writeJSON(w, http.StatusOK, makeStatusJSON(200, "Uwierzytelnianie zakończone sukcesem"))
	})

	mux.HandleFunc("POST /auth/token/redeem", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+opToken {
			http.Error(w, "bad auth", http.StatusUnauthorized)
			return
		}
		writeJSON(w, http.StatusOK, makeRedeemRespJSON(accessTok, refreshTok))
	})

	a := newTestAuthenticator(t, mux)
	result, err := a.Authenticate(context.Background(), nip, ksefToken)
	if err != nil {
		t.Fatalf("Authenticate: unexpected error: %v", err)
	}
	if result.AccessToken != accessTok {
		t.Errorf("AccessToken = %q, want %q", result.AccessToken, accessTok)
	}
	if result.RefreshToken != refreshTok {
		t.Errorf("RefreshToken = %q, want %q", result.RefreshToken, refreshTok)
	}
	if result.ReferenceNumber != refNum {
		t.Errorf("ReferenceNumber = %q, want %q", result.ReferenceNumber, refNum)
	}
	if result.AccessTokenValidUntil.IsZero() {
		t.Error("AccessTokenValidUntil is zero")
	}
	if result.RefreshTokenValidUntil.IsZero() {
		t.Error("RefreshTokenValidUntil is zero")
	}
}

func TestAuthenticate_PollRetry(t *testing.T) {
	// First status poll returns 100 (in progress), second returns 200 (done).
	var pollCount atomic.Int32

	mux := http.NewServeMux()
	mux.HandleFunc("POST /auth/challenge", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, makeChallengeJSON("ch", 123))
	})
	mux.HandleFunc("POST /auth/ksef-token", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusAccepted, makeInitRespJSON("ref-1", "op-tok"))
	})
	mux.HandleFunc("GET /auth/{referenceNumber}", func(w http.ResponseWriter, r *http.Request) {
		if pollCount.Add(1) == 1 {
			writeJSON(w, http.StatusOK, makeStatusJSON(100, "Uwierzytelnianie w toku"))
			return
		}
		writeJSON(w, http.StatusOK, makeStatusJSON(200, "Uwierzytelnianie zakończone sukcesem"))
	})
	mux.HandleFunc("POST /auth/token/redeem", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, makeRedeemRespJSON("acc", "ref"))
	})

	a := newTestAuthenticator(t, mux)
	if _, err := a.Authenticate(context.Background(), "1234567890", "tok"); err != nil {
		t.Fatalf("Authenticate: unexpected error: %v", err)
	}
	if got := pollCount.Load(); got != 2 {
		t.Errorf("poll count = %d, want 2", got)
	}
}

func TestAuthenticate_PollTimeout(t *testing.T) {
	// Status endpoint always returns 100 — should trigger poll timeout.
	mux := http.NewServeMux()
	mux.HandleFunc("POST /auth/challenge", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, makeChallengeJSON("ch", 123))
	})
	mux.HandleFunc("POST /auth/ksef-token", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusAccepted, makeInitRespJSON("ref-1", "op-tok"))
	})
	mux.HandleFunc("GET /auth/{referenceNumber}", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, makeStatusJSON(100, "Uwierzytelnianie w toku"))
	})

	a := newTestAuthenticator(t, mux)
	_, err := a.Authenticate(context.Background(), "1234567890", "tok")
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Errorf("error = %q, want it to contain 'timed out'", err.Error())
	}
}

func TestAuthenticate_PollAuthFailure(t *testing.T) {
	// Status endpoint returns a non-100/200 code indicating failure.
	mux := http.NewServeMux()
	mux.HandleFunc("POST /auth/challenge", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, makeChallengeJSON("ch", 123))
	})
	mux.HandleFunc("POST /auth/ksef-token", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusAccepted, makeInitRespJSON("ref-1", "op-tok"))
	})
	mux.HandleFunc("GET /auth/{referenceNumber}", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, makeStatusJSON(400, "Uwierzytelnianie zakończone niepowodzeniem"))
	})

	a := newTestAuthenticator(t, mux)
	_, err := a.Authenticate(context.Background(), "1234567890", "tok")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "400") {
		t.Errorf("error = %q, want it to contain '400'", err.Error())
	}
}

func TestAuthenticate_ChallengeError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /auth/challenge", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusInternalServerError, []byte(`{}`))
	})

	a := newTestAuthenticator(t, mux)
	_, err := a.Authenticate(context.Background(), "1234567890", "tok")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "get challenge") {
		t.Errorf("error = %q, want it to mention 'get challenge'", err.Error())
	}
}

func TestAuthenticate_SubmitTokenError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /auth/challenge", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, makeChallengeJSON("ch", 123))
	})
	mux.HandleFunc("POST /auth/ksef-token", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusBadRequest, []byte(`{}`))
	})

	a := newTestAuthenticator(t, mux)
	_, err := a.Authenticate(context.Background(), "1234567890", "tok")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "submit token") {
		t.Errorf("error = %q, want it to mention 'submit token'", err.Error())
	}
}

func TestAuthenticate_RedeemError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /auth/challenge", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, makeChallengeJSON("ch", 123))
	})
	mux.HandleFunc("POST /auth/ksef-token", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusAccepted, makeInitRespJSON("ref-1", "op-tok"))
	})
	mux.HandleFunc("GET /auth/{referenceNumber}", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, makeStatusJSON(200, "OK"))
	})
	mux.HandleFunc("POST /auth/token/redeem", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusBadRequest, []byte(`{}`))
	})

	a := newTestAuthenticator(t, mux)
	_, err := a.Authenticate(context.Background(), "1234567890", "tok")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "redeem tokens") {
		t.Errorf("error = %q, want it to mention 'redeem tokens'", err.Error())
	}
}

func TestAuthenticate_ContextCancelled(t *testing.T) {
	// Poll loop should respect ctx cancellation.
	mux := http.NewServeMux()
	mux.HandleFunc("POST /auth/challenge", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, makeChallengeJSON("ch", 123))
	})
	mux.HandleFunc("POST /auth/ksef-token", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusAccepted, makeInitRespJSON("ref-1", "op-tok"))
	})
	mux.HandleFunc("GET /auth/{referenceNumber}", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, makeStatusJSON(100, "in progress"))
	})

	a := newTestAuthenticator(t, mux)
	a.pollTimeout = 10 * time.Second // long timeout; cancellation comes from ctx

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := a.Authenticate(ctx, "1234567890", "tok")
	if err == nil {
		t.Fatal("expected error from context cancellation, got nil")
	}
}

func TestEncryptToken(t *testing.T) {
	// encryptToken must produce a non-empty Base64 string that decrypts back to
	// "token|timestampMs" using the matching private key.
	const (
		token = "my-ksef-token"
		tsMs  = int64(1752236636015)
	)

	encoded, err := encryptToken(&testKey.PublicKey, token, tsMs)
	if err != nil {
		t.Fatalf("encryptToken: %v", err)
	}
	if encoded == "" {
		t.Fatal("encryptToken returned empty string")
	}

	ciphertext, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("base64 decode: %v", err)
	}

	plaintext, err := rsa.DecryptOAEP(sha256.New(), rand.Reader, testKey, ciphertext, nil)
	if err != nil {
		t.Fatalf("DecryptOAEP: %v", err)
	}

	const want = "my-ksef-token|1752236636015"
	if string(plaintext) != want {
		t.Errorf("decrypted plaintext = %q, want %q", string(plaintext), want)
	}
}
