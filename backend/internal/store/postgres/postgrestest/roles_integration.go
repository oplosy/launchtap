//go:build integration

package postgrestest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/url"
	"testing"
)

// Service group roles created by migration 00039 (docs/specs/2026-11-16-postgres-service-roles-preflight.md).
const (
	APIRole       = "launchpad_api_role"
	IndexerRole   = "launchpad_indexer_role"
	TelegramRole  = "launchpad_telegram_role"
	FundingRole   = "launchpad_funding_role"
	ModerateRole  = "launchpad_moderate_role"
	roleLoginBase = "launchpad_test_login_"
)

// URLAs returns a URL for this migrated database that logs in as a throwaway member of the given
// service group role, so a store test fails on a missing grant instead of running as superuser.
func (d *Database) URLAs(t testing.TB, groupRole string) string {
	t.Helper()
	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatalf("create login role name: %v", err)
	}
	login := roleLoginBase + hex.EncodeToString(suffix[:])
	password := hex.EncodeToString(suffix[:]) + "-test-only"
	ctx, cancel := context.WithTimeout(context.Background(), databaseOperationTimeout)
	defer cancel()
	// Identifiers come from constants and random hex; the password is random hex.
	statement := fmt.Sprintf(`CREATE ROLE %s LOGIN PASSWORD '%s' IN ROLE %s`, login, password, groupRole)
	if _, err := d.DB.ExecContext(ctx, statement); err != nil {
		t.Fatalf("create login role in %s: %v", groupRole, err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), databaseOperationTimeout)
		defer cancel()
		// Best effort: pools close in earlier cleanups; a leftover login role owns nothing.
		_, _ = d.DB.ExecContext(ctx, `DROP ROLE IF EXISTS `+login)
	})
	parsed, err := url.Parse(d.URL)
	if err != nil {
		t.Fatalf("parse database URL: %v", err)
	}
	parsed.User = url.UserPassword(login, password)
	return parsed.String()
}
