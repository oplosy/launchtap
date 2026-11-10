package apiserver

import (
	"context"
	"net/http"

	"github.com/Contictus/launchtap/backend/internal/privyauth"
)

// authenticatePrivy parses the credential headers and verifies them. Every failure, including a
// missing header and a DID that is empty or longer than maxPrivyDIDBytes, is the same 401
// authentication_failed so a caller cannot tell the causes apart. Callers check their own
// availability (store, verifier) first.
func authenticatePrivy(ctx context.Context, v privyauth.Verifier, in WatchlistAuthInput) (privyauth.Principal, error) {
	access, err := privyauth.ParseBearer(in.Authorization)
	if err != nil {
		return privyauth.Principal{}, apiProblem(http.StatusUnauthorized, "authentication_failed", "Authentication failed")
	}
	principal, err := v.Verify(ctx, access, in.IdentityToken)
	if err != nil || principal.PrivyDID == "" || len(principal.PrivyDID) > maxPrivyDIDBytes {
		return privyauth.Principal{}, apiProblem(http.StatusUnauthorized, "authentication_failed", "Authentication failed")
	}
	return principal, nil
}
