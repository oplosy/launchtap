//go:build integration

package funding_test

import (
	"context"
	"math/big"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"

	"github.com/Contictus/launchtap/backend/internal/funding"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	gethrpc "github.com/ethereum/go-ethereum/rpc"
)

// forwarder is a contract whose fallback CALLs the address in calldata with the call's value:
// initcode copies the 15-byte runtime `CALL(gas, calldataload(0), callvalue, 0, 0, 0, 0)`.
const forwarderInitcode = "0x600f600c600039600f6000f3" + "6000600060006000346000355af100"

// Anvil's first two dev accounts (unlocked).
var (
	devZero = common.HexToAddress("0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266")
	devOne  = common.HexToAddress("0x70997970C51812dc3A010C7d01b50e0d17dc79C8")
)

func anvilBinary(t *testing.T) string {
	t.Helper()
	if path := os.Getenv("ANVIL_BIN"); path != "" {
		return path
	}
	if path, err := exec.LookPath("anvil"); err == nil {
		return path
	}
	if home, err := os.UserHomeDir(); err == nil {
		name := "anvil"
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		if path := filepath.Join(home, ".foundry", "bin", name); fileExists(path) {
			return path
		}
	}
	t.Skip("anvil not found; install Foundry or set ANVIL_BIN")
	return ""
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func startAnvil(t *testing.T, ctx context.Context) *gethrpc.Client {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(anvilBinary(t), "--port", strconv.Itoa(port), "--silent")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start anvil: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	url := "http://127.0.0.1:" + strconv.Itoa(port)
	for {
		client, err := gethrpc.DialContext(ctx, url)
		if err == nil {
			var id hexutil.Uint64
			if err = client.CallContext(ctx, &id, "eth_chainId"); err == nil {
				t.Cleanup(client.Close)
				return client
			}
			client.Close()
		}
		select {
		case <-ctx.Done():
			t.Fatalf("anvil did not start: %v", err)
		case <-time.After(50 * time.Millisecond):
		}
	}
}

type receipt struct {
	BlockNumber     hexutil.Uint64  `json:"blockNumber"`
	Status          hexutil.Uint64  `json:"status"`
	ContractAddress *common.Address `json:"contractAddress"`
}

func waitReceipt(t *testing.T, ctx context.Context, client *gethrpc.Client, hash common.Hash) receipt {
	t.Helper()
	for {
		var out *receipt
		if err := client.CallContext(ctx, &out, "eth_getTransactionReceipt", hash); err != nil {
			t.Fatalf("receipt %s: %v", hash, err)
		}
		if out != nil {
			return *out
		}
		select {
		case <-ctx.Done():
			t.Fatalf("receipt %s: %v", hash, ctx.Err())
		case <-time.After(20 * time.Millisecond):
		}
	}
}

func TestRPCResolveAgainstAnvil(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	client := startAnvil(t, ctx)
	chain := funding.RPC{Client: client}
	ether := big.NewInt(1_000_000_000_000_000_000)

	send := func(tx map[string]any) (common.Hash, uint64) {
		t.Helper()
		var hash common.Hash
		if err := client.CallContext(ctx, &hash, "eth_sendTransaction", tx); err != nil {
			t.Fatalf("send %v: %v", tx, err)
		}
		receipt := waitReceipt(t, ctx, client, hash)
		if receipt.Status != 1 {
			t.Fatalf("transaction %s reverted", hash)
		}
		return hash, uint64(receipt.BlockNumber)
	}
	transfer := func(from, to common.Address, value *big.Int, data []byte) (common.Hash, uint64) {
		return send(map[string]any{"from": from, "to": to, "value": (*hexutil.Big)(value), "data": hexutil.Bytes(data)})
	}

	var forwarder common.Address
	{
		var hash common.Hash
		if err := client.CallContext(ctx, &hash, "eth_sendTransaction", map[string]any{"from": devZero, "data": forwarderInitcode}); err != nil {
			t.Fatal(err)
		}
		receipt := waitReceipt(t, ctx, client, hash)
		if receipt.Status != 1 || receipt.ContractAddress == nil {
			t.Fatalf("deploy forwarder: %+v", receipt)
		}
		forwarder = *receipt.ContractAddress
	}

	topWallet := common.HexToAddress("0x00000000000000000000000000000000000a0001")
	internalWallet := common.HexToAddress("0x00000000000000000000000000000000000a0002")
	sentWallet := common.HexToAddress("0x00000000000000000000000000000000000a0003")
	emptyWallet := common.HexToAddress("0x00000000000000000000000000000000000a0004")

	// A few blocks of padding so the searches have something to search.
	for range 5 {
		transfer(devOne, devZero, big.NewInt(1), nil)
	}
	topTx, topBlock := transfer(devZero, topWallet, ether, nil)
	transfer(devOne, topWallet, ether, nil) // a later top-up must not win
	internalTx, internalBlock := transfer(devOne, forwarder, ether, common.LeftPadBytes(internalWallet.Bytes(), 32))
	sentTx, sentBlock := transfer(devOne, sentWallet, ether, nil)
	if err := client.CallContext(ctx, nil, "anvil_impersonateAccount", sentWallet); err != nil {
		t.Fatal(err)
	}
	transfer(sentWallet, devOne, big.NewInt(1), nil)
	transfer(devZero, sentWallet, ether, nil)
	var head hexutil.Uint64
	if err := client.CallContext(ctx, &head, "eth_blockNumber"); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name   string
		wallet common.Address
		want   funding.Result
	}{
		{"top-level transfer", topWallet, funding.Result{Status: funding.StatusFunded, Funder: devZero, Block: topBlock, TxHash: topTx}},
		{"internal call", internalWallet, funding.Result{Status: funding.StatusFunded, Funder: forwarder, Block: internalBlock, TxHash: internalTx}},
		{"funded before sending", sentWallet, funding.Result{Status: funding.StatusFunded, Funder: devOne, Block: sentBlock, TxHash: sentTx}},
		{"never funded", emptyWallet, funding.Result{Status: funding.StatusUnfunded}},
		{"contract", forwarder, funding.Result{Status: funding.StatusContract}},
	}
	for _, c := range cases {
		got, err := funding.Resolve(ctx, chain, c.wallet, uint64(head))
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		c.want.CheckedBlock = uint64(head)
		if got != c.want {
			t.Fatalf("%s: got %+v, want %+v", c.name, got, c.want)
		}
	}
}
