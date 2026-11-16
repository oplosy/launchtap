//go:build integration

package postgrestest

import (
	"context"
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

// writers names, for every table that is not chain-derived, the service roles allowed to write it.
// A chain-derived table is written by the indexer alone. Adding a table means classifying it here
// and granting it in its migration (docs/specs/2026-11-16-postgres-service-roles-preflight.md).
var writers = map[string][]string{
	"social_users":             {APIRole, ModerateRole},
	"token_comments":           {APIRole, ModerateRole},
	"comment_votes":            {APIRole},
	"comment_reports":          {APIRole, ModerateRole},
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
	"holder_balances", "indexed_blocks", "indexer_reorgs", "launch_fee_claims", "launch_pause_events",
	"leaderboard_window_ranks", "leaderboard_window_refreshes", "lp_fee_claims", "lp_fee_collections",
	"pool_burns", "pool_mints", "pool_swaps", "pool_syncs", "position_dirty", "protocol_daily",
	"protocol_daily_revenue", "protocol_fee_claims", "protocol_stats", "refund_claims",
	"refund_credits", "snipe_tax_claims", "sync_state", "token_launches", "token_reserves",
	"token_stats", "tokens", "trades", "trading_pause_events", "transfers", "wallet_positions",
	"wallet_realized_legs", "wallet_totals", "v4_liquidity_changes", "v4_pool_initializations",
	"v4_pool_managers", "v4_pools", "v4_swaps",
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
			if !read || insert != want || update != want || remove != want {
				t.Errorf("%s on %s: select=%t insert=%t update=%t delete=%t, want select and write=%t", role, table, read, insert, update, remove, want)
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
