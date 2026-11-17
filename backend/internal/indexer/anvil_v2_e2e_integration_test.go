//go:build integration

package indexer_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Contictus/launchtap/backend/internal/apiserver"
	"github.com/Contictus/launchtap/backend/internal/chain"
	"github.com/Contictus/launchtap/backend/internal/indexer"
	"github.com/Contictus/launchtap/backend/internal/quote"
	storepostgres "github.com/Contictus/launchtap/backend/internal/store/postgres"
	"github.com/Contictus/launchtap/backend/internal/store/postgres/postgrestest"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
	gethrpc "github.com/ethereum/go-ethereum/rpc"
	"github.com/jackc/pgx/v5/pgxpool"
)

const v2FixtureABI = `[
{"type":"function","name":"launch","stateMutability":"payable","inputs":[{"name":"request","type":"tuple","components":[{"name":"name","type":"string"},{"name":"symbol","type":"string"},{"name":"engineVersion","type":"uint16"},{"name":"developerBuyGross","type":"uint256"},{"name":"minDeveloperTokensOut","type":"uint256"},{"name":"deadline","type":"uint256"}]}],"outputs":[]},
{"type":"function","name":"launchFee","stateMutability":"view","inputs":[],"outputs":[{"name":"","type":"uint256"}]},
{"type":"function","name":"launchTime","stateMutability":"view","inputs":[],"outputs":[{"name":"","type":"uint64"}]},
{"type":"function","name":"proposeFeeRecipient","stateMutability":"nonpayable","inputs":[{"name":"next","type":"address"}],"outputs":[]},
{"type":"function","name":"acceptFeeRecipient","stateMutability":"nonpayable","inputs":[],"outputs":[]},
{"type":"function","name":"claimCreatorFees","stateMutability":"nonpayable","inputs":[],"outputs":[{"name":"amount","type":"uint256"}]},
{"type":"function","name":"creatorFeesOwed","stateMutability":"view","inputs":[{"name":"account","type":"address"}],"outputs":[{"name":"","type":"uint256"}]},
{"type":"function","name":"approve","stateMutability":"nonpayable","inputs":[{"name":"spender","type":"address"},{"name":"amount","type":"uint256"}],"outputs":[{"name":"","type":"bool"}]},
{"type":"function","name":"balanceOf","stateMutability":"view","inputs":[{"name":"account","type":"address"}],"outputs":[{"name":"","type":"uint256"}]},
{"type":"function","name":"collect","stateMutability":"nonpayable","inputs":[{"name":"token","type":"address"}],"outputs":[{"name":"ethAmount","type":"uint256"},{"name":"tokenAmount","type":"uint256"}]},
{"type":"function","name":"claim","stateMutability":"nonpayable","inputs":[{"name":"token","type":"address"}],"outputs":[{"name":"ethAmount","type":"uint256"},{"name":"tokenAmount","type":"uint256"}]},
{"type":"function","name":"owed","stateMutability":"view","inputs":[{"name":"token","type":"address"},{"name":"account","type":"address"}],"outputs":[{"name":"ethAmount","type":"uint256"},{"name":"tokenAmount","type":"uint256"}]},
{"type":"function","name":"extsload","stateMutability":"view","inputs":[{"name":"slot","type":"bytes32"}],"outputs":[{"name":"","type":"bytes32"}]},
{"type":"function","name":"swap","stateMutability":"payable","inputs":[
 {"name":"key","type":"tuple","components":[{"name":"currency0","type":"address"},{"name":"currency1","type":"address"},{"name":"fee","type":"uint24"},{"name":"tickSpacing","type":"int24"},{"name":"hooks","type":"address"}]},
 {"name":"params","type":"tuple","components":[{"name":"zeroForOne","type":"bool"},{"name":"amountSpecified","type":"int256"},{"name":"sqrtPriceLimitX96","type":"uint160"}]},
 {"name":"testSettings","type":"tuple","components":[{"name":"takeClaims","type":"bool"},{"name":"settleUsingBurn","type":"bool"}]},
 {"name":"hookData","type":"bytes"}],"outputs":[{"name":"delta","type":"int256"}]}
]`

type v4PoolKey struct {
	Currency0   common.Address
	Currency1   common.Address
	Fee         *big.Int
	TickSpacing *big.Int
	Hooks       common.Address
}

type v4SwapParams struct {
	ZeroForOne        bool
	AmountSpecified   *big.Int
	SqrtPriceLimitX96 *big.Int
}

type v4TestSettings struct {
	TakeClaims      bool
	SettleUsingBurn bool
}

// v2Chain drives the deployed engine-2 system from Anvil's unlocked dev accounts.
type v2Chain struct {
	t       *testing.T
	ctx     context.Context
	rpc     *gethrpc.Client
	fixture abi.ABI
}

func (c v2Chain) pack(method string, args ...any) []byte {
	c.t.Helper()
	data, err := c.fixture.Pack(method, args...)
	if err != nil {
		c.t.Fatalf("pack %s: %v", method, err)
	}
	return data
}

func (c v2Chain) send(from, to common.Address, value *big.Int, data []byte) {
	c.t.Helper()
	transaction := map[string]any{"from": from.Hex(), "to": to.Hex(), "value": "0x" + value.Text(16), "gas": "0x1c9c380", "data": "0x" + common.Bytes2Hex(data)}
	var hash common.Hash
	if err := c.rpc.CallContext(c.ctx, &hash, "eth_sendTransaction", transaction); err != nil {
		c.t.Fatalf("send transaction to %s: %v", to, err)
	}
	waitTransactionReceipt(c.t, c.ctx, c.rpc, hash)
}

func (c v2Chain) call(to common.Address, method string, args ...any) []any {
	c.t.Helper()
	var output hexutil.Bytes
	if err := c.rpc.CallContext(c.ctx, &output, "eth_call", map[string]any{"to": to.Hex(), "data": hexutil.Bytes(c.pack(method, args...))}, "latest"); err != nil {
		c.t.Fatalf("call %s on %s: %v", method, to, err)
	}
	values, err := c.fixture.Unpack(method, output)
	if err != nil {
		c.t.Fatalf("unpack %s: %v", method, err)
	}
	return values
}

func (c v2Chain) uint(to common.Address, method string, args ...any) *big.Int {
	c.t.Helper()
	switch value := c.call(to, method, args...)[0].(type) {
	case *big.Int:
		return value
	case uint64:
		return new(big.Int).SetUint64(value)
	default:
		c.t.Fatalf("%s returned %T", method, value)
		return nil
	}
}

// at mines the next transaction at the given timestamp so the anti-snipe clock is exact.
func (c v2Chain) at(timestamp uint64) {
	c.t.Helper()
	if err := c.rpc.CallContext(c.ctx, nil, "evm_setNextBlockTimestamp", timestamp); err != nil {
		c.t.Fatalf("set next block timestamp: %v", err)
	}
}

// slot0Price reads the pool's sqrtPriceX96 from PoolManager storage: StateLibrary keeps
// Pool.State in mapping slot 6 and sqrtPriceX96 in the low 160 bits of its first word.
func (c v2Chain) slot0Price(poolManager common.Address, poolID common.Hash) *big.Int {
	c.t.Helper()
	slot := crypto.Keccak256Hash(poolID.Bytes(), common.LeftPadBytes(big.NewInt(6).Bytes(), 32))
	word := c.call(poolManager, "extsload", slot)[0].([32]byte)
	price := new(big.Int).SetBytes(word[:])
	return price.And(price, new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 160), big.NewInt(1)))
}

// TestAnvilV2LifecycleAndReorg drives one engine-2 token through launch, taxed and untaxed
// curve buys, a graduation that is reorged away and replayed, V4 swaps both ways, LP fee
// collection and claims, and a fee recipient rotation; then it checks every projection
// against the chain and the public API against the projections (Plan v2-B Task 11).
func TestAnvilV2LifecycleAndReorg(t *testing.T) {
	if os.Getenv("ANVIL_V2_RPC_URL") == "" {
		t.Skip("run through task anvil-v2-indexer-e2e")
	}
	rpcURL := requireAnvilEnvironment(t, "ANVIL_V2_RPC_URL")
	factory := common.HexToAddress(requireAnvilEnvironment(t, "ANVIL_V2_FACTORY"))
	poolManager := common.HexToAddress(requireAnvilEnvironment(t, "ANVIL_V2_POOL_MANAGER"))
	locker := common.HexToAddress(requireAnvilEnvironment(t, "ANVIL_V2_LOCKER"))
	hook := common.HexToAddress(requireAnvilEnvironment(t, "ANVIL_V2_HOOK"))
	router := common.HexToAddress(requireAnvilEnvironment(t, "ANVIL_V2_SWAP_ROUTER"))
	treasury := common.HexToAddress(requireAnvilEnvironment(t, "ANVIL_V2_TREASURY"))
	start, err := strconv.ParseInt(requireAnvilEnvironment(t, "ANVIL_V2_START_BLOCK"), 10, 64)
	if err != nil || start < 0 {
		t.Fatalf("ANVIL_V2_START_BLOCK is invalid: %v", err)
	}
	// Anvil dev accounts 4-6; accounts 0-3 hold the deployment's roles.
	creator := common.HexToAddress("0x15d34AAf54267DB7D7c367839AAf71A00a2C6A65")
	trader := common.HexToAddress("0x9965507D1a55bcC2695C58ba16FB37d819B0A4dc")
	recipient := common.HexToAddress("0x976EA74026E726554dB657fA54763abd0C3a0aa9")
	const chainID int64 = 31_337
	const deploymentID = "anvil-v2-indexer-e2e"

	ctx, cancel := context.WithTimeout(t.Context(), 120*time.Second)
	defer cancel()
	database := postgrestest.NewMigrated(t)
	pool, err := storepostgres.OpenPool(ctx, database.URL, storepostgres.PoolOptions{})
	if err != nil {
		t.Fatalf("open indexer pool: %v", err)
	}
	defer pool.Close()
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
	// newStepper wires an indexer to a database the way cmd/indexer does for engine 2. One
	// Step covers the whole chunk up to the head, so each call indexes everything sent.
	newStepper := func(pool *pgxpool.Pool) func(label string) {
		t.Helper()
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
		return func(label string) {
			t.Helper()
			if advanced, err := engine.Step(ctx); err != nil || !advanced {
				t.Fatalf("%s: advanced=%t err=%v", label, advanced, err)
			}
		}
	}
	step := newStepper(pool)
	rpcClient, err := gethrpc.DialContext(ctx, rpcURL)
	if err != nil {
		t.Fatal(err)
	}
	defer rpcClient.Close()
	fixture, err := abi.JSON(strings.NewReader(v2FixtureABI))
	if err != nil {
		t.Fatal(err)
	}
	c := v2Chain{t: t, ctx: ctx, rpc: rpcClient, fixture: fixture}
	ether := big.NewInt(1_000_000_000_000_000_000)
	deadline := big.NewInt(time.Now().Add(24 * time.Hour).Unix())

	// Launch with a developer buy, then a taxed buy inside the anti-snipe window and an
	// untaxed one after it.
	developerBuy := new(big.Int).Div(ether, big.NewInt(100))
	launchValue := new(big.Int).Add(c.uint(factory, "launchFee"), developerBuy)
	c.send(creator, factory, launchValue, c.pack("launch", struct {
		Name                  string
		Symbol                string
		EngineVersion         uint16
		DeveloperBuyGross     *big.Int
		MinDeveloperTokensOut *big.Int
		Deadline              *big.Int
	}{"Anvil V2", "AV2", 2, developerBuy, big.NewInt(0), deadline}))
	step("index launch")
	var tokenBytes, curveBytes []byte
	if err := database.DB.QueryRowContext(ctx, `SELECT token_address, curve_address FROM tokens WHERE chain_id=$1 AND engine_version=2`, chainID).Scan(&tokenBytes, &curveBytes); err != nil {
		t.Fatalf("read launched v2 token: %v", err)
	}
	token, curveAddress := common.BytesToAddress(tokenBytes), common.BytesToAddress(curveBytes)
	launchTime := c.uint(curveAddress, "launchTime").Uint64()

	c.at(launchTime + 10)
	c.send(trader, curveAddress, new(big.Int).Div(ether, big.NewInt(10)), packBuy(t, trader))
	c.at(launchTime + 61)
	c.send(trader, curveAddress, new(big.Int).Div(ether, big.NewInt(10)), packBuy(t, trader))
	step("index curve buys")
	taxes := queryStrings(t, ctx, database.DB, `SELECT snipe_tax::text FROM trades WHERE chain_id=$1 AND token_address=$2 AND trader=$3 ORDER BY block_number, log_index`, chainID, token.Bytes(), trader.Bytes())
	if len(taxes) != 2 || taxes[0] == "0" || taxes[1] != "0" {
		t.Fatalf("trader snipe taxes = %v, want [taxed 0]", taxes)
	}

	// Graduate, then reorg the graduation away: the replacement chain has no graduation, so
	// every V4 projection must disappear with it.
	var snapshotID string
	if err := rpcClient.CallContext(ctx, &snapshotID, "evm_snapshot"); err != nil {
		t.Fatal(err)
	}
	graduate := func() {
		t.Helper()
		c.send(trader, curveAddress, new(big.Int).Mul(big.NewInt(10), ether), packBuy(t, trader))
	}
	graduate()
	step("index graduation")
	assertCount(t, ctx, database.DB, 1, `SELECT count(*) FROM v4_pools WHERE chain_id=$1`, chainID)
	var reverted bool
	if err := rpcClient.CallContext(ctx, &reverted, "evm_revert", snapshotID); err != nil || !reverted {
		t.Fatalf("revert Anvil: reverted=%t err=%v", reverted, err)
	}
	c.send(trader, recipient, big.NewInt(1), nil)
	step("recover reorg")
	step("index replacement block")
	assertCount(t, ctx, database.DB, 1, `SELECT count(*) FROM indexer_reorgs WHERE chain_id=$1 AND deployment_id=$2`, chainID, deploymentID)
	for _, query := range []string{
		`SELECT count(*) FROM graduations WHERE chain_id=$1`,
		`SELECT count(*) FROM v4_pools WHERE chain_id=$1`,
		`SELECT count(*) FROM v4_pool_initializations WHERE chain_id=$1`,
		`SELECT count(*) FROM v4_swaps WHERE chain_id=$1`,
		`SELECT count(*) FROM token_reserves WHERE chain_id=$1 AND reserve_source='v4'`,
	} {
		assertCount(t, ctx, database.DB, 0, query, chainID)
	}
	if phase := queryStrings(t, ctx, database.DB, `SELECT phase FROM tokens WHERE chain_id=$1 AND token_address=$2`, chainID, token.Bytes()); len(phase) != 1 || phase[0] != "curve" {
		t.Fatalf("phase after reorg = %v, want curve", phase)
	}

	graduate()
	step("index replayed graduation")
	var poolIDBytes []byte
	if err := database.DB.QueryRowContext(ctx, `SELECT pool_id FROM v4_pools WHERE chain_id=$1 AND token_address=$2`, chainID, token.Bytes()).Scan(&poolIDBytes); err != nil {
		t.Fatalf("read replayed v4 pool: %v", err)
	}
	poolID := common.BytesToHash(poolIDBytes)

	// V4 swaps in both directions through the test router; then the LP fees are collected and
	// claimed, the curve creator fees are claimed, and the fee recipient rotates.
	key := v4PoolKey{Currency0: common.Address{}, Currency1: token, Fee: big.NewInt(10_000), TickSpacing: big.NewInt(200), Hooks: hook}
	minPrice, _ := new(big.Int).SetString("4295128740", 10)
	maxPrice, _ := new(big.Int).SetString("1461446703485210103287273052203988822378723970341", 10)
	swapIn := func(zeroForOne bool, amountIn *big.Int) {
		t.Helper()
		limit, value := maxPrice, big.NewInt(0)
		if zeroForOne {
			limit, value = minPrice, amountIn
		}
		c.send(trader, router, value, c.pack("swap", key, v4SwapParams{ZeroForOne: zeroForOne, AmountSpecified: new(big.Int).Neg(amountIn), SqrtPriceLimitX96: limit}, v4TestSettings{}, []byte{}))
	}
	swapIn(true, new(big.Int).Div(ether, big.NewInt(2)))
	maxApproval := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
	c.send(trader, token, big.NewInt(0), c.pack("approve", router, maxApproval))
	swapIn(false, new(big.Int).Div(c.uint(token, "balanceOf", trader), big.NewInt(4)))
	c.send(trader, locker, big.NewInt(0), c.pack("collect", token))
	c.send(creator, locker, big.NewInt(0), c.pack("claim", token))
	c.send(creator, curveAddress, big.NewInt(0), c.pack("claimCreatorFees"))
	c.send(creator, curveAddress, big.NewInt(0), c.pack("proposeFeeRecipient", recipient))
	c.send(recipient, curveAddress, big.NewInt(0), c.pack("acceptFeeRecipient"))
	swapIn(true, new(big.Int).Div(ether, big.NewInt(4)))
	c.send(trader, locker, big.NewInt(0), c.pack("collect", token))
	c.send(recipient, locker, big.NewInt(0), c.pack("claim", token))
	c.send(treasury, locker, big.NewInt(0), c.pack("claim", token))
	// A last ETH-in swap leaves an ETH fee in the position (V4 charges the input currency),
	// and a collect leaves the rotated recipient owed.
	swapIn(true, new(big.Int).Div(ether, big.NewInt(8)))
	c.send(trader, locker, big.NewInt(0), c.pack("collect", token))
	step("index V4 lifecycle")

	// Projections against the chain.
	assertCount(t, ctx, database.DB, 4, `SELECT count(*) FROM v4_swaps WHERE chain_id=$1`, chainID)
	assertCount(t, ctx, database.DB, 3, `SELECT count(*) FROM lp_fee_collections WHERE chain_id=$1`, chainID)
	assertCount(t, ctx, database.DB, 3, `SELECT count(*) FROM lp_fee_claims WHERE chain_id=$1`, chainID)
	assertCount(t, ctx, database.DB, 1, `SELECT count(*) FROM fee_recipient_acceptances WHERE chain_id=$1`, chainID)
	for _, account := range []common.Address{creator, recipient, treasury} {
		row := queryStrings(t, ctx, database.DB, `SELECT curve_eth_owed::text || ',' || locker_eth_owed::text || ',' || locker_token_owed::text FROM creator_fee_balances WHERE chain_id=$1 AND token_address=$2 AND account=$3`, chainID, token.Bytes(), account.Bytes())
		got := "0,0,0"
		if len(row) == 1 {
			got = row[0]
		}
		owed := c.call(locker, "owed", token, account)
		want := c.uint(curveAddress, "creatorFeesOwed", account).String() + "," + owed[0].(*big.Int).String() + "," + owed[1].(*big.Int).String()
		if got != want {
			t.Fatalf("creator_fee_balances[%s] = %s, chain = %s", account.Hex(), got, want)
		}
	}
	if owed := c.call(locker, "owed", token, recipient); owed[0].(*big.Int).Sign() == 0 {
		t.Fatal("the rotated recipient is owed no LP ETH; the lifecycle did not exercise it")
	}
	prices := queryStrings(t, ctx, database.DB, `SELECT sqrt_price_x96::text FROM token_reserves WHERE chain_id=$1 AND token_address=$2 AND reserve_source='v4'`, chainID, token.Bytes())
	if want := c.slot0Price(poolManager, poolID).String(); len(prices) != 1 || prices[0] != want {
		t.Fatalf("token_reserves sqrt_price_x96 = %v, slot0 = %s", prices, want)
	}
	holders := queryStrings(t, ctx, database.DB, `SELECT '0x' || encode(holder_address, 'hex') || ',' || balance::text FROM holder_balances WHERE chain_id=$1 AND token_address=$2 AND balance > 0`, chainID, token.Bytes())
	if len(holders) < 4 {
		t.Fatalf("holder_balances = %v, want the curve trader, creator, locker and PoolManager", holders)
	}
	for _, holder := range holders {
		address, balance, _ := strings.Cut(holder, ",")
		if want := c.uint(token, "balanceOf", common.HexToAddress(address)).String(); balance != want {
			t.Fatalf("holder_balances[%s] = %s, balanceOf = %s", address, balance, want)
		}
	}
	incremental := v2ProjectionRows(t, ctx, database.DB, pool, chainID, token)
	if err := storepostgres.NewAdapter(pool).RebuildTokenProjections(ctx, chainID, token); err != nil {
		t.Fatalf("rebuild token projections: %v", err)
	}
	if rebuilt := v2ProjectionRows(t, ctx, database.DB, pool, chainID, token); !reflect.DeepEqual(incremental, rebuilt) {
		t.Fatalf("rebuild differs from incremental\nincremental=%v\nrebuilt=%v", incremental, rebuilt)
	}
	// Indexing the final chain from scratch must land on the reorg-recovered projections.
	fresh := postgrestest.NewMigrated(t)
	freshPool, err := storepostgres.OpenPool(ctx, fresh.URL, storepostgres.PoolOptions{})
	if err != nil {
		t.Fatalf("open fresh indexer pool: %v", err)
	}
	defer freshPool.Close()
	newStepper(freshPool)("index the final chain from scratch")
	if replayed := v2ProjectionRows(t, ctx, fresh.DB, freshPool, chainID, token); !reflect.DeepEqual(incremental, replayed) {
		t.Fatalf("fresh index differs from the reorg-recovered one\nrecovered=%v\nfresh=%v", incremental, replayed)
	}

	// The running reconciliation pass agrees with the chain for the graduated token.
	if result := assertReconciled(t, ctx, pool, source, chainID, deploymentID); result.Tokens == 0 {
		t.Fatal("reconciliation sampled no token")
	}

	// The public API serves the same facts.
	tokens := storepostgres.TokenReader{Pool: pool, DeploymentID: deploymentID}
	server := apiserver.New(apiserver.DefaultConfig(), apiserver.ReadyFunc(func(context.Context) error { return nil }), nil)
	server.RegisterPublicRoutes(apiserver.PublicRoutes{Tokens: tokens, Market: storepostgres.MarketReader{Pool: pool, DeploymentID: deploymentID}, Protocol: storepostgres.ProtocolReader{Pool: pool, DeploymentID: deploymentID}, ChainID: chainID})
	server.RegisterQuoteRoutes(apiserver.QuoteRoutes{Provider: quote.Service{Reader: tokens, ChainID: chainID}})
	httpServer := httptest.NewServer(server.Handler)
	defer httpServer.Close()
	var detail struct {
		Phase string `json:"phase"`
		V2    *struct {
			Pool *struct {
				PoolID string `json:"pool_id"`
				Hook   string `json:"hook"`
			} `json:"pool"`
			FeeRecipient        string  `json:"fee_recipient"`
			PendingFeeRecipient *string `json:"pending_fee_recipient"`
			LockerOwed          []struct {
				Account string `json:"account"`
				ETH     string `json:"eth"`
				Token   string `json:"token"`
			} `json:"locker_owed"`
		} `json:"v2"`
	}
	body := assertStatus(t, http.MethodGet, httpServer.URL+"/v1/tokens/"+token.Hex(), nil, nil, http.StatusOK)
	if err := json.Unmarshal(body, &detail); err != nil {
		t.Fatalf("decode token detail: %v", err)
	}
	if detail.Phase != "graduated" || detail.V2 == nil || detail.V2.Pool == nil || detail.V2.Pool.PoolID != poolID.Hex() || detail.V2.Pool.Hook != hook.Hex() {
		t.Fatalf("token detail = %s", body)
	}
	if detail.V2.FeeRecipient != recipient.Hex() || detail.V2.PendingFeeRecipient != nil {
		t.Fatalf("fee recipient = %s pending %v, want %s and none", detail.V2.FeeRecipient, detail.V2.PendingFeeRecipient, recipient.Hex())
	}
	for _, owed := range detail.V2.LockerOwed {
		onchain := c.call(locker, "owed", token, common.HexToAddress(owed.Account))
		if owed.ETH != onchain[0].(*big.Int).String() || owed.Token != onchain[1].(*big.Int).String() {
			t.Fatalf("API locker_owed[%s] = %s/%s, chain = %v/%v", owed.Account, owed.ETH, owed.Token, onchain[0], onchain[1])
		}
	}
	if len(detail.V2.LockerOwed) == 0 {
		t.Fatalf("API locker_owed is empty; body=%s", body)
	}
	assertStatus(t, http.MethodPost, httpServer.URL+"/v1/tokens/"+token.Hex()+"/quote", strings.NewReader(`{"side":"buy","amount":"1000000000000"}`), map[string]string{"Content-Type": "application/json"}, http.StatusConflict)
}

func queryStrings(t *testing.T, ctx context.Context, db *sql.DB, query string, args ...any) []string {
	t.Helper()
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	defer func() { _ = rows.Close() }()
	var values []string
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			t.Fatalf("%s: %v", query, err)
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return values
}

func assertCount(t *testing.T, ctx context.Context, db *sql.DB, want int, query string, args ...any) {
	t.Helper()
	var got int
	if err := db.QueryRowContext(ctx, query, args...).Scan(&got); err != nil || got != want {
		t.Fatalf("%s = %d (err %v), want %d", query, got, err, want)
	}
}

// v2ProjectionRows renders the token's derived rows as text, so a rebuild or a fresh index can
// be compared with the incremental projection. token_stats is written by the aggregation
// worker (and by reorg recovery), not by an indexer step, so it is recomputed first, as that
// worker would.
func v2ProjectionRows(t *testing.T, ctx context.Context, db *sql.DB, pool *pgxpool.Pool, chainID int64, token common.Address) []string {
	t.Helper()
	if err := storepostgres.NewAdapter(pool).RecomputeTokenStats(ctx, chainID, token); err != nil {
		t.Fatalf("recompute token stats: %v", err)
	}
	var rows []string
	for _, query := range []string{
		`SELECT row(t.phase)::text FROM tokens t WHERE chain_id=$1 AND token_address=$2`,
		`SELECT row(r.*)::text FROM token_reserves r WHERE chain_id=$1 AND token_address=$2`,
		`SELECT row(h.*)::text FROM holder_balances h WHERE chain_id=$1 AND token_address=$2 ORDER BY holder_address`,
		`SELECT row(b.*)::text FROM creator_fee_balances b WHERE chain_id=$1 AND token_address=$2 ORDER BY account`,
		`SELECT row(p.*)::text FROM v4_pools p WHERE chain_id=$1 AND token_address=$2`,
		`SELECT row(s.spot_price_eth_wad, s.market_cap_eth_wad, s.liquidity_eth_wad)::text FROM token_stats s WHERE chain_id=$1 AND token_address=$2`,
	} {
		rows = append(rows, queryStrings(t, ctx, db, query, chainID, token.Bytes())...)
	}
	return rows
}
