//go:build integration

package postgrestest

import (
	"context"
	"database/sql"
	"slices"
	"testing"
	"time"
)

// userContent are the tables only user-facing processes write; the indexer never does.
var userContent = []string{
	"social_users", "token_comments", "comment_votes", "comment_reports", "social_public_wallets",
	"username_releases", "token_metadata", "token_images", "watchlist_entries", "telegram_link_codes",
	"telegram_links", "moderation_actions", "wallet_funding",
}

// writers names, for every table that is not chain-derived, the service roles allowed to write it
// table-wide. A chain-derived table is written by the indexer alone. Adding a table means
// classifying it here and granting it in its migration
// (docs/specs/2026-11-16-postgres-service-roles-preflight.md).
var writers = map[string][]string{
	"social_users":             {ModerateRole},
	"token_comments":           {ModerateRole},
	"comment_votes":            {APIRole},
	"comment_reports":          {ModerateRole},
	"social_public_wallets":    {APIRole},
	"username_releases":        {APIRole},
	"token_metadata":           {APIRole},
	"token_images":             {APIRole},
	"watchlist_entries":        {APIRole},
	"notification_reads":       {APIRole, IndexerRole},
	"telegram_link_codes":      {APIRole, TelegramRole},
	"telegram_links":           {APIRole, TelegramRole},
	"moderation_actions":       {ModerateRole},
	"wallet_funding":           {FundingRole},
	"funding_excluded_funders": {},
}

// chainDerived lists every table the indexer alone writes. A new table that is in neither list
// fails the test until someone decides who may write it.
var chainDerived = []string{
	"aggregation_dirty", "candles", "creator_fee_balances", "creator_fee_claims",
	"engine_configurations", "fee_recipient_acceptances", "fee_recipient_proposals",
	"future_defaults_configurations", "future_treasury_configurations", "graduations",
	"holder_balances", "indexed_blocks", "indexer_engine_deployments", "indexer_heartbeats", "indexer_reorgs", "launch_fee_claims", "launch_pause_events",
	"leaderboard_window_ranks", "leaderboard_window_refreshes", "lp_fee_claims", "lp_fee_collections",
	"pool_burns", "pool_mints", "pool_swaps", "pool_syncs", "position_dirty", "protocol_daily",
	"protocol_daily_revenue", "protocol_fee_claims", "protocol_stats", "refund_claims",
	"refund_credits", "snipe_tax_claims", "sync_state", "token_launches", "token_reserves",
	"token_stats", "tokens", "trades", "trading_pause_events", "transfers", "wallet_positions",
	"wallet_realized_legs", "wallet_totals", "v4_liquidity_changes", "v4_pool_initializations",
	"v4_pool_managers", "v4_pools", "v4_swaps",
}

// apiColumnWrites are the only columns the API may insert or update on tables whose moderation
// columns belong to the moderate role (docs/specs/2026-11-20-narrow-role-grants-preflight.md).
var apiColumnWrites = map[string]struct{ insert, update []string }{
	"social_users": {
		insert: []string{"chain_id", "deployment_id", "privy_did"},
		update: []string{"username", "username_changed_at", "updated_at"},
	},
	"token_comments": {
		insert: []string{"chain_id", "deployment_id", "token_address", "privy_did", "wallet", "author_role", "body"},
	},
	"comment_reports": {
		insert: []string{"comment_id", "chain_id", "deployment_id", "reporter_did", "reason"},
	},
}

// privateReaders names the only roles that may read a table; every other table is readable by
// every role.
var privateReaders = map[string][]string{
	"telegram_link_codes": {APIRole, TelegramRole},
	"telegram_links":      {APIRole, TelegramRole},
	"social_users":        {APIRole, ModerateRole},
	"comment_reports":     {APIRole, ModerateRole},
	"moderation_actions":  {APIRole, ModerateRole},
}

// apiLockFunctions take row locks for the API, which holds no UPDATE privilege on the locked
// tables; only the API may execute them.
var apiLockFunctions = []string{
	"lock_token_launch(bigint, bytea)",
	"lock_reportable_comment(bigint, bigint, text)",
}

var serviceRoles = []string{APIRole, IndexerRole, TelegramRole, FundingRole, ModerateRole}

func TestServiceRoleGrants(t *testing.T) {
	database := NewMigrated(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	rows, err := database.DB.QueryContext(ctx, `SELECT tablename FROM pg_tables WHERE schemaname = 'public' AND tablename <> 'goose_db_version' ORDER BY tablename`)
	if err != nil {
		t.Fatal(err)
	}
	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		tables = append(tables, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	for _, table := range tables {
		allowed, classified := writers[table]
		if slices.Contains(chainDerived, table) {
			if classified {
				t.Fatalf("%s is both chain-derived and user content", table)
			}
			allowed, classified = []string{IndexerRole}, true
		}
		if !classified {
			t.Errorf("table %s has no grant decision: add it to writers or chainDerived and grant it in its migration", table)
			continue
		}
		for _, role := range serviceRoles {
			var read, insert, update, remove bool
			if err := database.DB.QueryRowContext(ctx, `SELECT has_table_privilege($1, $2, 'SELECT'), has_table_privilege($1, $2, 'INSERT'), has_table_privilege($1, $2, 'UPDATE'), has_table_privilege($1, $2, 'DELETE')`, role, "public."+table).Scan(&read, &insert, &update, &remove); err != nil {
				t.Fatal(err)
			}
			want := slices.Contains(allowed, role)
			wantRead := true
			if readers, private := privateReaders[table]; private {
				wantRead = slices.Contains(readers, role)
			}
			if read != wantRead || insert != want || update != want || remove != want {
				t.Errorf("%s on %s: select=%t insert=%t update=%t delete=%t, want select=%t write=%t", role, table, read, insert, update, remove, wantRead, want)
			}
		}
	}
	for table := range privateReaders {
		if !slices.Contains(tables, table) {
			t.Errorf("private table %s does not exist; remove it from privateReaders", table)
		}
	}
	for table, grant := range apiColumnWrites {
		assertAPIColumnWrites(ctx, t, database.DB, table, grant.insert, grant.update)
	}
	for _, function := range apiLockFunctions {
		for _, role := range serviceRoles {
			var execute bool
			if err := database.DB.QueryRowContext(ctx, `SELECT has_function_privilege($1, $2, 'EXECUTE')`, role, "public."+function).Scan(&execute); err != nil {
				t.Fatalf("%s on %s: %v", role, function, err)
			}
			if want := role == APIRole; execute != want {
				t.Errorf("%s execute %s = %t, want %t", role, function, execute, want)
			}
		}
	}
	for _, table := range append(slices.Clone(chainDerived), userContent...) {
		if !slices.Contains(tables, table) {
			t.Errorf("classified table %s does not exist; remove it from the matrix", table)
		}
	}
	for _, role := range serviceRoles {
		var superuser, login bool
		if err := database.DB.QueryRowContext(ctx, `SELECT rolsuper, rolcanlogin FROM pg_roles WHERE rolname = $1`, role).Scan(&superuser, &login); err != nil {
			t.Fatalf("group role %s: %v", role, err)
		}
		if superuser || login {
			t.Errorf("group role %s: superuser=%t login=%t, want neither", role, superuser, login)
		}
	}
}

// assertAPIColumnWrites checks that the API may insert and update exactly the named columns of
// table and no other column.
func assertAPIColumnWrites(ctx context.Context, t *testing.T, db *sql.DB, table string, insert, update []string) {
	t.Helper()
	rows, err := db.QueryContext(ctx, `SELECT attname, has_column_privilege($1, $2, attname, 'INSERT'), has_column_privilege($1, $2, attname, 'UPDATE')
FROM pg_attribute WHERE attrelid = $2::regclass AND attnum > 0 AND NOT attisdropped ORDER BY attnum`, APIRole, "public."+table)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var columns []string
	for rows.Next() {
		var column string
		var canInsert, canUpdate bool
		if err := rows.Scan(&column, &canInsert, &canUpdate); err != nil {
			t.Fatal(err)
		}
		columns = append(columns, column)
		if want := slices.Contains(insert, column); canInsert != want {
			t.Errorf("%s insert %s.%s = %t, want %t", APIRole, table, column, canInsert, want)
		}
		if want := slices.Contains(update, column); canUpdate != want {
			t.Errorf("%s update %s.%s = %t, want %t", APIRole, table, column, canUpdate, want)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	for _, column := range append(slices.Clone(insert), update...) {
		if !slices.Contains(columns, column) {
			t.Errorf("granted column %s.%s does not exist", table, column)
		}
	}
}
