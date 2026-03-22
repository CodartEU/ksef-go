// Package ksef provides a Go client for Poland's National e-Invoicing System
// (Krajowy System e-Faktur) 2.0 API.
package ksef

// Environment identifies the KSeF deployment to connect to.
type Environment int

const (
	// Test is the KSeF test environment. Use this during development.
	Test Environment = iota
	// Demo is the KSeF demo environment.
	Demo
	// Production is the live KSeF production environment.
	Production
)

// baseURLs maps each environment to its API v2 base URL.
var baseURLs = map[Environment]string{
	Test:       "https://api-test.ksef.mf.gov.pl/v2",
	Demo:       "https://api-demo.ksef.mf.gov.pl/v2",
	Production: "https://api.ksef.mf.gov.pl/v2",
}

// BaseURL returns the root URL for the environment's API, without a trailing
// slash. It panics if env is not a recognised Environment value.
func (env Environment) BaseURL() string {
	u, ok := baseURLs[env]
	if !ok {
		panic("ksef: unknown environment")
	}
	return u
}

// String returns a human-readable name for the environment.
func (env Environment) String() string {
	switch env {
	case Test:
		return "Test"
	case Demo:
		return "Demo"
	case Production:
		return "Production"
	default:
		return "Unknown"
	}
}
