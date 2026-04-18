// Package oauth — refresh.go owns the token-refresh retry pipeline.
// Task anchors `refresh`, `token`, `retry` all land here.
package oauth

import (
	"errors"
	"time"
)

// ErrRefreshFailed is returned by RefreshToken when retries are
// exhausted without a successful response.
var ErrRefreshFailed = errors.New("refresh failed after retries")

// RefreshToken attempts to refresh an OAuth token. The regression
// described in the task is that the retry loop here isn't firing
// when the network returns a transient 5xx — RefreshToken loops up
// to maxRetries times with exponential backoff.
func RefreshToken(maxRetries int) error {
	for attempt := 0; attempt < maxRetries; attempt++ {
		err := doRefresh()
		if err == nil {
			return nil
		}
		if !isRetryable(err) {
			return err
		}
		time.Sleep(time.Duration(attempt+1) * 100 * time.Millisecond)
	}
	return ErrRefreshFailed
}

// doRefresh is the single-shot refresh call. Stubbed for the fixture.
func doRefresh() error { return errors.New("transient") }

// isRetryable classifies an error as retryable. Deliberately simple.
func isRetryable(err error) bool { return err != nil }
