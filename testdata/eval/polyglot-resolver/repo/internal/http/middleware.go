// Package http hosts the HTTP middleware stack. The fixture task
// deliberately names "rate-limit header" — a v1.0 (Go-only) planner
// picks this file because "RateLimitMiddleware" is an exported Go
// symbol and the task text contains the substring. v1.1's tier-2
// support surfaces the TypeScript resolver as the real answer.
package http

// RateLimitMiddleware adds a rate-limit header to the response.
type RateLimitMiddleware struct {
	Limit int
}

// Wrap attaches the rate-limit header; a Go-side concern only.
func (m *RateLimitMiddleware) Wrap(next string) string {
	return "X-RateLimit-Limit: " + next
}
