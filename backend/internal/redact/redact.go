// Package redact renders errors for logs without URLs: provider API keys often sit in the RPC
// URL path or query, which *url.Error prints in full.
package redact

import (
	"errors"
	"net/url"
	"regexp"
	"strings"
)

// Placeholder replaces every URL in a redacted message.
const Placeholder = "<rpc-url>"

var urlPattern = regexp.MustCompile(`[A-Za-z][A-Za-z0-9+.-]*://[^\s"']+`)

// Error renders err without any URL. Exact URLs (the given secrets and every *url.Error in the
// chain) are replaced first, then any remaining URL-looking text.
func Error(err error, secrets ...string) string {
	if err == nil {
		return ""
	}
	message := err.Error()
	exact := append([]string(nil), secrets...)
	for current := err; current != nil; current = errors.Unwrap(current) {
		var urlErr *url.Error
		if errors.As(current, &urlErr) {
			exact = append(exact, urlErr.URL)
		}
	}
	for _, secret := range exact {
		if strings.TrimSpace(secret) != "" {
			message = strings.ReplaceAll(message, secret, Placeholder)
		}
	}
	return urlPattern.ReplaceAllString(message, Placeholder)
}
