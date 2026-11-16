package postgres

import (
	"errors"
	"fmt"
	"testing"

	"github.com/Contictus/launchtap/backend/internal/indexer"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestMarkStoreConflictTagsOnlyRolledBackConflicts(t *testing.T) {
	for _, code := range []string{"40P01", "40001"} {
		original := fmt.Errorf("recompute token stats: %w", &pgconn.PgError{Code: code})
		err := markStoreConflict(original)
		var pgError *pgconn.PgError
		if !errors.Is(err, indexer.ErrStoreConflict) || !errors.As(err, &pgError) || pgError.Code != code {
			t.Fatalf("markStoreConflict(%s) = %v, want a store conflict that keeps the Postgres error", code, err)
		}
	}
	for _, err := range []error{
		&pgconn.PgError{Code: "23514"},
		errors.New("connection reset"),
	} {
		if got := markStoreConflict(err); errors.Is(got, indexer.ErrStoreConflict) || got != err {
			t.Fatalf("markStoreConflict(%v) = %v, want the error unchanged", err, got)
		}
	}
	if markStoreConflict(nil) != nil {
		t.Fatal("markStoreConflict(nil) is not nil")
	}
}
