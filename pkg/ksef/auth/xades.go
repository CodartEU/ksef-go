package auth

import (
	"bytes"
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"time"

	"github.com/CodartEU/ksef-go/internal/httpclient"
	ksefcrypto "github.com/CodartEU/ksef-go/pkg/ksef/crypto"
)

// XAdESAuthenticator authenticates with the KSeF API using an XAdES-BES
// electronic signature over an AuthTokenRequest XML document.
//
// The authentication flow is:
//  1. POST /auth/challenge — obtain a one-time challenge and timestamp.
//  2. Build and sign the AuthTokenRequest XML with XAdES-BES.
//  3. POST /auth/xades-signature — submit the signed XML document.
//  4. GET  /auth/{referenceNumber} — poll until status 200 (success).
//  5. POST /auth/token/redeem — exchange the operation token for access and
//     refresh tokens.
type XAdESAuthenticator struct {
	http         *httpclient.Client
	pollInterval time.Duration
	pollTimeout  time.Duration
}

// NewXAdESAuthenticator creates an XAdESAuthenticator.
//
// hc is the internal HTTP client already configured with the correct base URL
// and retry policy.
func NewXAdESAuthenticator(hc *httpclient.Client) *XAdESAuthenticator {
	return &XAdESAuthenticator{
		http:         hc,
		pollInterval: time.Second,
		pollTimeout:  30 * time.Second,
	}
}

// Authenticate executes the full KSeF XAdES authentication flow and returns
// the resulting tokens on success.
//
// nip is the Polish tax identifier (NIP) of the entity being authenticated.
// certPEM is the PEM-encoded X.509 certificate to use for signing.
// keyPEM is the PEM-encoded RSA private key corresponding to certPEM
// (PKCS#1 or PKCS#8 format).
func (a *XAdESAuthenticator) Authenticate(ctx context.Context, nip string, certPEM, keyPEM []byte) (*AuthResult, error) {
	cert, privateKey, err := parseCertAndKey(certPEM, keyPEM)
	if err != nil {
		return nil, fmt.Errorf("auth/xades: parse credentials: %w", err)
	}

	// Step 1: obtain challenge.
	ch, err := a.getChallenge(ctx)
	if err != nil {
		return nil, fmt.Errorf("auth/xades: get challenge: %w", err)
	}

	// Step 2: build and sign the AuthTokenRequest XML.
	signedXML, err := buildAndSign(ch.Challenge, nip, cert, privateKey)
	if err != nil {
		return nil, fmt.Errorf("auth/xades: build signed XML: %w", err)
	}

	// Step 3: submit the signed XML document.
	initResp, err := a.submitXAdES(ctx, signedXML)
	if err != nil {
		return nil, fmt.Errorf("auth/xades: submit signature: %w", err)
	}

	// Step 4: poll until authentication completes.
	if err := a.pollUntilDone(ctx, initResp.ReferenceNumber, initResp.AuthenticationToken.Token); err != nil {
		return nil, fmt.Errorf("auth/xades: poll status: %w", err)
	}

	// Step 5: redeem operation token for access + refresh tokens.
	tokens, err := a.redeemTokens(ctx, initResp.AuthenticationToken.Token)
	if err != nil {
		return nil, fmt.Errorf("auth/xades: redeem tokens: %w", err)
	}

	return &AuthResult{
		AccessToken:            tokens.AccessToken.Token,
		AccessTokenValidUntil:  tokens.AccessToken.ValidUntil,
		RefreshToken:           tokens.RefreshToken.Token,
		RefreshTokenValidUntil: tokens.RefreshToken.ValidUntil,
		ReferenceNumber:        initResp.ReferenceNumber,
	}, nil
}

// ── private helpers ───────────────────────────────────────────────────────────

func (a *XAdESAuthenticator) getChallenge(ctx context.Context) (*challengeResponse, error) {
	raw, err := a.http.Post(ctx, "/auth/challenge", nil, nil)
	if err != nil {
		return nil, err
	}
	var resp challengeResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &resp, nil
}

func (a *XAdESAuthenticator) submitXAdES(ctx context.Context, signedXML []byte) (*authInitResponse, error) {
	raw, err := a.http.PostRaw(ctx, "/auth/xades-signature", "application/xml", signedXML, nil)
	if err != nil {
		return nil, err
	}
	var resp authInitResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &resp, nil
}

func (a *XAdESAuthenticator) pollUntilDone(ctx context.Context, referenceNumber, authToken string) error {
	headers := map[string]string{"Authorization": "Bearer " + authToken}
	deadline := time.Now().Add(a.pollTimeout)

	for {
		raw, err := a.http.Get(ctx, "/auth/"+referenceNumber, headers)
		if err != nil {
			return err
		}
		var resp authStatusResponse
		if err := json.Unmarshal(raw, &resp); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}

		switch resp.Status.Code {
		case 100:
			// authentication in progress — continue polling
		case 200:
			return nil
		default:
			return fmt.Errorf("authentication failed (status %d): %s", resp.Status.Code, resp.Status.Description)
		}

		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for authentication to complete")
		}
		if err := sleepContext(ctx, a.pollInterval); err != nil {
			return err
		}
	}
}

func (a *XAdESAuthenticator) redeemTokens(ctx context.Context, authToken string) (*authTokensResponse, error) {
	headers := map[string]string{"Authorization": "Bearer " + authToken}
	raw, err := a.http.Post(ctx, "/auth/token/redeem", nil, headers)
	if err != nil {
		return nil, err
	}
	var resp authTokensResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &resp, nil
}

// buildAndSign constructs the canonical AuthTokenRequest document and applies
// an enveloped XAdES-BES signature.
//
// The canonical document form (no XML declaration, compact, namespace
// declarations in C14N 1.0 alphabetical order) is required so that the
// document digest matches what a verifier computes after applying the
// enveloped-signature transform and C14N.
func buildAndSign(challenge, nip string, cert *x509.Certificate, key *rsa.PrivateKey) ([]byte, error) {
	doc := buildAuthRequestDoc(challenge, nip)
	return ksefcrypto.SignXML(doc, cert, key)
}

// buildAuthRequestDoc returns the canonical-form AuthTokenRequest element
// (no XML declaration, compact, namespaces in C14N sorted order).
//
// challenge and nip are escaped for XML safety.
func buildAuthRequestDoc(challenge, nip string) []byte {
	var buf bytes.Buffer
	buf.WriteString(`<AuthTokenRequest`)
	buf.WriteString(` xmlns="` + ksefcrypto.NSKSeFAuth + `"`)
	buf.WriteString(` xmlns:xsd="http://www.w3.org/2001/XMLSchema"`)
	buf.WriteString(` xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">`)
	buf.WriteString(`<Challenge>`)
	xmlEscapeText(&buf, challenge)
	buf.WriteString(`</Challenge>`)
	buf.WriteString(`<ContextIdentifier><Nip>`)
	xmlEscapeText(&buf, nip)
	buf.WriteString(`</Nip></ContextIdentifier>`)
	buf.WriteString(`<SubjectIdentifierType>certificateSubject</SubjectIdentifierType>`)
	buf.WriteString(`</AuthTokenRequest>`)
	return buf.Bytes()
}

// xmlEscapeText writes s to w with XML special characters escaped.
func xmlEscapeText(w *bytes.Buffer, s string) {
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '&':
			w.WriteString("&amp;")
		case '<':
			w.WriteString("&lt;")
		case '>':
			w.WriteString("&gt;")
		default:
			w.WriteByte(s[i])
		}
	}
}

// parseCertAndKey decodes PEM-encoded certificate and private key bytes.
// Both PKCS#1 and PKCS#8 key formats are accepted.
func parseCertAndKey(certPEM, keyPEM []byte) (*x509.Certificate, *rsa.PrivateKey, error) {
	certBlock, _ := pem.Decode(certPEM)
	if certBlock == nil {
		return nil, nil, fmt.Errorf("parse cert PEM: no PEM block found")
	}
	cert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		return nil, nil, fmt.Errorf("parse certificate: %w", err)
	}

	keyBlock, _ := pem.Decode(keyPEM)
	if keyBlock == nil {
		return nil, nil, fmt.Errorf("parse key PEM: no PEM block found")
	}

	// Try PKCS#1 first (RSA PRIVATE KEY), then PKCS#8 (PRIVATE KEY).
	if key, err := x509.ParsePKCS1PrivateKey(keyBlock.Bytes); err == nil {
		return cert, key, nil
	}
	key8, err := x509.ParsePKCS8PrivateKey(keyBlock.Bytes)
	if err != nil {
		return nil, nil, fmt.Errorf("parse private key: %w", err)
	}
	rsaKey, ok := key8.(*rsa.PrivateKey)
	if !ok {
		return nil, nil, fmt.Errorf("parse private key: not an RSA key")
	}
	return cert, rsaKey, nil
}
