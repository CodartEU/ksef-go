// Package auth provides authentication mechanisms for the KSeF API.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"

	"github.com/CodartEU/ksef-go/internal/httpclient"
)

// TokenAuthenticator authenticates with the KSeF API using a KSeF token.
//
// The authentication flow is:
//  1. POST /auth/challenge — obtain a one-time challenge and timestamp.
//  2. POST /auth/ksef-token — submit the token encrypted with the KSeF public
//     key (RSA-OAEP / SHA-256, format: "token|timestampMs").
//  3. GET  /auth/{referenceNumber} — poll until status 200 (success).
//  4. POST /auth/token/redeem — exchange the operation token for access and
//     refresh tokens.
type TokenAuthenticator struct {
	http         *httpclient.Client
	publicKey    *rsa.PublicKey
	pollInterval time.Duration
	pollTimeout  time.Duration
}

// NewTokenAuthenticator creates a TokenAuthenticator.
//
//   - hc is the internal HTTP client already configured with the correct base
//     URL and retry policy.
//   - publicKey is the KSeF environment's RSA public key used to encrypt the
//     token payload before submission.
func NewTokenAuthenticator(hc *httpclient.Client, publicKey *rsa.PublicKey) *TokenAuthenticator {
	return &TokenAuthenticator{
		http:         hc,
		publicKey:    publicKey,
		pollInterval: time.Second,
		pollTimeout:  30 * time.Second,
	}
}

// AuthResult holds the tokens returned after a successful KSeF authentication.
type AuthResult struct {
	// AccessToken is the JWT access token used to authorise API calls.
	AccessToken string
	// AccessTokenValidUntil is the expiry time of AccessToken.
	AccessTokenValidUntil time.Time
	// RefreshToken is the JWT token used to obtain a new AccessToken without
	// re-authenticating from scratch.
	RefreshToken string
	// RefreshTokenValidUntil is the expiry time of RefreshToken.
	RefreshTokenValidUntil time.Time
	// ReferenceNumber is the KSeF correlation identifier for the authentication
	// operation.
	ReferenceNumber string
}

// Authenticate executes the full KSeF token authentication flow and returns
// the resulting tokens on success.
//
// nip is the Polish tax identifier (NIP) of the entity being authenticated.
// token is the KSeF-issued API token secret.
func (a *TokenAuthenticator) Authenticate(ctx context.Context, nip, token string) (*AuthResult, error) {
	// Step 1: obtain challenge.
	ch, err := a.getChallenge(ctx)
	if err != nil {
		return nil, fmt.Errorf("auth/token: get challenge: %w", err)
	}

	// Step 2: encrypt token payload.
	encToken, err := encryptToken(a.publicKey, token, ch.TimestampMs)
	if err != nil {
		return nil, fmt.Errorf("auth/token: encrypt token: %w", err)
	}

	// Step 3: submit token auth request.
	initResp, err := a.submitToken(ctx, nip, ch.Challenge, encToken)
	if err != nil {
		return nil, fmt.Errorf("auth/token: submit token: %w", err)
	}

	// Step 4: poll until authentication completes.
	if err := a.pollUntilDone(ctx, initResp.ReferenceNumber, initResp.AuthenticationToken.Token); err != nil {
		return nil, fmt.Errorf("auth/token: poll status: %w", err)
	}

	// Step 5: redeem operation token for access + refresh tokens.
	tokens, err := a.redeemTokens(ctx, initResp.AuthenticationToken.Token)
	if err != nil {
		return nil, fmt.Errorf("auth/token: redeem tokens: %w", err)
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

// challengeResponse mirrors the POST /auth/challenge response body.
type challengeResponse struct {
	Challenge   string    `json:"challenge"`
	Timestamp   time.Time `json:"timestamp"`
	TimestampMs int64     `json:"timestampMs"`
}

// initTokenRequest mirrors the POST /auth/ksef-token request body.
type initTokenRequest struct {
	Challenge         string            `json:"challenge"`
	ContextIdentifier contextIdentifier `json:"contextIdentifier"`
	EncryptedToken    string            `json:"encryptedToken"`
}

// contextIdentifier identifies the entity context for an auth request.
type contextIdentifier struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

// authInitResponse mirrors the POST /auth/ksef-token 202 response body.
type authInitResponse struct {
	ReferenceNumber     string    `json:"referenceNumber"`
	AuthenticationToken tokenInfo `json:"authenticationToken"`
}

// tokenInfo mirrors the TokenInfo schema (used for both operation and access tokens).
type tokenInfo struct {
	Token      string    `json:"token"`
	ValidUntil time.Time `json:"validUntil"`
}

// authStatusResponse mirrors the GET /auth/{referenceNumber} response body.
type authStatusResponse struct {
	Status statusInfo `json:"status"`
}

// statusInfo mirrors the StatusInfo schema.
type statusInfo struct {
	Code        int32    `json:"code"`
	Description string   `json:"description"`
	Details     []string `json:"details"`
}

// authTokensResponse mirrors the POST /auth/token/redeem response body.
type authTokensResponse struct {
	AccessToken  tokenInfo `json:"accessToken"`
	RefreshToken tokenInfo `json:"refreshToken"`
}

func (a *TokenAuthenticator) getChallenge(ctx context.Context) (*challengeResponse, error) {
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

func (a *TokenAuthenticator) submitToken(ctx context.Context, nip, challenge, encryptedToken string) (*authInitResponse, error) {
	body := initTokenRequest{
		Challenge:         challenge,
		ContextIdentifier: contextIdentifier{Type: "Nip", Value: nip},
		EncryptedToken:    encryptedToken,
	}
	raw, err := a.http.Post(ctx, "/auth/ksef-token", body, nil)
	if err != nil {
		return nil, err
	}
	var resp authInitResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &resp, nil
}

func (a *TokenAuthenticator) pollUntilDone(ctx context.Context, referenceNumber, authToken string) error {
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

func (a *TokenAuthenticator) redeemTokens(ctx context.Context, authToken string) (*authTokensResponse, error) {
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

// encryptToken encrypts the token payload using RSA-OAEP with SHA-256.
// The plaintext format is "token|timestampMs" as required by the KSeF API.
// The result is Base64 (standard encoding) encoded.
func encryptToken(pub *rsa.PublicKey, token string, timestampMs int64) (string, error) {
	plaintext := fmt.Sprintf("%s|%d", token, timestampMs)
	ciphertext, err := rsa.EncryptOAEP(sha256.New(), rand.Reader, pub, []byte(plaintext), nil)
	if err != nil {
		return "", fmt.Errorf("RSA-OAEP encrypt: %w", err)
	}
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// sleepContext sleeps for d, returning early if ctx is cancelled.
func sleepContext(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
