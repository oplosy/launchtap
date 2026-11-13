package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Contictus/launchtap/backend/internal/stats"
	"github.com/Contictus/launchtap/backend/internal/store/postgres/sqlc"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestAggregationSourceComputeBatchCoalescesByChainAndKeepsTokenFailures(t *testing.T) {
	tokenFailure := errors.New("token stats failure")
	db := &aggregationSourceRecorder{failToken: [20]byte{3}, tokenFailure: tokenFailure}
	source := AggregationSource{Adapter: NewAdapter(db)}
	claims := []stats.Claim{
		{ChainID: 4663, Token: [20]byte{1}},
		{ChainID: 4663, Token: [20]byte{2}},
		{ChainID: 4663, Token: [20]byte{3}},
		{ChainID: 46630, Token: [20]byte{4}},
		{ChainID: 46630, Token: [20]byte{5}},
	}

	results := source.ComputeBatch(context.Background(), claims)
	if len(results) != len(claims) {
		t.Fatalf("got %d results for %d claims", len(results), len(claims))
	}
	for i, err := range results {
		if i == 2 {
			if !errors.Is(err, tokenFailure) {
				t.Fatalf("claim %d error = %v, want token stats failure", i, err)
			}
			continue
		}
		if err != nil {
			t.Fatalf("claim %d error = %v, want nil", i, err)
		}
	}
	if db.tokenStatsCalls != len(claims) {
		t.Fatalf("token stats calls = %d, want %d", db.tokenStatsCalls, len(claims))
	}
	for _, chainID := range []int64{4663, 46630} {
		if got := db.clearDailyCalls[chainID]; got != 1 {
			t.Errorf("chain %d clear calls = %d, want 1", chainID, got)
		}
		if got := db.dailyCalls[chainID]; got != 1 {
			t.Errorf("chain %d daily rebuild calls = %d, want 1", chainID, got)
		}
		if got := db.protocolStatsCalls[chainID]; got != 1 {
			t.Errorf("chain %d stats rebuild calls = %d, want 1", chainID, got)
		}
	}
}

func TestAggregationSourceComputeBatchFailsAllComputedClaimsOnProtocolError(t *testing.T) {
	protocolFailure := errors.New("protocol stats failure")
	db := &aggregationSourceRecorder{failProtocolChain: 4663, protocolFailure: protocolFailure}
	source := AggregationSource{Adapter: NewAdapter(db)}
	claims := []stats.Claim{
		{ChainID: 4663, Token: [20]byte{1}},
		{ChainID: 4663, Token: [20]byte{2}},
		{ChainID: 46630, Token: [20]byte{3}},
	}

	results := source.ComputeBatch(context.Background(), claims)
	for i, err := range results {
		if i < 2 {
			if !errors.Is(err, protocolFailure) {
				t.Fatalf("claim %d error = %v, want protocol refresh failure", i, err)
			}
			continue
		}
		if err != nil {
			t.Fatalf("other chain claim error = %v, want nil", err)
		}
	}
	if got := db.clearDailyCalls[4663]; got != 1 {
		t.Errorf("failed chain clear calls = %d, want 1", got)
	}
	if got := db.clearDailyCalls[46630]; got != 1 {
		t.Errorf("other chain clear calls = %d, want 1", got)
	}
}

type aggregationSourceRecorder struct {
	tokenStatsCalls    int
	clearDailyCalls    map[int64]int
	dailyCalls         map[int64]int
	protocolStatsCalls map[int64]int
	failToken          [20]byte
	tokenFailure       error
	failProtocolChain  int64
	protocolFailure    error
}

func (recorder *aggregationSourceRecorder) Exec(_ context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	if recorder.clearDailyCalls == nil {
		recorder.clearDailyCalls = make(map[int64]int)
		recorder.dailyCalls = make(map[int64]int)
		recorder.protocolStatsCalls = make(map[int64]int)
	}
	if strings.Contains(query, "INSERT INTO token_stats") {
		recorder.tokenStatsCalls++
		if len(args) > 1 {
			if address, ok := args[1].(sqlc.Address); ok && [20]byte(address) == recorder.failToken && recorder.tokenFailure != nil {
				return pgconn.CommandTag{}, recorder.tokenFailure
			}
		}
	}
	chainID, _ := args[0].(int64)
	switch {
	case strings.Contains(query, "DELETE FROM protocol_daily"):
		recorder.clearDailyCalls[chainID]++
	case strings.Contains(query, "INSERT INTO protocol_daily"):
		recorder.dailyCalls[chainID]++
	case strings.Contains(query, "INSERT INTO protocol_stats"):
		recorder.protocolStatsCalls[chainID]++
		if chainID == recorder.failProtocolChain && recorder.protocolFailure != nil {
			return pgconn.CommandTag{}, recorder.protocolFailure
		}
	}
	return pgconn.NewCommandTag("EXEC 0"), nil
}

func (*aggregationSourceRecorder) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, errors.New("unexpected query")
}

// QueryRow answers the protocol aggregate bounds read with "no watermark, no indexed block",
// which selects the full rebuild path.
func (*aggregationSourceRecorder) QueryRow(_ context.Context, query string, _ ...any) pgx.Row {
	if strings.Contains(query, "GetProtocolAggregateBounds") {
		return aggregationSourceBoundsRow{}
	}
	return aggregationSourceBoundsRow{err: errors.New("unexpected query row")}
}

type aggregationSourceBoundsRow struct{ err error }

func (row aggregationSourceBoundsRow) Scan(dest ...any) error {
	if row.err != nil {
		return row.err
	}
	for _, target := range dest {
		*(target.(*int64)) = -1
	}
	return nil
}
