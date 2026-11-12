package apiserver

import (
	"context"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Contictus/launchtap/backend/internal/pagination"
	"github.com/Contictus/launchtap/backend/internal/token"
	"github.com/ethereum/go-ethereum/common"
)

func detailFixture(engine uint16, phase string) token.Detail {
	address := common.HexToAddress("0x0000000000000000000000000000000000000001")
	now := time.Date(2026, 11, 12, 9, 30, 0, 0, time.UTC)
	return token.Detail{
		Summary:       token.Summary{Address: address, Name: "Alpha", Symbol: "ALP", Phase: phase, LaunchTime: now, TotalSupply: big.NewInt(1000), MarketCapETH: big.NewInt(1), Volume24hETH: big.NewInt(1)},
		Curve:         address,
		Creator:       address,
		EngineVersion: engine,
		ATHAt:         now,
		Snapshot:      pagination.Snapshot{ChainID: 46630, BlockNumber: 42, BlockHash: [32]byte{31: 1}},
		Finality:      "safe",
	}
}

func readDetail(t *testing.T, detail token.Detail) map[string]json.RawMessage {
	t.Helper()
	s := New(DefaultConfig(), ReadyFunc(func(context.Context) error { return nil }), nil)
	s.RegisterPublicRoutes(PublicRoutes{Tokens: publicTokenStub{detail: detail}, Market: publicMarketStub{}, Protocol: protocolStub{}, ChainID: 46630})
	w := httptest.NewRecorder()
	s.Handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/tokens/"+detail.Address.Hex(), nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	return body
}

func TestTokenDetailEngineV1HasNullV2(t *testing.T) {
	body := readDetail(t, detailFixture(1, "curve"))
	if string(body["engine_version"]) != "1" {
		t.Fatalf("engine_version = %s, want 1", body["engine_version"])
	}
	if raw, ok := body["v2"]; !ok || string(raw) != "null" {
		t.Fatalf("v2 = %s (present %t), want null", raw, ok)
	}
}

type v2Wire struct {
	AntiSnipe struct {
		StartBPS        uint16 `json:"start_bps"`
		DurationSeconds uint32 `json:"duration_seconds"`
		EndsAt          string `json:"ends_at"`
	} `json:"anti_snipe"`
	LPCreatorShareBPS uint16 `json:"lp_creator_share_bps"`
	Pool              *struct {
		PoolID          string `json:"pool_id"`
		PositionTokenID string `json:"position_token_id"`
		Hook            string `json:"hook"`
	} `json:"pool"`
	FeeRecipient        string  `json:"fee_recipient"`
	PendingFeeRecipient *string `json:"pending_fee_recipient"`
	LockerOwed          []struct {
		Account string `json:"account"`
		ETH     string `json:"eth"`
		Token   string `json:"token"`
	} `json:"locker_owed"`
}

func readV2(t *testing.T, detail token.Detail) v2Wire {
	t.Helper()
	body := readDetail(t, detail)
	if string(body["engine_version"]) != "2" {
		t.Fatalf("engine_version = %s, want 2", body["engine_version"])
	}
	var facts v2Wire
	if err := json.Unmarshal(body["v2"], &facts); err != nil || string(body["v2"]) == "null" {
		t.Fatalf("v2 = %s: %v", body["v2"], err)
	}
	return facts
}

func TestTokenDetailEngineV2BeforeGraduation(t *testing.T) {
	recipient := common.HexToAddress("0x000000000000000000000000000000000000fEE2")
	pending := common.HexToAddress("0x000000000000000000000000000000000000fEE3")
	detail := detailFixture(2, "curve")
	detail.V2 = &token.V2Facts{
		AntiSnipeStartBPS: 5000, AntiSnipeDurationSeconds: 60, LPCreatorShareBPS: 5000,
		FeeRecipient: recipient, PendingFeeRecipient: &pending,
	}
	facts := readV2(t, detail)
	wantEnds := detail.LaunchTime.Add(60 * time.Second).Format(time.RFC3339)
	if facts.AntiSnipe.StartBPS != 5000 || facts.AntiSnipe.DurationSeconds != 60 || facts.AntiSnipe.EndsAt != wantEnds {
		t.Fatalf("anti_snipe = %+v, want 5000/60 ending %s", facts.AntiSnipe, wantEnds)
	}
	if facts.LPCreatorShareBPS != 5000 || facts.Pool != nil {
		t.Fatalf("lp share %d pool %+v, want 5000 and no pool", facts.LPCreatorShareBPS, facts.Pool)
	}
	if facts.FeeRecipient != recipient.Hex() || facts.PendingFeeRecipient == nil || *facts.PendingFeeRecipient != pending.Hex() {
		t.Fatalf("recipients %s / %v, want %s / %s", facts.FeeRecipient, facts.PendingFeeRecipient, recipient.Hex(), pending.Hex())
	}
	if facts.LockerOwed == nil || len(facts.LockerOwed) != 0 {
		t.Fatalf("locker_owed = %v, want an empty list", facts.LockerOwed)
	}
}

func TestTokenDetailEngineV2AfterGraduation(t *testing.T) {
	recipient := common.HexToAddress("0x000000000000000000000000000000000000fEE2")
	hook := common.HexToAddress("0x0000000000000000000000000000000000002Ac0")
	poolID := common.HexToHash("0x3596ab89d9d574e268be394b0192f214359be8ce39b9b2f9a12f5e7aa08d4860")
	detail := detailFixture(2, "graduated")
	detail.V2 = &token.V2Facts{
		AntiSnipeStartBPS: 5000, AntiSnipeDurationSeconds: 60, LPCreatorShareBPS: 5000,
		Pool:         &token.V2Pool{PoolID: poolID, PositionTokenID: big.NewInt(7), Hook: hook},
		FeeRecipient: recipient,
		LockerOwed:   []token.LockerOwed{{Account: recipient, ETH: big.NewInt(999), Token: big.NewInt(4999)}},
	}
	facts := readV2(t, detail)
	if facts.Pool == nil || facts.Pool.PoolID != poolID.Hex() || facts.Pool.PositionTokenID != "7" || facts.Pool.Hook != hook.Hex() {
		t.Fatalf("pool = %+v, want %s #7 hook %s", facts.Pool, poolID.Hex(), hook.Hex())
	}
	if facts.PendingFeeRecipient != nil {
		t.Fatalf("pending_fee_recipient = %s, want null", *facts.PendingFeeRecipient)
	}
	if len(facts.LockerOwed) != 1 || facts.LockerOwed[0].Account != recipient.Hex() || facts.LockerOwed[0].ETH != "999" || facts.LockerOwed[0].Token != "4999" {
		t.Fatalf("locker_owed = %+v", facts.LockerOwed)
	}
}
