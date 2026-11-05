// Package pricefeed polls an external ETH/USD reference price. It is stdlib-only
// and never influences on-chain amounts: the price is an external decimal string.
package pricefeed

import (
	"encoding/json"
	"errors"
	"regexp"
	"strings"
)

// canonicalPrice accepts a plain positive decimal: no sign, no exponent, no
// leading zeros, and at most 18 fractional digits.
var canonicalPrice = regexp.MustCompile(`^(0|[1-9][0-9]*)(\.[0-9]{1,18})?$`)

var errInvalidPrice = errors.New("pricefeed: invalid price")

// ParsePrice validates the JSON number lexeme and returns it unchanged as the
// canonical decimal. It never converts through a float.
func ParsePrice(n json.Number) (string, error) {
	s := string(n)
	if !canonicalPrice.MatchString(s) {
		return "", errInvalidPrice
	}
	if strings.Trim(s, "0.") == "" {
		return "", errInvalidPrice
	}
	return s, nil
}
