//go:build integration

package indexer_test

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	stdimage "image"
	"image/png"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Contictus/launchtap/backend/internal/apiserver"
	"github.com/Contictus/launchtap/backend/internal/indexer"
	"github.com/Contictus/launchtap/backend/internal/privyauth"
	"github.com/Contictus/launchtap/backend/internal/quote"
	"github.com/Contictus/launchtap/backend/internal/realtime"
	storepostgres "github.com/Contictus/launchtap/backend/internal/store/postgres"
	"github.com/Contictus/launchtap/backend/internal/store/postgres/postgrestest"
	"github.com/Contictus/launchtap/backend/internal/token"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	gethrpc "github.com/ethereum/go-ethereum/rpc"
	"github.com/jackc/pgx/v5/pgxpool"
)

func exercisePlan3API(t *testing.T, ctx context.Context, database *postgrestest.Database, pool *pgxpool.Pool, engine *indexer.Engine, rpcURL string, chainID int64) {
	t.Helper()
	var tokenBytes, curveBytes, creatorBytes []byte
	if err := database.DB.QueryRowContext(ctx, `SELECT token_address, curve_address, creator FROM tokens WHERE chain_id=$1`, chainID).Scan(&tokenBytes, &curveBytes, &creatorBytes); err != nil {
		t.Fatal(err)
	}
	tokenAddress, curveAddress, creator := common.BytesToAddress(tokenBytes), common.BytesToAddress(curveBytes), common.BytesToAddress(creatorBytes)
	verifier, signer := generatedVerifier(t)
	hub := realtime.NewHub(16, 4)
	listenerContext, stopListener := context.WithCancel(ctx)
	defer stopListener()
	ready := make(chan struct{})
	listenerErrors := make(chan error, 1)
	go func() {
		listenerErrors <- storepostgres.ListenAPIRefreshReady(listenerContext, pool, ready, func(payload []byte) {
			if event, err := realtime.Decode(payload); err == nil {
				hub.Publish(event)
			}
		})
	}()
	select {
	case <-ready:
	case err := <-listenerErrors:
		t.Fatalf("start refresh listener: %v", err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}

	tokens := storepostgres.TokenReader{Pool: pool, DeploymentID: "anvil-indexer-e2e"}
	market := storepostgres.MarketReader{Pool: pool, DeploymentID: "anvil-indexer-e2e"}
	server := apiserver.New(apiserver.DefaultConfig(), apiserver.ReadyFunc(func(context.Context) error { return nil }), nil)
	server.RegisterTokenRoutes(apiserver.TokenRoutes{Reader: tokens, ChainID: chainID})
	server.RegisterCandleRoutes(apiserver.CandleRoutes{Reader: storepostgres.CandleReader{Pool: pool, DeploymentID: "anvil-indexer-e2e"}, ChainID: chainID})
	server.RegisterPublicRoutes(apiserver.PublicRoutes{Tokens: tokens, Market: market, Protocol: storepostgres.ProtocolReader{Pool: pool, DeploymentID: "anvil-indexer-e2e"}, ChainID: chainID})
	server.RegisterQuoteRoutes(apiserver.QuoteRoutes{Provider: quote.Service{Reader: tokens, ChainID: chainID}})
	server.RegisterMetadataRoutes(apiserver.MetadataRoutes{Store: storepostgres.MetadataStore{Pool: pool, DeploymentID: "anvil-indexer-e2e"}, Verifier: verifier, ChainID: chainID})
	server.RegisterEventRoutes(apiserver.EventRoutes{Hub: hub, ChainID: chainID, DeploymentID: "anvil-indexer-e2e", Heartbeat: 50 * time.Millisecond})
	httpServer := httptest.NewServer(server.Handler)
	defer httpServer.Close()

	assertStatus(t, http.MethodGet, httpServer.URL+"/v1/tokens?phase=curve&sort=newest", nil, nil, http.StatusOK)
	assertStatus(t, http.MethodGet, httpServer.URL+"/v1/tokens/"+tokenAddress.Hex(), nil, nil, http.StatusOK)
	page, err := tokens.List(ctx, token.ListQuery{ChainID: chainID, Phase: "curve", Sort: "newest", Limit: 20})
	if err != nil {
		t.Fatalf("read indexed token list directly: %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].Address != tokenAddress {
		t.Fatalf("indexed token list = %+v; want exactly %s", page.Items, tokenAddress.Hex())
	}
	if _, err := tokens.Get(ctx, chainID, tokenAddress); err != nil {
		t.Fatalf("read indexed token detail directly: %v", err)
	}
	quoteResponse := assertStatus(t, http.MethodPost, httpServer.URL+"/v1/tokens/"+tokenAddress.Hex()+"/quote", strings.NewReader(`{"side":"buy","amount":"1000000000000"}`), map[string]string{"Content-Type": "application/json"}, http.StatusOK)
	if !bytes.Contains(quoteResponse, []byte(`"informational":true`)) {
		t.Fatalf("quote=%s", quoteResponse)
	}

	rpcClient, err := gethrpc.DialContext(ctx, rpcURL)
	if err != nil {
		t.Fatal(err)
	}
	defer rpcClient.Close()
	var snapshotID string
	if err := rpcClient.CallContext(ctx, &snapshotID, "evm_snapshot"); err != nil {
		t.Fatal(err)
	}
	buyData := packBuy(t, creator)
	transaction := map[string]any{"from": creator.Hex(), "to": curveAddress.Hex(), "value": "0x38d7ea4c68000", "gas": "0x989680", "data": "0x" + fmt.Sprintf("%x", buyData)}
	var simulation string
	if err := rpcClient.CallContext(ctx, &simulation, "eth_call", transaction, "latest"); err != nil {
		t.Fatalf("simulate second buy: %v", err)
	}
	var buyHash common.Hash
	if err := rpcClient.CallContext(ctx, &buyHash, "eth_sendTransaction", transaction); err != nil {
		t.Fatalf("second buy: %v", err)
	}
	waitTransactionReceipt(t, ctx, rpcClient, buyHash)
	if advanced, err := engine.Step(ctx); err != nil || !advanced {
		t.Fatalf("index second buy: advanced=%t err=%v", advanced, err)
	}
	tradesBody := assertStatus(t, http.MethodGet, httpServer.URL+"/v1/tokens/"+tokenAddress.Hex()+"/trades?limit=1", nil, nil, http.StatusOK)
	var tradesPage struct {
		NextCursor string            `json:"next_cursor"`
		Items      []json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(tradesBody, &tradesPage); err != nil || len(tradesPage.Items) != 1 || tradesPage.NextCursor == "" {
		t.Fatalf("trades page=%s err=%v", tradesBody, err)
	}

	eventsContext, stopEvents := context.WithCancel(ctx)
	defer stopEvents()
	eventsRequest, _ := http.NewRequestWithContext(eventsContext, http.MethodGet, httpServer.URL+"/v1/events", nil)
	eventsResponse, err := http.DefaultClient.Do(eventsRequest)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = eventsResponse.Body.Close() })
	eventsReader := bufio.NewReader(eventsResponse.Body)
	readStreamUntil(t, eventsReader, "refresh-only", time.Second)
	access, identity := signer.tokens(t, creator)
	headers := map[string]string{"Authorization": "Bearer " + access, "privy-id-token": identity, "If-Match": "0", "Content-Type": "application/json"}
	assertStatus(t, http.MethodPut, httpServer.URL+"/v1/tokens/"+tokenAddress.Hex()+"/metadata", strings.NewReader(`{"description":"Anvil metadata","x_url":"https://x.com/anvil"}`), headers, http.StatusOK)
	readStreamUntil(t, eventsReader, tokenAddress.Hex(), time.Second)
	imageHeaders := map[string]string{"Authorization": "Bearer " + access, "privy-id-token": identity, "If-Match": "0", "Content-Type": "image/png"}
	var image bytes.Buffer
	if err := png.Encode(&image, stdimage.NewGray(stdimage.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	assertStatus(t, http.MethodPut, httpServer.URL+"/v1/tokens/"+tokenAddress.Hex()+"/image", bytes.NewReader(image.Bytes()), imageHeaders, http.StatusOK)
	assertStatus(t, http.MethodGet, httpServer.URL+"/v1/tokens/"+tokenAddress.Hex()+"/image", nil, nil, http.StatusOK)

	var reverted bool
	if err := rpcClient.CallContext(ctx, &reverted, "evm_revert", snapshotID); err != nil || !reverted {
		t.Fatalf("revert Anvil: reverted=%t err=%v", reverted, err)
	}
	var alternateHash common.Hash
	if err := rpcClient.CallContext(ctx, &alternateHash, "eth_sendTransaction", map[string]any{"from": creator.Hex(), "to": creator.Hex(), "value": "0x1"}); err != nil {
		t.Fatalf("alternate transaction: %v", err)
	}
	waitTransactionReceipt(t, ctx, rpcClient, alternateHash)
	if advanced, err := engine.Step(ctx); err != nil || !advanced {
		t.Fatalf("recover shallow reorg: advanced=%t err=%v", advanced, err)
	}
	if advanced, err := engine.Step(ctx); err != nil || !advanced {
		t.Fatalf("index replacement block: advanced=%t err=%v", advanced, err)
	}
	readStreamUntil(t, eventsReader, "event: reorg", time.Second)
	assertStatus(t, http.MethodGet, httpServer.URL+"/v1/tokens/"+tokenAddress.Hex()+"/trades?limit=1&cursor="+tradesPage.NextCursor, nil, nil, http.StatusConflict)
	assertStatus(t, http.MethodGet, httpServer.URL+"/v1/tokens/"+tokenAddress.Hex(), nil, nil, http.StatusOK)
	assertStatus(t, http.MethodGet, httpServer.URL+"/v1/tokens/"+tokenAddress.Hex()+"/image", nil, nil, http.StatusOK)
}

func waitTransactionReceipt(t *testing.T, ctx context.Context, client *gethrpc.Client, hash common.Hash) {
	t.Helper()
	for {
		var receipt *types.Receipt
		if err := client.CallContext(ctx, &receipt, "eth_getTransactionReceipt", hash); err != nil {
			t.Fatalf("read transaction receipt %s: %v", hash, err)
		}
		if receipt != nil {
			if receipt.Status != types.ReceiptStatusSuccessful {
				var trace json.RawMessage
				_ = client.CallContext(ctx, &trace, "debug_traceTransaction", hash, map[string]any{"tracer": "callTracer"})
				t.Fatalf("transaction %s reverted gas_used=%d trace=%s", hash, receipt.GasUsed, trace)
			}
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(10 * time.Millisecond):
		}
	}
}

type testSigner struct{ private *ecdsa.PrivateKey }

func generatedVerifier(t *testing.T) (*privyauth.ES256Verifier, testSigner) {
	t.Helper()
	private, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKIXPublicKey(&private.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := privyauth.NewES256Verifier("anvil-app", string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})))
	if err != nil {
		t.Fatal(err)
	}
	return verifier, testSigner{private: private}
}
func (signer testSigner) tokens(t *testing.T, wallet common.Address) (string, string) {
	t.Helper()
	now := time.Now().Unix()
	base := map[string]any{"iss": "privy.io", "aud": "anvil-app", "sub": "did:privy:anvil", "iat": now - 1, "exp": now + 300}
	access := signer.sign(t, base)
	identityClaims := make(map[string]any, len(base)+1)
	for key, value := range base {
		identityClaims[key] = value
	}
	accounts, _ := json.Marshal([]map[string]any{{"type": "wallet", "chain_type": "ethereum", "address": wallet.Hex()}})
	identityClaims["linked_accounts"] = string(accounts)
	return access, signer.sign(t, identityClaims)
}
func (signer testSigner) sign(t *testing.T, claims map[string]any) string {
	t.Helper()
	header, _ := json.Marshal(map[string]string{"alg": "ES256", "typ": "JWT"})
	payload, _ := json.Marshal(claims)
	a, b := base64.RawURLEncoding.EncodeToString(header), base64.RawURLEncoding.EncodeToString(payload)
	digest := sha256.Sum256([]byte(a + "." + b))
	r, s, err := ecdsa.Sign(rand.Reader, signer.private, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	signature := append(r.FillBytes(make([]byte, 32)), s.FillBytes(make([]byte, 32))...)
	return a + "." + b + "." + base64.RawURLEncoding.EncodeToString(signature)
}
func packBuy(t *testing.T, recipient common.Address) []byte {
	t.Helper()
	contractABI, err := abi.JSON(strings.NewReader(`[{"type":"function","name":"buy","stateMutability":"payable","inputs":[{"name":"tokenRecipient","type":"address"},{"name":"refundRecipient","type":"address"},{"name":"minTokensOut","type":"uint256"},{"name":"deadline","type":"uint256"}],"outputs":[]}]`))
	if err != nil {
		t.Fatal(err)
	}
	data, err := contractABI.Pack("buy", recipient, recipient, big.NewInt(0), big.NewInt(time.Now().Add(time.Hour).Unix()))
	if err != nil {
		t.Fatal(err)
	}
	return data
}
func assertStatus(t *testing.T, method, url string, body io.Reader, headers map[string]string, want int) []byte {
	t.Helper()
	request, err := http.NewRequestWithContext(t.Context(), method, url, body)
	if err != nil {
		t.Fatal(err)
	}
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = response.Body.Close() })
	content, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != want {
		t.Fatalf("%s %s status=%d want=%d body=%s", method, url, response.StatusCode, want, content)
	}
	return content
}
func readStreamUntil(t *testing.T, reader *bufio.Reader, wanted string, timeout time.Duration) string {
	t.Helper()
	result := make(chan string, 1)
	go func() {
		var out strings.Builder
		for {
			line, err := reader.ReadString('\n')
			out.WriteString(line)
			if strings.Contains(out.String(), wanted) || err != nil {
				result <- out.String()
				return
			}
		}
	}()
	select {
	case value := <-result:
		if !strings.Contains(value, wanted) {
			t.Fatalf("stream ended before %q: %q", wanted, value)
		}
		return value
	case <-time.After(timeout):
		t.Fatalf("timed out waiting for %q", wanted)
		return ""
	}
}
