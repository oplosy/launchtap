//go:build integration

package indexer_test

import (
	"context"
	"math/big"
	"math/rand/v2"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Contictus/launchtap/backend/internal/chain"
	"github.com/Contictus/launchtap/backend/internal/indexer"
	storepostgres "github.com/Contictus/launchtap/backend/internal/store/postgres"
	"github.com/Contictus/launchtap/backend/internal/store/postgres/postgrestest"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	gethrpc "github.com/ethereum/go-ethereum/rpc"
	"github.com/jackc/pgx/v5/pgxpool"
)

const soakTradeABI = `[
{"type":"function","name":"sell","stateMutability":"nonpayable","inputs":[{"name":"tokensIn","type":"uint256"},{"name":"ethRecipient","type":"address"},{"name":"minEthOut","type":"uint256"},{"name":"deadline","type":"uint256"}],"outputs":[{"name":"ethOut","type":"uint256"}]},
{"type":"function","name":"transfer","stateMutability":"nonpayable","inputs":[{"name":"to","type":"address"},{"name":"amount","type":"uint256"}],"outputs":[{"name":"","type":"bool"}]}
]`

// soakDepths are the abandoned-branch lengths, deepest last and under the default search depth.
var soakDepths = []int{1, 2, 8, 16, 32, 64, 100}

const defaultSoakSeed = 20261114

// TestAnvilV2DeepReorgSoak reorgs an engine-2 token's trading history at increasing depths and,
// after every round, compares the recovered database with the chain and with a fresh index of
// the same chain (docs/specs/2026-11-14-deep-reorg-soak-preflight.md).
func TestAnvilV2DeepReorgSoak(t *testing.T) {
	if os.Getenv("ANVIL_V2_RPC_URL") == "" {
		t.Skip("run through task anvil-v2-indexer-e2e")
	}
	rpcURL := requireAnvilEnvironment(t, "ANVIL_V2_RPC_URL")
	factory := common.HexToAddress(requireAnvilEnvironment(t, "ANVIL_V2_FACTORY"))
	poolManager := common.HexToAddress(requireAnvilEnvironment(t, "ANVIL_V2_POOL_MANAGER"))
	locker := common.HexToAddress(requireAnvilEnvironment(t, "ANVIL_V2_LOCKER"))
	hook := common.HexToAddress(requireAnvilEnvironment(t, "ANVIL_V2_HOOK"))
	start, err := strconv.ParseInt(requireAnvilEnvironment(t, "ANVIL_V2_START_BLOCK"), 10, 64)
	if err != nil || start < 0 {
		t.Fatalf("ANVIL_V2_START_BLOCK is invalid: %v", err)
	}
	seed := uint64(defaultSoakSeed)
	if text := os.Getenv("ANVIL_REORG_SOAK_SEED"); text != "" {
		if seed, err = strconv.ParseUint(text, 10, 64); err != nil {
			t.Fatalf("ANVIL_REORG_SOAK_SEED is invalid: %v", err)
		}
	}
	t.Logf("reorg soak seed %d (set ANVIL_REORG_SOAK_SEED to replay)", seed)
	// Anvil dev accounts 7-9; the lifecycle test and the deployment use accounts 0-6.
	traders := []common.Address{
		common.HexToAddress("0x14dC79964da2C08b23698B3D3cc7Ca32193d9955"),
		common.HexToAddress("0x23618e81E3f5cdF7f54C3d65f7FBc0aBf5B21E8f"),
		common.HexToAddress("0xa0Ee7A142d267C1f36714E4a8F75612F20a79720"),
	}
	const chainID int64 = 31_337
	const deploymentID = "anvil-v2-reorg-soak"

	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Minute)
	defer cancel()
	source, err := chain.Dial(ctx, rpcURL, chain.RPCConfig{Timeout: 2 * time.Second, RetryBackoff: 10 * time.Millisecond})
	if err != nil {
		t.Fatalf("dial Anvil: %v", err)
	}
	defer source.Close()
	decoder, err := chain.NewDecoder(2)
	if err != nil {
		t.Fatal(err)
	}
	discovery, err := chain.NewDiscovererV2(source, decoder, factory, 64, chain.V4Config{PoolManager: poolManager, Locker: locker, Hook: hook})
	if err != nil {
		t.Fatal(err)
	}
	// newIndexer wires an indexer to a fresh migrated database the way cmd/indexer does for
	// engine 2 and returns the database, its pool and a function that indexes up to the head.
	newIndexer := func(label string) (*postgrestest.Database, *pgxpool.Pool, func(string)) {
		t.Helper()
		database := postgrestest.NewMigrated(t)
		pool, err := storepostgres.OpenPool(ctx, database.URL, storepostgres.PoolOptions{})
		if err != nil {
			t.Fatalf("open %s pool: %v", label, err)
		}
		t.Cleanup(pool.Close)
		if err := storepostgres.NewAdapter(pool).RegisterV4PoolManager(ctx, chainID, poolManager); err != nil {
			t.Fatalf("register pool manager: %v", err)
		}
		engine, err := indexer.New(indexer.Settings{
			ChainID: chainID, DeploymentID: deploymentID, Factory: factory,
			StartBlock: start, ChunkSize: 128, PollInterval: time.Millisecond, Senders: source,
		}, storepostgres.IndexerStore{Pool: pool, ChainID: chainID, DeploymentID: deploymentID, Factory: factory},
			laggedFinalitySource{client: source, stable: uint64(start)}, discovery, decoder, indexer.LedgerRouter{ChainID: chainID})
		if err != nil {
			t.Fatal(err)
		}
		return database, pool, func(step string) {
			t.Helper()
			// Steps until the indexer has nothing left: a reorg recovery is one step and the
			// replacement blocks are the next ones.
			for range 64 {
				advanced, err := engine.Step(ctx)
				if err != nil {
					t.Fatalf("%s: %v", step, err)
				}
				if !advanced {
					return
				}
			}
			t.Fatalf("%s: indexer still advancing after 64 steps", step)
		}
	}
	database, pool, indexAll := newIndexer("soak")

	rpcClient, err := gethrpc.DialContext(ctx, rpcURL)
	if err != nil {
		t.Fatal(err)
	}
	defer rpcClient.Close()
	fixture, err := abi.JSON(strings.NewReader(v2FixtureABI))
	if err != nil {
		t.Fatal(err)
	}
	trading, err := abi.JSON(strings.NewReader(soakTradeABI))
	if err != nil {
		t.Fatal(err)
	}
	c := v2Chain{t: t, ctx: ctx, rpc: rpcClient, fixture: fixture}
	pack := func(method string, args ...any) []byte {
		t.Helper()
		data, err := trading.Pack(method, args...)
		if err != nil {
			t.Fatalf("pack %s: %v", method, err)
		}
		return data
	}
	milliEther := big.NewInt(1_000_000_000_000_000)
	deadline := big.NewInt(time.Now().Add(24 * time.Hour).Unix())

	// Launch, then let every trader buy and approve the curve before the first snapshot, so
	// sells and transfers have balances on every branch.
	creator := traders[0]
	developerBuy := new(big.Int).Mul(big.NewInt(5), milliEther)
	c.send(creator, factory, new(big.Int).Add(c.uint(factory, "launchFee"), developerBuy), c.pack("launch", struct {
		Name                  string
		Symbol                string
		EngineVersion         uint16
		DeveloperBuyGross     *big.Int
		MinDeveloperTokensOut *big.Int
		Deadline              *big.Int
	}{"Reorg Soak", "SOAK", 2, developerBuy, big.NewInt(0), deadline}))
	indexAll("index launch")
	var tokenBytes, curveBytes []byte
	if err := database.DB.QueryRowContext(ctx, `SELECT token_address, curve_address FROM tokens WHERE chain_id=$1 AND symbol='SOAK'`, chainID).Scan(&tokenBytes, &curveBytes); err != nil {
		t.Fatalf("read soak token: %v", err)
	}
	token, curve := common.BytesToAddress(tokenBytes), common.BytesToAddress(curveBytes)
	c.at(c.uint(curve, "launchTime").Uint64() + 61)
	maxApproval := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
	for _, trader := range traders {
		c.send(trader, curve, new(big.Int).Mul(big.NewInt(5), milliEther), packBuy(t, trader))
		c.send(trader, token, big.NewInt(0), c.pack("approve", curve, maxApproval))
	}
	indexAll("index setup trades")

	random := rand.New(rand.NewPCG(seed, seed^0x5eed))
	// mine sends one random transaction per block: a buy of 1-5 milli-ether, a sell of a tenth
	// of the sender's balance, or a transfer of a twentieth of it to another trader.
	mine := func(blocks int) {
		t.Helper()
		for range blocks {
			trader := traders[random.IntN(len(traders))]
			balance := c.uint(token, "balanceOf", trader)
			action := random.IntN(4)
			switch {
			case action >= 2 || balance.Sign() == 0:
				amount := new(big.Int).Mul(big.NewInt(int64(1+random.IntN(5))), milliEther)
				c.send(trader, curve, amount, packBuy(t, trader))
			case action == 1:
				c.send(trader, curve, big.NewInt(0), pack("sell", new(big.Int).Div(balance, big.NewInt(10)), trader, big.NewInt(0), deadline))
			default:
				to := traders[(random.IntN(len(traders)-1)+1+indexOf(traders, trader))%len(traders)]
				c.send(trader, token, big.NewInt(0), pack("transfer", to, new(big.Int).Div(balance, big.NewInt(20))))
			}
		}
	}

	for round, depth := range soakDepths {
		var snapshotID string
		if err := rpcClient.CallContext(ctx, &snapshotID, "evm_snapshot"); err != nil {
			t.Fatal(err)
		}
		ancestor := headNumber(t, ctx, rpcClient)
		mine(depth)
		indexAll("index abandoned branch")
		var reverted bool
		if err := rpcClient.CallContext(ctx, &reverted, "evm_revert", snapshotID); err != nil || !reverted {
			t.Fatalf("round %d: revert Anvil: reverted=%t err=%v", round, reverted, err)
		}
		mine(depth + 1 + random.IntN(4))
		indexAll("recover and index replacement branch")

		reorgs := queryStrings(t, ctx, database.DB, `SELECT depth::text || ',' || common_ancestor_number::text || ',' || outcome FROM indexer_reorgs WHERE chain_id=$1 AND deployment_id=$2 ORDER BY reorg_id`, chainID, deploymentID)
		want := strconv.Itoa(depth) + "," + strconv.FormatUint(ancestor, 10) + ",recovered"
		if len(reorgs) != round+1 || reorgs[round] != want {
			t.Fatalf("round %d (depth %d): indexer_reorgs = %v, want %d rows ending in %s", round, depth, reorgs, round+1, want)
		}
		for _, holder := range queryStrings(t, ctx, database.DB, `SELECT '0x' || encode(holder_address, 'hex') || ',' || balance::text FROM holder_balances WHERE chain_id=$1 AND token_address=$2`, chainID, token.Bytes()) {
			address, balance, _ := strings.Cut(holder, ",")
			if onchain := c.uint(token, "balanceOf", common.HexToAddress(address)).String(); balance != onchain {
				t.Fatalf("round %d (depth %d): holder_balances[%s] = %s, balanceOf = %s", round, depth, address, balance, onchain)
			}
		}
		recovered := soakRows(t, ctx, database, pool, chainID, token)
		freshDatabase, freshPool, indexFresh := newIndexer("fresh")
		indexFresh("index the chain from scratch")
		if fresh := soakRows(t, ctx, freshDatabase, freshPool, chainID, token); !reflect.DeepEqual(recovered, fresh) {
			t.Fatalf("round %d (depth %d): recovered index differs from a fresh index\nrecovered=%v\nfresh=%v", round, depth, recovered, fresh)
		}
		t.Logf("round %d: depth %d reorg recovered; %d ledger and projection rows match a fresh index", round, depth, len(recovered))
	}
}

// soakRows renders the deployment's canonical ledger and the token's projections as text.
func soakRows(t *testing.T, ctx context.Context, database *postgrestest.Database, pool *pgxpool.Pool, chainID int64, token common.Address) []string {
	t.Helper()
	rows := queryStrings(t, ctx, database.DB, `SELECT row(tr.*)::text FROM trades tr WHERE chain_id=$1 ORDER BY block_number, log_index`, chainID)
	rows = append(rows, queryStrings(t, ctx, database.DB, `SELECT row(tf.*)::text FROM transfers tf WHERE chain_id=$1 ORDER BY block_number, log_index`, chainID)...)
	return append(rows, v2ProjectionRows(t, ctx, database.DB, pool, chainID, token)...)
}

func headNumber(t *testing.T, ctx context.Context, client *gethrpc.Client) uint64 {
	t.Helper()
	var head hexutil.Uint64
	if err := client.CallContext(ctx, &head, "eth_blockNumber"); err != nil {
		t.Fatalf("read head: %v", err)
	}
	return uint64(head)
}

func indexOf(addresses []common.Address, address common.Address) int {
	for i, candidate := range addresses {
		if candidate == address {
			return i
		}
	}
	return -1
}
