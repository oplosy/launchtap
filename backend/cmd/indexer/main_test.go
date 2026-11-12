package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Contictus/launchtap/backend/deployments"
	"github.com/Contictus/launchtap/backend/internal/chain"
	storepostgres "github.com/Contictus/launchtap/backend/internal/store/postgres"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/jackc/pgx/v5/pgxpool"
)

type startupRPCRequest struct {
	JSONRPC string            `json:"jsonrpc"`
	ID      json.RawMessage   `json:"id"`
	Method  string            `json:"method"`
	Params  []json.RawMessage `json:"params"`
}

func TestInitializeIndexerResourcesFailsBeforeDatabaseOrOwnership(t *testing.T) {
	deployment, code := startupDeployment()
	tests := []struct {
		name        string
		chainID     string
		codeAddress common.Address
		code        string
		want        error
	}{
		{name: "wrong chain", chainID: "0x1234", want: chain.ErrChainIDMismatch},
		{name: "zero chain", chainID: "0x0", want: chain.ErrChainIDMismatch},
		{name: "malformed chain", chainID: "0xnothex"},
		{name: "overflow chain", chainID: "0x10000000000000000", want: chain.ErrChainIDMismatch},
		{name: "absent factory code", chainID: "0x1237", codeAddress: deployment.Factory, code: "0x", want: chain.ErrBytecodeMismatch},
		{name: "mismatched WETH code", chainID: "0x1237", codeAddress: deployment.WETH, code: "0x6001", want: chain.ErrBytecodeMismatch},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			methods := make([]string, 0, 5)
			var methodsMu sync.Mutex
			server := startupRPCServer(t, test.chainID, code, test.codeAddress, test.code, &methods, &methodsMu)
			defer server.Close()
			client, err := chain.Dial(context.Background(), server.URL, chain.RPCConfig{Timeout: time.Second, RetryBackoff: time.Millisecond})
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()

			poolOpened, ownershipAcquired := false, false
			_, _, err = initializeIndexerResources(context.Background(), client, deployment.ChainID, deployment,
				func() (*pgxpool.Pool, error) {
					poolOpened = true
					return &pgxpool.Pool{}, nil
				},
				func() (*storepostgres.Ownership, error) {
					ownershipAcquired = true
					return &storepostgres.Ownership{}, nil
				},
			)
			if err == nil {
				t.Fatal("initializeIndexerResources() succeeded for invalid RPC identity or deployment")
			}
			if test.want != nil && !errors.Is(err, test.want) {
				t.Fatalf("initializeIndexerResources() error = %v, want errors.Is(_, %v)", err, test.want)
			}
			if poolOpened || ownershipAcquired {
				t.Fatalf("database/ownership reached after failed preflight: pool=%v ownership=%v", poolOpened, ownershipAcquired)
			}
			if !strings.Contains(err.Error(), deployment.DeploymentID) && !strings.Contains(err.Error(), "bytecode") && !strings.Contains(err.Error(), "chain ID") {
				t.Fatalf("startup error lacks verification context: %v", err)
			}
			methodsMu.Lock()
			gotMethods := append([]string(nil), methods...)
			methodsMu.Unlock()
			if len(gotMethods) == 0 || gotMethods[0] != "eth_chainId" {
				t.Fatalf("RPC method order = %v, want eth_chainId first", gotMethods)
			}
			if test.want == chain.ErrChainIDMismatch && len(gotMethods) != 1 {
				t.Fatalf("wrong-chain startup made RPC calls after eth_chainId: %v", gotMethods)
			}
		})
	}
}

func TestInitializeIndexerResourcesVerifiesBeforeOpeningAndAcquiring(t *testing.T) {
	deployment, code := startupDeployment()
	methods := make([]string, 0, 7)
	var methodsMu sync.Mutex
	server := startupRPCServer(t, "0x1237", code, common.Address{}, "", &methods, &methodsMu)
	defer server.Close()
	client, err := chain.Dial(context.Background(), server.URL, chain.RPCConfig{Timeout: time.Second, RetryBackoff: time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	order := make([]string, 0, 2)
	pool, owner, err := initializeIndexerResources(context.Background(), client, deployment.ChainID, deployment,
		func() (*pgxpool.Pool, error) {
			order = append(order, "open-pool")
			return &pgxpool.Pool{}, nil
		},
		func() (*storepostgres.Ownership, error) {
			order = append(order, "acquire-ownership")
			return &storepostgres.Ownership{}, nil
		},
	)
	if err != nil {
		t.Fatalf("initializeIndexerResources() error = %v", err)
	}
	if pool == nil || owner == nil {
		t.Fatalf("resources = %v/%v, want non-nil", pool, owner)
	}
	if fmt.Sprint(order) != "[open-pool acquire-ownership]" {
		t.Fatalf("database stages = %v, want pool then ownership", order)
	}
	methodsMu.Lock()
	gotMethods := append([]string(nil), methods...)
	methodsMu.Unlock()
	if len(gotMethods) != 5 || gotMethods[0] != "eth_chainId" {
		t.Fatalf("RPC verification methods = %v, want chain ID and four code reads before database", gotMethods)
	}
}

func startupDeployment() (deployments.Deployment, map[string]string) {
	addresses := []common.Address{
		common.HexToAddress("0x1001"), common.HexToAddress("0x1002"), common.HexToAddress("0x1003"), common.HexToAddress("0x1004"),
	}
	codes := [][]byte{{0x60, 0x01}, {0x60, 0x02}, {0x60, 0x03}, {0x60, 0x04}}
	runtimeCodes := make(map[string]string, len(addresses))
	for i, address := range addresses {
		runtimeCodes[strings.ToLower(address.Hex())] = hexutil.Encode(codes[i])
	}
	return deployments.Deployment{
		ChainID:             4663,
		DeploymentID:        "robinhood-mainnet",
		EngineVersion:       1,
		Factory:             addresses[0],
		CurveImplementation: addresses[1],
		WETH:                addresses[2],
		UniV2Factory:        addresses[3],
		BytecodeHashes: deployments.BytecodeHashes{
			LaunchFactory:    crypto.Keccak256Hash(codes[0]),
			BondingCurveV1:   crypto.Keccak256Hash(codes[1]),
			WETH:             crypto.Keccak256Hash(codes[2]),
			UniswapV2Factory: crypto.Keccak256Hash(codes[3]),
		},
	}, runtimeCodes
}

func startupRPCServer(t *testing.T, chainID string, code map[string]string, overrideAddress common.Address, overrideCode string, methods *[]string, methodsMu *sync.Mutex) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		defer func() {
			if err := request.Body.Close(); err != nil {
				t.Errorf("close JSON-RPC request body: %v", err)
			}
		}()
		var call startupRPCRequest
		if err := json.NewDecoder(request.Body).Decode(&call); err != nil {
			t.Errorf("decode JSON-RPC request: %v", err)
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		methodsMu.Lock()
		*methods = append(*methods, call.Method)
		methodsMu.Unlock()

		var result any
		switch call.Method {
		case "eth_chainId":
			result = chainID
		case "eth_getCode":
			if len(call.Params) == 0 {
				t.Errorf("eth_getCode missing address")
				result = "0x"
				break
			}
			var address string
			if err := json.Unmarshal(call.Params[0], &address); err != nil {
				t.Errorf("decode eth_getCode address: %v", err)
				result = "0x"
				break
			}
			if overrideAddress != (common.Address{}) && strings.EqualFold(address, overrideAddress.Hex()) {
				result = overrideCode
			} else {
				result = code[strings.ToLower(address)]
			}
		default:
			result = "0x"
		}
		response := map[string]any{"jsonrpc": "2.0", "id": call.ID, "result": result}
		writer.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(writer).Encode(response); err != nil {
			t.Errorf("encode JSON-RPC response: %v", err)
		}
	}))
}
