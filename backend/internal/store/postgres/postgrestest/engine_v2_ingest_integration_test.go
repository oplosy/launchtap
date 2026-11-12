//go:build integration

package postgrestest

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"math/big"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Contictus/launchtap/backend/internal/chain"
	"github.com/Contictus/launchtap/backend/internal/indexer"
	"github.com/Contictus/launchtap/backend/internal/ledger"
	storepostgres "github.com/Contictus/launchtap/backend/internal/store/postgres"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

// decodeV2Fixture decodes the frozen engine-2 fixture as one block, one transaction per log,
// and attaches swapper as every V4 swap's transaction sender.
func decodeV2Fixture(t *testing.T, block ledger.IndexedBlock, swapper common.Address) []chain.DecodedLog {
	t.Helper()
	body, err := os.ReadFile("../../../chain/testdata/v2/event-logs-v2.json")
	if err != nil {
		t.Fatalf("read v2 fixture: %v", err)
	}
	var artifact struct {
		Logs []struct {
			EmitterKind string   `json:"emitterKind"`
			Address     string   `json:"address"`
			Topics      []string `json:"topics"`
			Data        string   `json:"data"`
		} `json:"logs"`
	}
	if err := json.Unmarshal(body, &artifact); err != nil {
		t.Fatalf("decode v2 fixture: %v", err)
	}
	emitters := chain.Emitters{Curves: chain.NewAddressSet(), Tokens: chain.NewAddressSet(), Pairs: chain.NewAddressSet(), Pools: make(map[common.Hash]common.Address)}
	logs := make([]types.Log, 0, len(artifact.Logs))
	for index, fixture := range artifact.Logs {
		address := common.HexToAddress(fixture.Address)
		topics := make([]common.Hash, len(fixture.Topics))
		for topicIndex, topic := range fixture.Topics {
			topics[topicIndex] = common.HexToHash(topic)
		}
		switch fixture.EmitterKind {
		case "factory":
			emitters.Factory = address
		case "curve":
			emitters.Curves[address] = struct{}{}
		case "token":
			emitters.Tokens[address] = struct{}{}
		case "locker":
			emitters.Locker = address
		case "poolManager":
			emitters.PoolManager = address
			if len(topics) == 4 {
				emitters.Pools[topics[1]] = common.BytesToAddress(topics[3][12:])
			}
		}
		data, err := hex.DecodeString(strings.TrimPrefix(fixture.Data, "0x"))
		if err != nil {
			t.Fatalf("decode fixture data: %v", err)
		}
		logs = append(logs, types.Log{Address: address, Topics: topics, Data: data, BlockNumber: uint64(block.BlockNumber), BlockHash: block.BlockHash, TxHash: common.BigToHash(big.NewInt(int64(index + 1))), TxIndex: uint(index), Index: uint(index)})
	}
	decoder, err := chain.NewDecoder(2)
	if err != nil {
		t.Fatal(err)
	}
	decoded := make([]chain.DecodedLog, 0, len(logs))
	for _, log := range logs {
		event, err := decoder.Decode(log, emitters)
		if err != nil {
			t.Fatalf("decode fixture log %d: %v", log.Index, err)
		}
		if swap, ok := event.Value.(chain.V4Swap); ok {
			sender := swapper
			swap.TxFrom = &sender
			event.Value = swap
		}
		decoded = append(decoded, event)
	}
	return decoded
}

func TestEngineV2FixtureIngestsThroughRouter(t *testing.T) {
	database := NewMigrated(t)
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	pool := openPool(t, ctx, database.URL)

	const chainID int64 = 49020
	block := ledger.IndexedBlock{ChainID: chainID, BlockNumber: 7, BlockHash: common.Hash{0xb7}, ParentHash: common.Hash{0xb6}, BlockTime: time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC), FinalityStatus: "observed"}
	swapper := common.HexToAddress("0x5a5a")
	decoded := decodeV2Fixture(t, block, swapper)
	router := indexer.LedgerRouter{ChainID: chainID}
	blocks := map[int64]ledger.IndexedBlock{block.BlockNumber: block}

	if err := storepostgres.WithinTx(ctx, pool, func(ctx context.Context, adapter *storepostgres.Adapter) error {
		if _, err := adapter.UpsertIndexedBlock(ctx, block); err != nil {
			return err
		}
		return router.Apply(ctx, adapter, decoded, blocks, nil)
	}); err != nil {
		t.Fatalf("ingest v2 fixture: %v", err)
	}

	for table, want := range map[string]int{
		"token_launches": 1, "tokens": 1, "trades": 4, "graduations": 1, "creator_fee_claims": 2,
		"protocol_fee_claims": 1, "snipe_tax_claims": 1, "fee_recipient_proposals": 1,
		"fee_recipient_acceptances": 1, "lp_fee_collections": 1, "lp_fee_claims": 2,
		"v4_pool_initializations": 1, "v4_liquidity_changes": 4, "v4_swaps": 2,
	} {
		assertRowCount(t, ctx, database.DB, table, chainID, want)
	}
	var phase string
	var engineVersion int
	var hookSet bool
	if err := database.DB.QueryRowContext(ctx, "SELECT phase, engine_version, hook IS NOT NULL AND locker IS NOT NULL AND lp_creator_share_bps IS NOT NULL FROM tokens WHERE chain_id=$1", chainID).Scan(&phase, &engineVersion, &hookSet); err != nil {
		t.Fatalf("read v2 token: %v", err)
	}
	if phase != "graduated" || engineVersion != 2 || !hookSet {
		t.Fatalf("token = phase %q engine %d v2 fields %t, want a graduated engine-2 token with its v2 fields", phase, engineVersion, hookSet)
	}
	var swapsWithSender int
	if err := database.DB.QueryRowContext(ctx, "SELECT count(*) FROM v4_swaps WHERE chain_id=$1 AND tx_from=$2", chainID, swapper.Bytes()).Scan(&swapsWithSender); err != nil {
		t.Fatalf("read v4 swap senders: %v", err)
	}
	if swapsWithSender != 2 {
		t.Fatalf("v4 swaps with tx sender = %d, want 2", swapsWithSender)
	}

	var identities []indexer.TokenIdentity
	var pools map[common.Hash]common.Address
	if err := storepostgres.WithinTx(ctx, pool, func(ctx context.Context, adapter *storepostgres.Adapter) error {
		var err error
		if identities, err = adapter.TokenIdentities(ctx, chainID); err != nil {
			return err
		}
		pools, err = adapter.GraduatedV4Pools(ctx, chainID)
		return err
	}); err != nil {
		t.Fatalf("read identities: %v", err)
	}
	if len(identities) != 1 || identities[0].EngineVersion != 2 || identities[0].Pair != (common.Address{}) {
		t.Fatalf("identities = %+v, want one engine-2 token without a pair", identities)
	}
	if len(pools) != 1 || pools[chain.V4PoolID(identities[0].Token, firstLaunchHook(t, decoded))] != identities[0].Token {
		t.Fatalf("graduated pools = %v, want the token's V4 pool", pools)
	}

	// A replayed chunk inserts nothing and changes nothing.
	if err := storepostgres.WithinTx(ctx, pool, func(ctx context.Context, adapter *storepostgres.Adapter) error {
		return router.Apply(ctx, adapter, decoded, blocks, nil)
	}); err != nil {
		t.Fatalf("replay v2 fixture: %v", err)
	}
	assertRowCount(t, ctx, database.DB, "v4_swaps", chainID, 2)
	assertRowCount(t, ctx, database.DB, "trades", chainID, 4)
}

func firstLaunchHook(t *testing.T, decoded []chain.DecodedLog) common.Address {
	t.Helper()
	for _, log := range decoded {
		if launch, ok := log.Value.(chain.TokenLaunchedV2); ok {
			return launch.Hook
		}
	}
	t.Fatal("fixture has no v2 launch")
	return common.Address{}
}
