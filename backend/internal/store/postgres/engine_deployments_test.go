package postgres

import (
	"errors"
	"strings"
	"testing"

	"github.com/Contictus/launchtap/backend/internal/store/postgres/sqlc"
	"github.com/ethereum/go-ethereum/common"
)

func TestNewEngineDeployments(t *testing.T) {
	v1 := EngineDeployment{DeploymentID: "chain-v1", EngineVersion: 1, Factory: common.HexToAddress("0xf1"), StartBlock: 100}
	v2 := EngineDeployment{DeploymentID: "chain-v2", EngineVersion: 2, Factory: common.HexToAddress("0xf2"), StartBlock: 500}
	row := func(engine EngineDeployment) sqlc.ListIndexerEngineDeploymentsRow {
		return sqlc.ListIndexerEngineDeploymentsRow{EngineDeploymentID: engine.DeploymentID, EngineVersion: int32(engine.EngineVersion), Factory: sqlc.Address(engine.Factory), StartBlock: engine.StartBlock}
	}
	at := func(block int64) *int64 { return &block }

	t.Run("first start records every engine", func(t *testing.T) {
		fresh, err := newEngineDeployments(nil, "chain-v1", []EngineDeployment{v1, v2}, nil)
		if err != nil || len(fresh) != 2 {
			t.Fatalf("newEngineDeployments() = %v, %v", fresh, err)
		}
	})
	t.Run("recorded engines are not recorded again", func(t *testing.T) {
		fresh, err := newEngineDeployments([]sqlc.ListIndexerEngineDeploymentsRow{row(v1), row(v2)}, "chain-v1", []EngineDeployment{v1, v2}, at(900))
		if err != nil || len(fresh) != 0 {
			t.Fatalf("newEngineDeployments() = %v, %v", fresh, err)
		}
	})
	t.Run("an engine added before the cursor reaches it is recorded", func(t *testing.T) {
		fresh, err := newEngineDeployments([]sqlc.ListIndexerEngineDeploymentsRow{row(v1)}, "chain-v1", []EngineDeployment{v1, v2}, at(499))
		if err != nil || len(fresh) != 1 || fresh[0].DeploymentID != "chain-v2" {
			t.Fatalf("newEngineDeployments() = %v, %v", fresh, err)
		}
	})
	t.Run("an engine added after the cursor passed its start is refused", func(t *testing.T) {
		_, err := newEngineDeployments([]sqlc.ListIndexerEngineDeploymentsRow{row(v1)}, "chain-v1", []EngineDeployment{v1, v2}, at(500))
		var late *EngineAddedLateError
		if !errors.As(err, &late) || late.EngineDeploymentID != "chain-v2" || late.Observed != 500 {
			t.Fatalf("newEngineDeployments() error = %v, want EngineAddedLateError", err)
		}
	})
	t.Run("an indexer that ran before engines were recorded records its site engine", func(t *testing.T) {
		fresh, err := newEngineDeployments(nil, "chain-v1", []EngineDeployment{v1}, at(900))
		if err != nil || len(fresh) != 1 || fresh[0].DeploymentID != "chain-v1" {
			t.Fatalf("newEngineDeployments() = %v, %v", fresh, err)
		}
	})
	t.Run("a second engine is still refused when added late to such an indexer", func(t *testing.T) {
		_, err := newEngineDeployments(nil, "chain-v1", []EngineDeployment{v1, v2}, at(900))
		var late *EngineAddedLateError
		if !errors.As(err, &late) || late.EngineDeploymentID != "chain-v2" {
			t.Fatalf("newEngineDeployments() error = %v, want EngineAddedLateError", err)
		}
	})
	t.Run("a recorded engine whose factory changed is refused", func(t *testing.T) {
		moved := v2
		moved.Factory = common.HexToAddress("0xf3")
		_, err := newEngineDeployments([]sqlc.ListIndexerEngineDeploymentsRow{row(v1), row(v2)}, "chain-v1", []EngineDeployment{v1, moved}, at(900))
		if err == nil || !strings.Contains(err.Error(), "changed since it was first ingested") {
			t.Fatalf("newEngineDeployments() error = %v", err)
		}
	})
}
