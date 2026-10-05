// Package privyauth verifies Privy access and identity tokens locally.
package privyauth

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
)

const (
	maxTokenBytes   = 16 << 10
	maxHeaderBytes  = 2 << 10
	maxClaimsBytes  = 32 << 10
	maxAccountsJSON = 24 << 10

	// maxClockSkew tolerates a server clock slightly behind Privy's for freshly issued tokens.
	// It applies only to iat/nbf; expiry stays strict.
	maxClockSkew int64 = 30
)

var ErrInvalidCredentials = errors.New("invalid Privy credentials")

type Principal struct {
	PrivyDID string
	Wallets  []common.Address
}

type Verifier interface {
	Verify(context.Context, string, string) (Principal, error)
}

type ES256Verifier struct {
	appID string
	key   *ecdsa.PublicKey
	now   func() time.Time
}

func NewES256Verifier(appID, serializedKey string) (*ES256Verifier, error) {
	if strings.TrimSpace(appID) == "" {
		return nil, errors.New("Privy app ID is required") //nolint:staticcheck // Privy is a proper name.
	}
	key, err := parsePublicKey(serializedKey)
	if err != nil {
		return nil, fmt.Errorf("parse Privy verification key: %w", err)
	}
	return &ES256Verifier{appID: appID, key: key, now: time.Now}, nil
}

func (v *ES256Verifier) Verify(_ context.Context, accessToken, identityToken string) (Principal, error) {
	access, err := v.verifyJWT(accessToken)
	if err != nil {
		return Principal{}, invalid(err)
	}
	identity, err := v.verifyJWT(identityToken)
	if err != nil {
		return Principal{}, invalid(err)
	}
	accessSubject, err := requiredString(access, "sub")
	if err != nil {
		return Principal{}, invalid(err)
	}
	identitySubject, err := requiredString(identity, "sub")
	if err != nil || accessSubject != identitySubject {
		return Principal{}, invalid(errors.New("token subjects do not match"))
	}
	accountsValue, ok := identity["linked_accounts"]
	if !ok {
		return Principal{}, invalid(errors.New("linked_accounts is missing"))
	}
	var encodedAccounts string
	if err := json.Unmarshal(accountsValue, &encodedAccounts); err != nil || len(encodedAccounts) > maxAccountsJSON {
		return Principal{}, invalid(errors.New("linked_accounts is invalid"))
	}
	wallets, err := parseWallets([]byte(encodedAccounts))
	if err != nil {
		return Principal{}, invalid(err)
	}
	return Principal{PrivyDID: accessSubject, Wallets: wallets}, nil
}

func (v *ES256Verifier) verifyJWT(token string) (map[string]json.RawMessage, error) {
	if token == "" || len(token) > maxTokenBytes || strings.TrimSpace(token) != token {
		return nil, errors.New("token size or whitespace is invalid")
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 || strings.Contains(token, "=") {
		return nil, errors.New("token compact form is invalid")
	}
	header, err := decodePart(parts[0], maxHeaderBytes)
	if err != nil {
		return nil, err
	}
	claimsBytes, err := decodePart(parts[1], maxClaimsBytes)
	if err != nil {
		return nil, err
	}
	signature, err := decodePart(parts[2], 64)
	if err != nil || len(signature) != 64 {
		return nil, errors.New("ES256 signature is invalid")
	}
	var headerClaims map[string]json.RawMessage
	if err := decodeStrict(header, &headerClaims); err != nil {
		return nil, err
	}
	algorithm, err := requiredString(headerClaims, "alg")
	if err != nil || algorithm != "ES256" {
		return nil, errors.New("JWT algorithm is not ES256")
	}
	if _, ok := headerClaims["crit"]; ok {
		return nil, errors.New("critical JWT extensions are unsupported")
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	r := new(big.Int).SetBytes(signature[:32])
	s := new(big.Int).SetBytes(signature[32:])
	if !ecdsa.Verify(v.key, digest[:], r, s) {
		return nil, errors.New("JWT signature verification failed")
	}
	var claims map[string]json.RawMessage
	if err := decodeStrict(claimsBytes, &claims); err != nil {
		return nil, err
	}
	issuer, err := requiredString(claims, "iss")
	if err != nil || issuer != "privy.io" {
		return nil, errors.New("JWT issuer is invalid")
	}
	audience, err := requiredString(claims, "aud")
	if err != nil || audience != v.appID {
		return nil, errors.New("JWT audience is invalid")
	}
	if subject, err := requiredString(claims, "sub"); err != nil || strings.TrimSpace(subject) == "" {
		return nil, errors.New("JWT subject is invalid")
	}
	now := v.now().Unix()
	issuedAt, err := requiredInteger(claims, "iat")
	if err != nil || issuedAt > now+maxClockSkew {
		return nil, errors.New("JWT issued-at time is invalid")
	}
	expiresAt, err := requiredInteger(claims, "exp")
	if err != nil || expiresAt <= now {
		return nil, errors.New("JWT is expired")
	}
	if raw, ok := claims["nbf"]; ok {
		notBefore, err := integer(raw)
		if err != nil || notBefore > now+maxClockSkew {
			return nil, errors.New("JWT is not yet valid")
		}
	}
	return claims, nil
}

func ParseBearer(value string) (string, error) {
	if len(value) > maxTokenBytes+7 || !strings.HasPrefix(value, "Bearer ") {
		return "", ErrInvalidCredentials
	}
	token := strings.TrimPrefix(value, "Bearer ")
	if token == "" || strings.ContainsAny(token, " \t\r\n,") {
		return "", ErrInvalidCredentials
	}
	return token, nil
}

func parseWallets(data []byte) ([]common.Address, error) {
	var accounts []map[string]json.RawMessage
	if err := decodeStrict(data, &accounts); err != nil {
		return nil, errors.New("linked accounts JSON is invalid")
	}
	seen := make(map[common.Address]struct{})
	wallets := make([]common.Address, 0, len(accounts))
	for _, account := range accounts {
		kind, err := requiredString(account, "type")
		if err != nil {
			return nil, errors.New("linked account type is invalid")
		}
		if kind != "wallet" && kind != "smart_wallet" {
			continue
		}
		if raw, ok := account["linked"]; ok {
			var linked bool
			if err := json.Unmarshal(raw, &linked); err != nil {
				return nil, errors.New("linked account flag is invalid")
			}
			if !linked {
				continue
			}
		}
		if raw, ok := account["chain_type"]; ok {
			var chainType string
			if err := json.Unmarshal(raw, &chainType); err != nil {
				return nil, errors.New("linked account chain type is invalid")
			}
			if chainType != "ethereum" {
				continue
			}
		}
		addressText, err := requiredString(account, "address")
		if err != nil || !common.IsHexAddress(addressText) {
			return nil, errors.New("linked EVM wallet address is invalid")
		}
		address := common.HexToAddress(addressText)
		if _, exists := seen[address]; exists {
			return nil, errors.New("duplicate linked wallet")
		}
		seen[address] = struct{}{}
		wallets = append(wallets, address)
	}
	return wallets, nil
}

func parsePublicKey(value string) (*ecdsa.PublicKey, error) {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "{") {
		var jwk struct {
			KTY string `json:"kty"`
			CRV string `json:"crv"`
			X   string `json:"x"`
			Y   string `json:"y"`
		}
		if err := decodeStrict([]byte(value), &jwk); err != nil {
			return nil, err
		}
		if jwk.KTY != "EC" || jwk.CRV != "P-256" {
			return nil, errors.New("verification JWK must be EC P-256")
		}
		x, errX := base64.RawURLEncoding.DecodeString(jwk.X)
		y, errY := base64.RawURLEncoding.DecodeString(jwk.Y)
		if errX != nil || errY != nil || len(x) != 32 || len(y) != 32 {
			return nil, errors.New("verification JWK coordinates are invalid")
		}
		encoded := make([]byte, 1+len(x)+len(y))
		encoded[0] = 4
		copy(encoded[1:], x)
		copy(encoded[1+len(x):], y)
		key, err := ecdsa.ParseUncompressedPublicKey(elliptic.P256(), encoded)
		if err != nil {
			return nil, errors.New("verification JWK is not on P-256")
		}
		return key, nil
	}
	if !strings.Contains(value, "\n") && strings.Contains(value, `\n`) {
		value = strings.ReplaceAll(value, `\n`, "\n")
	}
	block, rest := pem.Decode([]byte(value))
	if block == nil || len(bytes.TrimSpace(rest)) != 0 {
		return nil, errors.New("verification key must be one PEM block or a JWK")
	}
	var parsed any
	var err error
	switch block.Type {
	case "PUBLIC KEY":
		parsed, err = x509.ParsePKIXPublicKey(block.Bytes)
	case "CERTIFICATE":
		var certificate *x509.Certificate
		certificate, err = x509.ParseCertificate(block.Bytes)
		if err == nil {
			parsed = certificate.PublicKey
		}
	default:
		return nil, errors.New("private or unsupported PEM block")
	}
	if err != nil {
		return nil, err
	}
	key, ok := parsed.(*ecdsa.PublicKey)
	if !ok || key.Curve != elliptic.P256() {
		return nil, errors.New("verification key must be ECDSA P-256")
	}
	return key, nil
}

func decodePart(value string, maximum int) ([]byte, error) {
	if value == "" || len(value) > maximum*2 {
		return nil, errors.New("JWT section size is invalid")
	}
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(decoded) > maximum {
		return nil, errors.New("JWT section encoding is invalid")
	}
	return decoded, nil
}

func decodeStrict(data []byte, target any) error {
	if err := rejectDuplicateKeys(data); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	decoder.UseNumber()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("trailing JSON value")
		}
		return err
	}
	return nil
}

func rejectDuplicateKeys(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := scanJSONValue(decoder); err != nil {
		return err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("trailing JSON token")
		}
		return err
	}
	return nil
}

func scanJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]struct{}{}
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return errors.New("JSON object key is invalid")
			}
			if _, exists := seen[key]; exists {
				return fmt.Errorf("duplicate JSON key %q", key)
			}
			seen[key] = struct{}{}
			if err := scanJSONValue(decoder); err != nil {
				return err
			}
		}
	case '[':
		for decoder.More() {
			if err := scanJSONValue(decoder); err != nil {
				return err
			}
		}
	default:
		return errors.New("unexpected JSON delimiter")
	}
	_, err = decoder.Token()
	return err
}

func requiredString(claims map[string]json.RawMessage, name string) (string, error) {
	raw, ok := claims[name]
	if !ok {
		return "", fmt.Errorf("%s is missing", name)
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil || value == "" {
		return "", fmt.Errorf("%s is invalid", name)
	}
	return value, nil
}

func requiredInteger(claims map[string]json.RawMessage, name string) (int64, error) {
	raw, ok := claims[name]
	if !ok {
		return 0, fmt.Errorf("%s is missing", name)
	}
	return integer(raw)
}

func integer(raw json.RawMessage) (int64, error) {
	var number json.Number
	if err := json.Unmarshal(raw, &number); err != nil {
		return 0, err
	}
	return number.Int64()
}

func invalid(cause error) error { return fmt.Errorf("%w: %v", ErrInvalidCredentials, cause) }
