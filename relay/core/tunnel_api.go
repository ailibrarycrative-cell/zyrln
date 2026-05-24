package core

import (
	"context"
	"net/http"
	"time"
)

// AppsScriptRoundTrip POSTs a JSON payload to a domain-fronted Apps Script URL.
func AppsScriptRoundTrip(ctx context.Context, client *http.Client, appScriptURL, frontDomain, payload string, timeout time.Duration) ([]byte, error) {
	return appsScriptRoundTrip(ctx, client, appScriptURL, frontDomain, payload, timeout)
}

// BuildRelayPayload builds the desktop relay JSON body (also used for tunnel keepalive HEAD).
func BuildRelayPayload(authKey, method, targetURL string, headers map[string]string, body []byte) string {
	return buildRelayPayload(authKey, method, targetURL, headers, body)
}

// TryOneURL executes one relay request and decodes the worker JSON response.
func TryOneURL(ctx context.Context, client *http.Client, appScriptURL, frontDomain, payload string, timeout time.Duration) (RelayResponse, error) {
	return tryOneURL(ctx, client, appScriptURL, frontDomain, payload, timeout)
}

// PreviewBytes returns a truncated string preview of raw bytes.
func PreviewBytes(b []byte, max int) string {
	return previewBytes(b, max)
}

// PerURLTimeout splits a total timeout across n parallel URL attempts (minimum 3s each).
func PerURLTimeout(total time.Duration, n int) time.Duration {
	if n <= 1 {
		return total
	}
	d := total / time.Duration(n)
	if d < 3*time.Second {
		return 3 * time.Second
	}
	return d
}
