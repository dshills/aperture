// Package oauth holds a minimal provider used by the Aperture fixture
// to exercise AST extraction of every exported-symbol kind.
package oauth

import (
	"errors"
	"net/http"
	"time"
)

// ErrExpired is returned when a token refresh is attempted past the
// expiry horizon.
var ErrExpired = errors.New("token expired")

// DefaultTimeout is the HTTP client timeout applied to refresh calls.
const DefaultTimeout = 30 * time.Second

// Provider describes the subset of provider behavior the fixture app
// exercises. It is deliberately small to keep AST tests fast.
type Provider interface {
	Name() string
	RefreshToken(oldToken string) (string, error)
}

// GitHubProvider implements Provider for GitHub.
type GitHubProvider struct {
	label  string
	client *http.Client
}

// NewProvider constructs a GitHubProvider with the given label.
func NewProvider(label string) *GitHubProvider {
	return &GitHubProvider{label: label, client: &http.Client{Timeout: DefaultTimeout}}
}

// Name returns the label assigned at construction time.
func (p *GitHubProvider) Name() string { return p.label }

// RefreshToken is a stub that always returns ErrExpired. The fixture
// does not actually perform network I/O.
func (p *GitHubProvider) RefreshToken(oldToken string) (string, error) {
	return "", ErrExpired
}
