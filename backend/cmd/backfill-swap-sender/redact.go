package main

import (
	"errors"
	"net/url"
	"regexp"
	"strings"
)

const redactedURL = "<rpc-url>"

var urlPattern = regexp.MustCompile(`[A-Za-z][A-Za-z0-9+.-]*://[^\s"']+`)

// redactError renders err for logs without any URL: provider API keys often sit in the RPC URL
// path or query, which *url.Error prints in full. Exact URLs (the configured ones and every
// *url.Error in the chain) are replaced first, then any remaining URL-looking text.
func redactError(err error, secrets ...string) string {
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
			message = strings.ReplaceAll(message, secret, redactedURL)
		}
	}
	return urlPattern.ReplaceAllString(message, redactedURL)
}
