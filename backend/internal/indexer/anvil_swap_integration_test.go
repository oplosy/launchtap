//go:build integration

package indexer_test

import (
	"bytes"
	"context"
	"math/big"
	"os"
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
	gethrpc "github.com/ethereum/go-ethereum/rpc"
)

const swapFixtureABI = `[
{"type":"function","name":"launch","stateMutability":"payable","inputs":[{"name":"request","type":"tuple","components":[{"name":"name","type":"string"},{"name":"symbol","type":"string"},{"name":"engineVersion","type":"uint16"},{"name":"developerBuyGross","type":"uint256"},{"name":"minDeveloperTokensOut","type":"uint256"},{"name":"deadline","type":"uint256"}]}],"outputs":[]},
{"type":"function","name":"approve","stateMutability":"nonpayable","inputs":[{"name":"spender","type":"address"},{"name":"amount","type":"uint256"}],"outputs":[{"name":"","type":"bool"}]},
{"type":"function","name":"swapExactTokensForETH","stateMutability":"nonpayable","inputs":[{"name":"amountIn","type":"uint256"},{"name":"amountOutMin","type":"uint256"},{"name":"path","type":"address[]"},{"name":"to","type":"address"},{"name":"deadline","type":"uint256"}],"outputs":[{"name":"amounts","type":"uint256[]"}]}
]`

// TestAnvilPoolSwapSenderAndReorg graduates a token, sells through the Uniswap router from a
// funded account and asserts the indexer stores that account as pool_swaps.tx_from, then
// reverts the swap block and asserts the row disappears with the reorg.
func TestAnvilPoolSwapSenderAndReorg(t *testing.T) {
	if os.Getenv("ANVIL_INDEXER_RPC_URL") == "" {
		t.Skip("run through task anvil-indexer-e2e")
	}
	rpcURL := requireAnvilEnvironment(t, "ANVIL_INDEXER_RPC_URL")
	factory := common.HexToAddress(requireAnvilEnvironment(t, "ANVIL_INDEXER_FACTORY"))
	router := common.HexToAddress(requireAnvilEnvironment(t, "ANVIL_INDEXER_ROUTER"))
	weth := common.HexToAddress(requireAnvilEnvironment(t, "ANVIL_INDEXER_WETH"))
	start, err := strconv.ParseInt(requireAnvilEnvironment(t, "ANVIL_INDEXER_START_BLOCK"), 10, 64)
	if err != nil || start < 0 {
		t.Fatalf("ANVIL_INDEXER_START_BLOCK is invalid: %v", err)
	}
	// Anvil's first two dev accounts: the trader signs every transaction and the recipient
	// receives the swap proceeds, so tx_from differs from the swap's `to`.
	trader := common.HexToAddress("0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266")
	recipient := common.HexToAddress("0x70997970C51812dc3A010C7d01b50e0d17dc79C8")

	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
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
	decoder, err := chain.NewDecoder(1)
	if err != nil {
		t.Fatal(err)
	}
	discovery, err := chain.NewDiscoverer(source, decoder, factory, 64)
	if err != nil {
		t.Fatal(err)
	}
	const chainID int64 = 31_337
	engine, err := indexer.New(indexer.Settings{
		ChainID: chainID, DeploymentID: "anvil-indexer-e2e", Factory: factory,
		StartBlock: start, ChunkSize: 128, PollInterval: time.Millisecond, Senders: source,
	}, storepostgres.IndexerStore{Pool: pool, ChainID: chainID, DeploymentID: "anvil-indexer-e2e"},
		laggedFinalitySource{client: source, stable: uint64(start)}, discovery, decoder, indexer.LedgerRouter{ChainID: chainID})
	if err != nil {
		t.Fatal(err)
	}
	rpcClient, err := gethrpc.DialContext(ctx, rpcURL)
	if err != nil {
		t.Fatal(err)
	}
	defer rpcClient.Close()
	fixture, err := abi.JSON(strings.NewReader(swapFixtureABI))
	if err != nil {
		t.Fatal(err)
	}
	send := func(to common.Address, value *big.Int, data []byte) {
		t.Helper()
		transaction := map[string]any{"from": trader.Hex(), "to": to.Hex(), "value": "0x" + value.Text(16), "data": "0x" + common.Bytes2Hex(data)}
		var hash common.Hash
		if err := rpcClient.CallContext(ctx, &hash, "eth_sendTransaction", transaction); err != nil {
			t.Fatalf("send transaction to %s: %v", to, err)
		}
		waitTransactionReceipt(t, ctx, rpcClient, hash)
	}
	step := func(label string) {
		t.Helper()
		if advanced, err := engine.Step(ctx); err != nil || !advanced {
			t.Fatalf("%s: advanced=%t err=%v", label, advanced, err)
		}
	}
	deadline := big.NewInt(time.Now().Add(time.Hour).Unix())
	ether := big.NewInt(1_000_000_000_000_000_000)

	launchData, err := fixture.Pack("launch", struct {
		Name                  string
		Symbol                string
		EngineVersion         uint16
		DeveloperBuyGross     *big.Int
		MinDeveloperTokensOut *big.Int
		Deadline              *big.Int
	}{"Swap Anvil", "SWP", 1, big.NewInt(1_000_000_000_000_000), big.NewInt(0), deadline})
	if err != nil {
		t.Fatal(err)
	}
	send(factory, big.NewInt(1_500_000_000_000_000), launchData)
	step("index launch")
	var tokenBytes, curveBytes []byte
	if err := database.DB.QueryRowContext(ctx, `SELECT token_address, curve_address FROM tokens WHERE chain_id=$1 ORDER BY launch_block_number DESC LIMIT 1`, chainID).Scan(&tokenBytes, &curveBytes); err != nil {
		t.Fatalf("read launched token: %v", err)
	}
	tokenAddress, curveAddress := common.BytesToAddress(tokenBytes), common.BytesToAddress(curveBytes)

	send(curveAddress, new(big.Int).Mul(big.NewInt(5), ether), packBuy(t, trader))
	approveData, err := fixture.Pack("approve", router, new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1)))
	if err != nil {
		t.Fatal(err)
	}
	send(tokenAddress, big.NewInt(0), approveData)
	step("index graduation")
	var graduations int
	if err := database.DB.QueryRowContext(ctx, `SELECT count(*) FROM graduations WHERE chain_id=$1`, chainID).Scan(&graduations); err != nil || graduations != 1 {
		t.Fatalf("graduations = %d, err = %v; want 1", graduations, err)
	}

	var snapshotID string
	if err := rpcClient.CallContext(ctx, &snapshotID, "evm_snapshot"); err != nil {
		t.Fatal(err)
	}
	sellData, err := fixture.Pack("swapExactTokensForETH", new(big.Int).Mul(big.NewInt(1000), ether), big.NewInt(0), []common.Address{tokenAddress, weth}, recipient, deadline)
	if err != nil {
		t.Fatal(err)
	}
	send(router, big.NewInt(0), sellData)
	step("index router sell")
	var swaps int
	var txFrom []byte
	if err := database.DB.QueryRowContext(ctx, `SELECT count(*), max(tx_from) FROM pool_swaps WHERE chain_id=$1`, chainID).Scan(&swaps, &txFrom); err != nil {
		t.Fatalf("read pool swap: %v", err)
	}
	if swaps != 1 || !bytes.Equal(txFrom, trader.Bytes()) {
		t.Fatalf("pool_swaps rows=%d tx_from=%x; want 1 row from %x", swaps, txFrom, trader.Bytes())
	}

	var reverted bool
	if err := rpcClient.CallContext(ctx, &reverted, "evm_revert", snapshotID); err != nil || !reverted {
		t.Fatalf("revert Anvil: reverted=%t err=%v", reverted, err)
	}
	var alternateHash common.Hash
	if err := rpcClient.CallContext(ctx, &alternateHash, "eth_sendTransaction", map[string]any{"from": trader.Hex(), "to": recipient.Hex(), "value": "0x1"}); err != nil {
		t.Fatalf("alternate transaction: %v", err)
	}
	waitTransactionReceipt(t, ctx, rpcClient, alternateHash)
	step("recover reorg")
	step("index replacement block")
	if err := database.DB.QueryRowContext(ctx, `SELECT count(*) FROM pool_swaps WHERE chain_id=$1`, chainID).Scan(&swaps); err != nil || swaps != 0 {
		t.Fatalf("pool_swaps rows after reorg = %d, err = %v; want 0", swaps, err)
	}
}
