package chain

import (
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

type rpcRequest struct {
	JSONRPC string            `json:"jsonrpc"`
	ID      json.RawMessage   `json:"id"`
	Method  string            `json:"method"`
	Params  []json.RawMessage `json:"params"`
}

func TestClientReadsLatestSafeAndFinalizedHeadsFromFakeRPC(t *testing.T) {
	t.Parallel()
	numbers := map[string]int64{"latest": 30, "safe": 20, "finalized": 10}
	server := fakeRPCServer(t, func(request rpcRequest) any {
		var tag string
		if err := json.Unmarshal(request.Params[0], &tag); err != nil {
			t.Fatalf("decode block tag: %v", err)
		}
		return testHeader(numbers[tag])
	})
	defer server.Close()
	client, err := Dial(context.Background(), server.URL, RPCConfig{Timeout: time.Second, RetryBackoff: time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	heads, err := client.Heads(context.Background())
	if err != nil {
		t.Fatalf("Heads() error = %v", err)
	}
	if heads.Latest.Number.Uint64() != 30 || heads.Safe.Number.Uint64() != 20 || heads.Finalized.Number.Uint64() != 10 {
		t.Fatalf("head numbers = %d/%d/%d", heads.Latest.Number.Uint64(), heads.Safe.Number.Uint64(), heads.Finalized.Number.Uint64())
	}
}

func TestClientReturnsTypedUnsupportedFinalityError(t *testing.T) {
	t.Parallel()
	server := fakeRPCServer(t, func(request rpcRequest) any {
		var tag string
		_ = json.Unmarshal(request.Params[0], &tag)
		if tag == "safe" {
			return rpcFailure{Code: -32602, Message: "unsupported block tag"}
		}
		return testHeader(30)
	})
	defer server.Close()
	client, _ := Dial(context.Background(), server.URL, RPCConfig{Timeout: time.Second, RetryBackoff: time.Millisecond})
	defer client.Close()
	_, err := client.Heads(context.Background())
	if !errors.Is(err, ErrFinalityUnsupported) {
		t.Fatalf("Heads() error = %v, want ErrFinalityUnsupported", err)
	}
}

func TestClientRetriesTransientRPCError(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	server := fakeRPCServer(t, func(rpcRequest) any {
		if calls.Add(1) < 3 {
			return rpcFailure{Code: -32000, Message: "temporary upstream error"}
		}
		return testHeader(42)
	})
	defer server.Close()
	client, _ := Dial(context.Background(), server.URL, RPCConfig{Timeout: time.Second, MaxRetries: 2, RetryBackoff: time.Millisecond})
	defer client.Close()
	header, err := client.HeaderByNumber(context.Background(), 42)
	if err != nil || header.Number.Uint64() != 42 {
		t.Fatalf("HeaderByNumber() = %v, %v", header, err)
	}
	if calls.Load() != 3 {
		t.Fatalf("calls = %d, want 3", calls.Load())
	}
}

func TestClientReadsCodeLogsAndHeaderByHashFromFakeRPC(t *testing.T) {
	t.Parallel()
	expectedLog := types.Log{Address: common.HexToAddress("0x1234"), Topics: []common.Hash{common.HexToHash("0xab")}, Data: []byte{1, 2}, BlockNumber: 7, TxHash: common.HexToHash("0xcd"), TxIndex: 2, BlockHash: common.HexToHash("0xef"), Index: 3}
	server := fakeRPCServer(t, func(request rpcRequest) any {
		switch request.Method {
		case "eth_getCode":
			return "0x6000"
		case "eth_getLogs":
			return []types.Log{expectedLog}
		case "eth_getBlockByHash":
			return testHeader(7)
		default:
			return rpcFailure{Code: -32601, Message: "unexpected method"}
		}
	})
	defer server.Close()
	client, _ := Dial(context.Background(), server.URL, RPCConfig{Timeout: time.Second, RetryBackoff: time.Millisecond})
	defer client.Close()
	code, err := client.CodeAt(context.Background(), expectedLog.Address)
	if err != nil || string(code) != string([]byte{0x60, 0x00}) {
		t.Fatalf("CodeAt() = %x, %v", code, err)
	}
	logs, err := client.FilterLogs(context.Background(), ethereum.FilterQuery{FromBlock: big.NewInt(7), ToBlock: big.NewInt(7), Addresses: []common.Address{expectedLog.Address}})
	if err != nil || len(logs) != 1 || logs[0].Index != expectedLog.Index {
		t.Fatalf("FilterLogs() = %#v, %v", logs, err)
	}
	header, err := client.HeaderByHash(context.Background(), common.HexToHash("0xef"))
	if err != nil || header.Number.Uint64() != 7 {
		t.Fatalf("HeaderByHash() = %#v, %v", header, err)
	}
}

func TestClientFetchesHeadersInOneBatch(t *testing.T) {
	t.Parallel()
	var batches atomic.Int32
	missing := uint64(99)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var calls []rpcRequest
		if err := json.NewDecoder(request.Body).Decode(&calls); err != nil {
			t.Errorf("batch request was not a JSON array: %v", err)
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		batches.Add(1)
		responses := make([]map[string]any, 0, len(calls))
		for _, call := range calls {
			var number string
			if call.Method != "eth_getBlockByNumber" || json.Unmarshal(call.Params[0], &number) != nil {
				t.Errorf("unexpected batch element %+v", call)
			}
			parsed, _ := new(big.Int).SetString(number[2:], 16)
			response := map[string]any{"jsonrpc": "2.0", "id": call.ID, "result": testHeader(parsed.Int64())}
			if parsed.Uint64() == missing {
				response["result"] = nil
			}
			responses = append(responses, response)
		}
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(responses)
	}))
	defer server.Close()
	client, err := Dial(context.Background(), server.URL, RPCConfig{Timeout: time.Second, RetryBackoff: time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	headers, err := client.HeadersByNumbers(context.Background(), []uint64{5, 6, 7})
	if err != nil || len(headers) != 3 || headers[0].Number.Uint64() != 5 || headers[2].Number.Uint64() != 7 {
		t.Fatalf("HeadersByNumbers() = %v, %v", headers, err)
	}
	if batches.Load() != 1 {
		t.Fatalf("batches = %d, want 1", batches.Load())
	}
	if _, err := client.HeadersByNumbers(context.Background(), []uint64{98, missing}); !errors.Is(err, ethereum.NotFound) {
		t.Fatalf("missing header error = %v", err)
	}
}

type rpcFailure struct {
	Code    int
	Message string
}

type codedRPCError struct {
	code    int
	message string
}

func (e codedRPCError) Error() string  { return e.message }
func (e codedRPCError) ErrorCode() int { return e.code }

func TestCapacityClassificationDoesNotConfuseRateLimiting(t *testing.T) {
	t.Parallel()
	if IsCapacityError(codedRPCError{code: -32000, message: "rate limit exceeded"}) {
		t.Fatal("rate limiting was classified as log-query capacity")
	}
	if !IsCapacityError(codedRPCError{code: -32000, message: "logs matched by query exceeds limit of 50000"}) {
		t.Fatal("observed provider capacity error was not classified")
	}
}

func fakeRPCServer(t *testing.T, respond func(rpcRequest) any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		defer func() {
			if err := request.Body.Close(); err != nil {
				t.Errorf("close request body: %v", err)
			}
		}()
		var call rpcRequest
		if err := json.NewDecoder(request.Body).Decode(&call); err != nil {
			t.Errorf("decode request: %v", err)
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		response := map[string]any{"jsonrpc": "2.0", "id": call.ID}
		switch value := respond(call).(type) {
		case rpcFailure:
			response["error"] = map[string]any{"code": value.Code, "message": value.Message}
		default:
			response["result"] = value
		}
		writer.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(writer).Encode(response); err != nil {
			t.Errorf("encode response: %v", err)
		}
	}))
}

func testHeader(number int64) *types.Header {
	return &types.Header{ParentHash: common.HexToHash("0x01"), UncleHash: types.EmptyUncleHash, Root: common.HexToHash("0x02"), TxHash: types.EmptyTxsHash, ReceiptHash: types.EmptyReceiptsHash, Difficulty: big.NewInt(1), Number: big.NewInt(number), GasLimit: 30_000_000, Time: uint64(number), Extra: []byte{}, MixDigest: common.HexToHash("0x03")}
}
