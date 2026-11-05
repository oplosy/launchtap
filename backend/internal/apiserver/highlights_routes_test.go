package apiserver

import (
	"context"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Contictus/launchtap/backend/internal/pagination"
	"github.com/Contictus/launchtap/backend/internal/token"
	"github.com/ethereum/go-ethereum/common"
)

type highlightsReaderStub struct {
	mu          sync.Mutex
	calls       int
	windowStart time.Time
	limit       int
	err         error
	result      token.Highlights
}

func (s *highlightsReaderStub) Highlights(_ context.Context, _ int64, windowStart time.Time, limit int) (token.Highlights, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	s.windowStart, s.limit = windowStart, limit
	if s.err != nil {
		return token.Highlights{}, s.err
	}
	out := s.result
	out.WindowStart = windowStart
	return out, nil
}

func (s *highlightsReaderStub) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func highlightsServer(reader token.HighlightsReader, now func() time.Time) *Server {
	s := New(DefaultConfig(), ReadyFunc(func(context.Context) error { return nil }), nil)
	// Registered after the token routes, as in cmd/api, to prove the literal path still wins.
	s.RegisterPublicRoutes(PublicRoutes{ChainID: 46630})
	s.RegisterHighlightsRoutes(HighlightsRoutes{Reader: reader, ChainID: 46630, Now: now})
	return s
}

func getHighlights(t *testing.T, s *Server) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	s.Handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/tokens/highlights", nil))
	return w
}

func TestHighlightsCached(t *testing.T) {
	now := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	reader := &highlightsReaderStub{}
	s := highlightsServer(reader, func() time.Time { return now })

	for i := 0; i < 2; i++ {
		if w := getHighlights(t, s); w.Code != http.StatusOK {
			t.Fatalf("call %d status=%d body=%s", i, w.Code, w.Body.String())
		}
		now = now.Add(5 * time.Second)
	}
	if reader.callCount() != 1 {
		t.Fatalf("reader calls within TTL = %d, want 1", reader.callCount())
	}
	now = now.Add(11 * time.Second) // 21s after the first read
	if w := getHighlights(t, s); w.Code != http.StatusOK {
		t.Fatalf("status=%d", w.Code)
	}
	if reader.callCount() != 2 {
		t.Fatalf("reader calls after TTL = %d, want 2", reader.callCount())
	}
}

func TestHighlightsCacheExpiresExactlyAtTTL(t *testing.T) {
	now := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	reader := &highlightsReaderStub{}
	s := highlightsServer(reader, func() time.Time { return now })
	getHighlights(t, s)
	now = now.Add(15*time.Second - time.Nanosecond)
	getHighlights(t, s)
	if reader.callCount() != 1 {
		t.Fatalf("calls just inside TTL = %d, want 1", reader.callCount())
	}
	now = now.Add(time.Nanosecond)
	getHighlights(t, s)
	if reader.callCount() != 2 {
		t.Fatalf("calls at TTL = %d, want 2", reader.callCount())
	}
}

func TestHighlightsWireContract(t *testing.T) {
	now := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	a := common.HexToAddress("0x00000000000000000000000000000000000000a1")
	k := common.HexToAddress("0x00000000000000000000000000000000000000b2")
	launch := now.Add(-3 * time.Hour)
	reader := &highlightsReaderStub{result: token.Highlights{
		Trending: []token.TrendingToken{{
			Summary:         token.Summary{Address: a, Name: "Alpha", Symbol: "ALP", Phase: "curve", LaunchTime: launch, TotalSupply: big.NewInt(1000), MarketCapETH: big.NewInt(5), Volume24hETH: big.NewInt(9)},
			WindowVolumeETH: big.NewInt(12), DistinctBuyers: 3,
		}},
		King: &token.KingOfTheHill{
			Summary:               token.Summary{Address: k, Name: "King", Symbol: "KNG", Phase: "curve", LaunchTime: launch, TotalSupply: big.NewInt(1000)},
			GraduationProgressBPS: 8000, RemainingETH: big.NewInt(20),
		},
		Snapshot: pagination.Snapshot{ChainID: 46630, BlockNumber: 42, BlockHash: [32]byte{31: 1}},
		Finality: "safe",
	}}
	s := highlightsServer(reader, func() time.Time { return now })
	w := getHighlights(t, s)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	for _, want := range []string{
		`"trending":[`, `"window_volume_eth":"12"`, `"distinct_buyers":3`, `"symbol":"ALP"`,
		`"king_of_the_hill":{`, `"graduation_progress_bps":8000`, `"remaining_eth":"20"`,
		`"window_start":"2026-11-05T11:00:00Z"`, `"as_of":"2026-11-05T12:00:00Z"`, `"finality":"safe"`,
		`"as_of_block":42`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("body missing %s: %s", want, body)
		}
	}
	if reader.limit != 20 || !reader.windowStart.Equal(now.Add(-60*time.Minute)) {
		t.Fatalf("reader got window=%v limit=%d", reader.windowStart, reader.limit)
	}
}

func TestHighlightsNullKingAndEmptyTrending(t *testing.T) {
	now := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	s := highlightsServer(&highlightsReaderStub{}, func() time.Time { return now })
	w := getHighlights(t, s)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	for _, want := range []string{`"trending":[]`, `"king_of_the_hill":null`} {
		if !strings.Contains(w.Body.String(), want) {
			t.Fatalf("body missing %s: %s", want, w.Body.String())
		}
	}
}

func TestHighlightsRouteNotShadowed(t *testing.T) {
	reader := &highlightsReaderStub{}
	s := highlightsServer(reader, nil)
	w := getHighlights(t, s)
	if w.Code != http.StatusOK || strings.Contains(w.Body.String(), "invalid_address") || strings.Contains(w.Body.String(), "invalid_token") {
		t.Fatalf("highlights was captured by the address route: status=%d body=%s", w.Code, w.Body.String())
	}
	if reader.callCount() != 1 {
		t.Fatalf("highlights reader calls = %d, want 1", reader.callCount())
	}
	// A genuine malformed address on the sibling route still fails as an address problem.
	bad := httptest.NewRecorder()
	s.Handler.ServeHTTP(bad, httptest.NewRequest(http.MethodGet, "/v1/tokens/not-an-address", nil))
	if bad.Code == http.StatusOK {
		t.Fatalf("sibling address route unexpectedly accepted a malformed address: %s", bad.Body.String())
	}
}

func TestHighlightsReaderErrorMapped(t *testing.T) {
	now := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	reader := &highlightsReaderStub{err: pagination.ErrSnapshotUnavailable}
	s := highlightsServer(reader, func() time.Time { return now })
	w := getHighlights(t, s)
	if w.Code != http.StatusServiceUnavailable || !strings.Contains(w.Body.String(), "urn:launchpad:problem:snapshot_unavailable") {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	// Failed reads are not cached: the next request reads again and can succeed.
	reader.mu.Lock()
	reader.err = nil
	reader.mu.Unlock()
	if w := getHighlights(t, s); w.Code != http.StatusOK {
		t.Fatalf("retry status=%d body=%s", w.Code, w.Body.String())
	}
	if reader.callCount() != 2 {
		t.Fatalf("reader calls = %d, want 2 (failure must not be cached)", reader.callCount())
	}
}

func TestHighlightsUnavailableReader(t *testing.T) {
	s := highlightsServer(nil, nil)
	if w := getHighlights(t, s); w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}
