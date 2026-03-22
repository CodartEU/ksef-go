package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/CodartEU/ksef-go/internal/httpclient"
	ksefcrypto "github.com/CodartEU/ksef-go/pkg/ksef/crypto"
)

// ── test helpers ──────────────────────────────────────────────────────────────

// newTestXAdESAuthenticator spins up an httptest.Server driven by handler and
// returns an XAdESAuthenticator pointed at it with short poll intervals.
func newTestXAdESAuthenticator(t *testing.T, handler http.Handler) (*XAdESAuthenticator, []byte, []byte) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	hc := httpclient.New(srv.URL, srv.Client(), nil, httpclient.RetryConfig{MaxRetries: 0})
	a := NewXAdESAuthenticator(hc)
	a.pollInterval = time.Millisecond
	a.pollTimeout = 200 * time.Millisecond

	certPEM, keyPEM, err := ksefcrypto.GenerateTestCertificate("5265877635")
	if err != nil {
		t.Fatalf("GenerateTestCertificate: %v", err)
	}
	return a, certPEM, keyPEM
}

// ── tests ─────────────────────────────────────────────────────────────────────

func TestXAdES_HappyPath(t *testing.T) {
	const (
		nip         = "5265877635"
		refNum      = "20250514-AU-AABBCC0000-DDEEFF1122-A1"
		opToken     = "op.jwt.xades"
		accessTok   = "access.jwt.xades"
		refreshTok  = "refresh.jwt.xades"
		challengeID = "20250625-CR-2FDC223000-C2BFC98A9C-4E"
		tsMs        = int64(1752236636015)
	)

	mux := http.NewServeMux()

	mux.HandleFunc("POST /auth/challenge", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, makeChallengeJSON(challengeID, tsMs))
	})

	mux.HandleFunc("POST /auth/xades-signature", func(w http.ResponseWriter, r *http.Request) {
		// Verify Content-Type is application/xml.
		if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/xml") {
			http.Error(w, "wrong Content-Type: "+ct, http.StatusBadRequest)
			return
		}
		// Body must be non-empty XML.
		if r.ContentLength == 0 {
			http.Error(w, "empty body", http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusAccepted, makeInitRespJSON(refNum, opToken))
	})

	mux.HandleFunc("GET /auth/{referenceNumber}", func(w http.ResponseWriter, r *http.Request) {
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

	a, certPEM, keyPEM := newTestXAdESAuthenticator(t, mux)
	result, err := a.Authenticate(context.Background(), nip, certPEM, keyPEM)
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
}

func TestXAdES_SubmittedXMLContainsSignature(t *testing.T) {
	var receivedBody []byte

	mux := http.NewServeMux()
	mux.HandleFunc("POST /auth/challenge", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, makeChallengeJSON("test-challenge", 123))
	})
	mux.HandleFunc("POST /auth/xades-signature", func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 64*1024)
		n, _ := r.Body.Read(buf)
		receivedBody = buf[:n]
		writeJSON(w, http.StatusAccepted, makeInitRespJSON("ref", "op"))
	})
	mux.HandleFunc("GET /auth/{referenceNumber}", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, makeStatusJSON(200, "OK"))
	})
	mux.HandleFunc("POST /auth/token/redeem", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, makeRedeemRespJSON("acc", "ref"))
	})

	a, certPEM, keyPEM := newTestXAdESAuthenticator(t, mux)
	if _, err := a.Authenticate(context.Background(), "5265877635", certPEM, keyPEM); err != nil {
		t.Fatalf("Authenticate: %v", err)
	}

	body := string(receivedBody)
	for _, want := range []string{
		`<?xml version="1.0" encoding="utf-8"?>`,
		`<AuthTokenRequest`,
		`<Challenge>test-challenge</Challenge>`,
		`<ds:Signature`,
		`<ds:SignedInfo`,
		`<ds:SignatureValue`,
		`<xades:QualifyingProperties`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("submitted XML missing %q", want)
		}
	}
}

func TestXAdES_PollRetry(t *testing.T) {
	var pollCount atomic.Int32

	mux := http.NewServeMux()
	mux.HandleFunc("POST /auth/challenge", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, makeChallengeJSON("ch", 123))
	})
	mux.HandleFunc("POST /auth/xades-signature", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusAccepted, makeInitRespJSON("ref-1", "op-tok"))
	})
	mux.HandleFunc("GET /auth/{referenceNumber}", func(w http.ResponseWriter, r *http.Request) {
		if pollCount.Add(1) == 1 {
			writeJSON(w, http.StatusOK, makeStatusJSON(100, "Uwierzytelnianie w toku"))
			return
		}
		writeJSON(w, http.StatusOK, makeStatusJSON(200, "OK"))
	})
	mux.HandleFunc("POST /auth/token/redeem", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, makeRedeemRespJSON("acc", "ref"))
	})

	a, certPEM, keyPEM := newTestXAdESAuthenticator(t, mux)
	if _, err := a.Authenticate(context.Background(), "1234567890", certPEM, keyPEM); err != nil {
		t.Fatalf("Authenticate: unexpected error: %v", err)
	}
	if got := pollCount.Load(); got != 2 {
		t.Errorf("poll count = %d, want 2", got)
	}
}

func TestXAdES_PollTimeout(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /auth/challenge", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, makeChallengeJSON("ch", 123))
	})
	mux.HandleFunc("POST /auth/xades-signature", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusAccepted, makeInitRespJSON("ref-1", "op-tok"))
	})
	mux.HandleFunc("GET /auth/{referenceNumber}", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, makeStatusJSON(100, "Uwierzytelnianie w toku"))
	})

	a, certPEM, keyPEM := newTestXAdESAuthenticator(t, mux)
	_, err := a.Authenticate(context.Background(), "1234567890", certPEM, keyPEM)
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Errorf("error = %q, want it to contain 'timed out'", err.Error())
	}
}

func TestXAdES_ChallengeError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /auth/challenge", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusInternalServerError, []byte(`{}`))
	})

	a, certPEM, keyPEM := newTestXAdESAuthenticator(t, mux)
	_, err := a.Authenticate(context.Background(), "1234567890", certPEM, keyPEM)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "get challenge") {
		t.Errorf("error = %q, want it to mention 'get challenge'", err.Error())
	}
}

func TestXAdES_SubmitError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /auth/challenge", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, makeChallengeJSON("ch", 123))
	})
	mux.HandleFunc("POST /auth/xades-signature", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusBadRequest, []byte(`{}`))
	})

	a, certPEM, keyPEM := newTestXAdESAuthenticator(t, mux)
	_, err := a.Authenticate(context.Background(), "1234567890", certPEM, keyPEM)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "submit signature") {
		t.Errorf("error = %q, want it to mention 'submit signature'", err.Error())
	}
}

func TestXAdES_RedeemError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /auth/challenge", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, makeChallengeJSON("ch", 123))
	})
	mux.HandleFunc("POST /auth/xades-signature", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusAccepted, makeInitRespJSON("ref-1", "op-tok"))
	})
	mux.HandleFunc("GET /auth/{referenceNumber}", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, makeStatusJSON(200, "OK"))
	})
	mux.HandleFunc("POST /auth/token/redeem", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusBadRequest, []byte(`{}`))
	})

	a, certPEM, keyPEM := newTestXAdESAuthenticator(t, mux)
	_, err := a.Authenticate(context.Background(), "1234567890", certPEM, keyPEM)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "redeem tokens") {
		t.Errorf("error = %q, want it to mention 'redeem tokens'", err.Error())
	}
}

func TestXAdES_InvalidCertPEM(t *testing.T) {
	a, _, keyPEM := newTestXAdESAuthenticator(t, http.NewServeMux())
	_, err := a.Authenticate(context.Background(), "1234567890", []byte("not-a-pem"), keyPEM)
	if err == nil {
		t.Fatal("expected error for invalid cert PEM, got nil")
	}
	if !strings.Contains(err.Error(), "parse credentials") {
		t.Errorf("error = %q, want it to mention 'parse credentials'", err.Error())
	}
}

func TestXAdES_ContextCancelled(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /auth/challenge", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, makeChallengeJSON("ch", 123))
	})
	mux.HandleFunc("POST /auth/xades-signature", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusAccepted, makeInitRespJSON("ref-1", "op-tok"))
	})
	mux.HandleFunc("GET /auth/{referenceNumber}", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, makeStatusJSON(100, "in progress"))
	})

	a, certPEM, keyPEM := newTestXAdESAuthenticator(t, mux)
	a.pollTimeout = 10 * time.Second

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := a.Authenticate(ctx, "1234567890", certPEM, keyPEM)
	if err == nil {
		t.Fatal("expected error from context cancellation, got nil")
	}
}

// ── buildAuthRequestDoc ───────────────────────────────────────────────────────

func TestBuildAuthRequestDoc_Structure(t *testing.T) {
	const challenge = "20250625-CR-20F5EE4000-DA48AE4124-46"
	const nip = "5265877635"

	doc := buildAuthRequestDoc(challenge, nip)
	s := string(doc)

	want := []string{
		`<AuthTokenRequest xmlns="http://ksef.mf.gov.pl/auth/token/2.0"`,
		` xmlns:xsd="http://www.w3.org/2001/XMLSchema"`,
		` xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance"`,
		`<Challenge>` + challenge + `</Challenge>`,
		`<ContextIdentifier><Nip>` + nip + `</Nip></ContextIdentifier>`,
		`<SubjectIdentifierType>certificateSubject</SubjectIdentifierType>`,
		`</AuthTokenRequest>`,
	}
	for _, fragment := range want {
		if !strings.Contains(s, fragment) {
			t.Errorf("doc missing %q\ndoc: %s", fragment, s)
		}
	}

	// Must NOT contain an XML declaration (canonical form).
	if strings.Contains(s, "<?xml") {
		t.Error("canonical doc must not contain XML declaration")
	}
}

func TestBuildAuthRequestDoc_XMLEscaping(t *testing.T) {
	doc := buildAuthRequestDoc("a&b<c>d", "<nip>")
	s := string(doc)

	if strings.Contains(s, "<Challenge>a&b<c>d</Challenge>") {
		t.Error("challenge was not XML-escaped")
	}
	if !strings.Contains(s, "<Challenge>a&amp;b&lt;c&gt;d</Challenge>") {
		t.Errorf("challenge escaping wrong, got: %s", s)
	}
}

// ── parseCertAndKey ───────────────────────────────────────────────────────────

func TestParseCertAndKey_ValidPKCS1(t *testing.T) {
	certPEM, keyPEM, err := ksefcrypto.GenerateTestCertificate("5265877635")
	if err != nil {
		t.Fatalf("GenerateTestCertificate: %v", err)
	}
	cert, key, err := parseCertAndKey(certPEM, keyPEM)
	if err != nil {
		t.Fatalf("parseCertAndKey: %v", err)
	}
	if cert == nil {
		t.Error("cert is nil")
	}
	if key == nil {
		t.Error("key is nil")
	}
}

func TestParseCertAndKey_InvalidCert(t *testing.T) {
	_, keyPEM, _ := ksefcrypto.GenerateTestCertificate("5265877635")
	_, _, err := parseCertAndKey([]byte("garbage"), keyPEM)
	if err == nil {
		t.Fatal("expected error for invalid cert, got nil")
	}
}

func TestParseCertAndKey_InvalidKey(t *testing.T) {
	certPEM, _, _ := ksefcrypto.GenerateTestCertificate("5265877635")
	_, _, err := parseCertAndKey(certPEM, []byte("garbage"))
	if err == nil {
		t.Fatal("expected error for invalid key, got nil")
	}
}
