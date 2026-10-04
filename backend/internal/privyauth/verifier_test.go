package privyauth

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"math/big"
	"strings"
	"testing"
	"time"
)

func TestVerifierAcceptsMatchingTokensAndOnlyLinkedEVMWallets(t *testing.T) {
	verifier, private := testVerifier(t)
	now := verifier.now().Unix()
	accounts, _ := json.Marshal([]map[string]any{
		{"type": "wallet", "chain_type": "ethereum", "address": "0x00000000000000000000000000000000000000AA"},
		{"type": "smart_wallet", "address": "0x00000000000000000000000000000000000000bb"},
		{"type": "wallet", "chain_type": "solana", "address": "not-an-evm-address"},
		{"type": "wallet", "chain_type": "ethereum", "address": "0x00000000000000000000000000000000000000cc", "linked": false},
	})
	access := signClaims(t, private, "ES256", baseClaims(now, "did:privy:user"))
	identityClaims := baseClaims(now, "did:privy:user")
	identityClaims["linked_accounts"] = string(accounts)
	identity := signClaims(t, private, "ES256", identityClaims)
	principal, err := verifier.Verify(context.Background(), access, identity)
	if err != nil {
		t.Fatal(err)
	}
	if principal.PrivyDID != "did:privy:user" || len(principal.Wallets) != 2 || principal.Wallets[0].Hex() != "0x00000000000000000000000000000000000000AA" {
		t.Fatalf("principal=%+v", principal)
	}
}

func TestVerifierToleratesBoundedClockSkewForFreshTokens(t *testing.T) {
	verifier, private := testVerifier(t)
	now := verifier.now().Unix()
	accessClaims := baseClaims(now, "did:privy:user")
	accessClaims["iat"] = now + maxClockSkew
	accessClaims["nbf"] = now + maxClockSkew
	identityClaims := baseClaims(now, "did:privy:user")
	identityClaims["linked_accounts"] = `[]`
	if _, err := verifier.Verify(context.Background(), signClaims(t, private, "ES256", accessClaims), signClaims(t, private, "ES256", identityClaims)); err != nil {
		t.Fatalf("token issued within clock skew rejected: %v", err)
	}
}

func TestVerifierRejectsInvalidTokenConditions(t *testing.T) {
	verifier, private := testVerifier(t)
	now := verifier.now().Unix()
	accounts := `[{"type":"wallet","chain_type":"ethereum","address":"0x00000000000000000000000000000000000000aa"}]`
	validAccessClaims := baseClaims(now, "did:privy:user")
	validIdentityClaims := baseClaims(now, "did:privy:user")
	validIdentityClaims["linked_accounts"] = accounts
	validAccess := signClaims(t, private, "ES256", validAccessClaims)
	validIdentity := signClaims(t, private, "ES256", validIdentityClaims)

	tests := []struct {
		name     string
		access   func() string
		identity func() string
	}{
		{"wrong algorithm", func() string { return signClaims(t, private, "HS256", validAccessClaims) }, func() string { return validIdentity }},
		{"invalid signature", func() string { return corruptSignature(t, validAccess) }, func() string { return validIdentity }},
		{"expired", func() string {
			c := cloneClaims(validAccessClaims)
			c["exp"] = now
			return signClaims(t, private, "ES256", c)
		}, func() string { return validIdentity }},
		{"not yet valid", func() string {
			c := cloneClaims(validAccessClaims)
			c["nbf"] = now + maxClockSkew + 1
			return signClaims(t, private, "ES256", c)
		}, func() string { return validIdentity }},
		{"issued beyond clock skew", func() string {
			c := cloneClaims(validAccessClaims)
			c["iat"] = now + maxClockSkew + 1
			return signClaims(t, private, "ES256", c)
		}, func() string { return validIdentity }},
		{"wrong issuer", func() string {
			c := cloneClaims(validAccessClaims)
			c["iss"] = "other"
			return signClaims(t, private, "ES256", c)
		}, func() string { return validIdentity }},
		{"wrong audience", func() string {
			c := cloneClaims(validAccessClaims)
			c["aud"] = "other"
			return signClaims(t, private, "ES256", c)
		}, func() string { return validIdentity }},
		{"missing subject", func() string {
			c := cloneClaims(validAccessClaims)
			delete(c, "sub")
			return signClaims(t, private, "ES256", c)
		}, func() string { return validIdentity }},
		{"subject mismatch", func() string { return validAccess }, func() string {
			c := cloneClaims(validIdentityClaims)
			c["sub"] = "did:privy:other"
			return signClaims(t, private, "ES256", c)
		}},
		{"malformed linked account", func() string { return validAccess }, func() string {
			c := cloneClaims(validIdentityClaims)
			c["linked_accounts"] = `[{"type":"wallet","chain_type":"ethereum"}]`
			return signClaims(t, private, "ES256", c)
		}},
		{"duplicate wallet", func() string { return validAccess }, func() string {
			c := cloneClaims(validIdentityClaims)
			c["linked_accounts"] = `[{"type":"wallet","address":"0x00000000000000000000000000000000000000aa"},{"type":"smart_wallet","address":"0x00000000000000000000000000000000000000AA"}]`
			return signClaims(t, private, "ES256", c)
		}},
		{"duplicate claim", func() string {
			return signRaw(t, private, `{"alg":"ES256"}`, `{"iss":"privy.io","iss":"privy.io","aud":"test-app","sub":"did:privy:user","iat":`+itoa(now-1)+`,"exp":`+itoa(now+60)+`}`)
		}, func() string { return validIdentity }},
		{"oversize", func() string { return strings.Repeat("a", maxTokenBytes+1) }, func() string { return validIdentity }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := verifier.Verify(context.Background(), test.access(), test.identity()); !errors.Is(err, ErrInvalidCredentials) {
				t.Fatalf("error=%v", err)
			}
		})
	}
}

func corruptSignature(t *testing.T, token string) string {
	t.Helper()
	parts := strings.Split(token, ".")
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || len(signature) != 64 {
		t.Fatalf("decode test signature: length=%d err=%v", len(signature), err)
	}
	signature[0] ^= 0x01
	parts[2] = base64.RawURLEncoding.EncodeToString(signature)
	return strings.Join(parts, ".")
}

func TestPublicKeyFormatsAndBearerSyntax(t *testing.T) {
	_, private := testVerifier(t)
	encoded, err := private.PublicKey.Bytes()
	if err != nil || len(encoded) != 65 {
		t.Fatalf("encode public key: length=%d err=%v", len(encoded), err)
	}
	x, y := encoded[1:33], encoded[33:]
	jwk, _ := json.Marshal(map[string]string{"kty": "EC", "crv": "P-256", "x": base64.RawURLEncoding.EncodeToString(x), "y": base64.RawURLEncoding.EncodeToString(y)})
	if _, err := NewES256Verifier("app", string(jwk)); err != nil {
		t.Fatalf("JWK: %v", err)
	}
	der, _ := x509.MarshalPKCS8PrivateKey(private)
	privatePEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
	if _, err := NewES256Verifier("app", string(privatePEM)); err == nil {
		t.Fatal("private key accepted")
	}
	for _, value := range []string{"", "bearer token", "Bearer", "Bearer a b", "Bearer a,b"} {
		if _, err := ParseBearer(value); !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("accepted bearer %q", value)
		}
	}
	if token, err := ParseBearer("Bearer a.b.c"); err != nil || token != "a.b.c" {
		t.Fatalf("valid bearer: %q %v", token, err)
	}
}

func testVerifier(t *testing.T) (*ES256Verifier, *ecdsa.PrivateKey) {
	t.Helper()
	private, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKIXPublicKey(&private.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	serialized := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})
	verifier, err := NewES256Verifier("test-app", string(serialized))
	if err != nil {
		t.Fatal(err)
	}
	verifier.now = func() time.Time { return time.Unix(2_000_000_000, 0) }
	return verifier, private
}

func baseClaims(now int64, subject string) map[string]any {
	return map[string]any{"iss": "privy.io", "aud": "test-app", "sub": subject, "iat": now - 1, "exp": now + 60}
}

func cloneClaims(input map[string]any) map[string]any {
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func signClaims(t *testing.T, private *ecdsa.PrivateKey, algorithm string, claims map[string]any) string {
	t.Helper()
	header, _ := json.Marshal(map[string]any{"alg": algorithm, "typ": "JWT"})
	payload, _ := json.Marshal(claims)
	return signRaw(t, private, string(header), string(payload))
}

func signRaw(t *testing.T, private *ecdsa.PrivateKey, header, payload string) string {
	t.Helper()
	encodedHeader := base64.RawURLEncoding.EncodeToString([]byte(header))
	encodedPayload := base64.RawURLEncoding.EncodeToString([]byte(payload))
	digest := sha256.Sum256([]byte(encodedHeader + "." + encodedPayload))
	r, s, err := ecdsa.Sign(rand.Reader, private, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	signature := append(r.FillBytes(make([]byte, 32)), s.FillBytes(make([]byte, 32))...)
	return encodedHeader + "." + encodedPayload + "." + base64.RawURLEncoding.EncodeToString(signature)
}

func itoa(value int64) string { return new(big.Int).SetInt64(value).String() }
