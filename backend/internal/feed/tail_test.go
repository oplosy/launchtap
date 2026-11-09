package feed

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"math/big"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"

	"github.com/Contictus/launchtap/backend/internal/realtime"
)

type fakeReader struct {
	mu       sync.Mutex
	trades   []Trade
	grads    []Graduation
	launches []Launch
	marks    Watermarks
	err      error
	// tradeErrs makes TradesAfter fail that many times, then succeed.
	tradeErrs int
}

func (f *fakeReader) TradesAfter(_ context.Context, _ int64, after Cursor, limit int) ([]Trade, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	if f.tradeErrs > 0 {
		f.tradeErrs--
		return nil, errors.New("trades boom")
	}
	var out []Trade
	for _, row := range f.trades {
		if cursorLess(after, Cursor{row.Block, row.TxIndex, row.LogIndex}) && len(out) < limit {
			out = append(out, row)
		}
	}
	return out, nil
}

func (f *fakeReader) RecentTrades(context.Context, int64, int) ([]Trade, error) { return nil, nil }

func (f *fakeReader) GraduationsAfter(_ context.Context, _ int64, after Cursor, limit int) ([]Graduation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []Graduation
	for _, row := range f.grads {
		if cursorLess(after, Cursor{row.Block, row.TxIndex, row.LogIndex}) && len(out) < limit {
			out = append(out, row)
		}
	}
	return out, nil
}

func (f *fakeReader) LaunchesAfter(_ context.Context, _ int64, after Cursor, limit int) ([]Launch, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []Launch
	for _, row := range f.launches {
		if cursorLess(after, Cursor{row.Block, row.TxIndex, row.LogIndex}) && len(out) < limit {
			out = append(out, row)
		}
	}
	return out, nil
}

func (f *fakeReader) Newest(context.Context, int64) (Cursor, Cursor, Cursor, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var t, g, l Cursor
	for _, r := range f.trades {
		if c := (Cursor{r.Block, r.TxIndex, r.LogIndex}); cursorLess(t, c) {
			t = c
		}
	}
	for _, r := range f.grads {
		if c := (Cursor{r.Block, r.TxIndex, r.LogIndex}); cursorLess(g, c) {
			g = c
		}
	}
	for _, r := range f.launches {
		if c := (Cursor{r.Block, r.TxIndex, r.LogIndex}); cursorLess(l, c) {
			l = c
		}
	}
	return t, g, l, nil
}

func (f *fakeReader) Watermarks(context.Context, int64, string) (Watermarks, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.marks, nil
}

func (f *fakeReader) update(fn func(*fakeReader)) {
	f.mu.Lock()
	fn(f)
	f.mu.Unlock()
}

type recorder struct {
	mu     sync.Mutex
	events []realtime.Event
	notify chan struct{}
}

func newRecorder() *recorder { return &recorder{notify: make(chan struct{}, 1024)} }

func (r *recorder) Publish(e realtime.Event) {
	r.mu.Lock()
	r.events = append(r.events, e)
	r.mu.Unlock()
	r.notify <- struct{}{}
}

func (r *recorder) snapshot() []realtime.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]realtime.Event(nil), r.events...)
}

func (r *recorder) types() []string {
	var out []string
	for _, e := range r.snapshot() {
		out = append(out, e.Type)
	}
	return out
}

func (r *recorder) waitCount(t *testing.T, n int) {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for len(r.snapshot()) < n {
		select {
		case <-r.notify:
		case <-deadline:
			t.Fatalf("timed out waiting for %d events, got %v", n, r.types())
		}
	}
}

// quiet gives the worker time to process pending hints so absence of events can be asserted.
func quiet() { time.Sleep(80 * time.Millisecond) }

type countObserver struct {
	mu     sync.Mutex
	bursts int
	failed []string
}

func (o *countObserver) ReadFailed(op string) {
	o.mu.Lock()
	o.failed = append(o.failed, op)
	o.mu.Unlock()
}

func (o *countObserver) failures() []string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]string(nil), o.failed...)
}

func (o *countObserver) Published(string)  {}
func (o *countObserver) Lag(time.Duration) {}
func (o *countObserver) Burst()            { o.mu.Lock(); o.bursts++; o.mu.Unlock() }
func (o *countObserver) burstCount() int   { o.mu.Lock(); defer o.mu.Unlock(); return o.bursts }

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *clock) advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

func trade(block int64, tx, log int32, token byte) Trade {
	return Trade{Token: common.BytesToAddress([]byte{token}), Name: "N", Symbol: "S", Source: "curve", Buy: true,
		ETHVolume: big.NewInt(1), TokenVolume: big.NewInt(2), Block: block, TxIndex: tx, LogIndex: log}
}

func startTail(t *testing.T, tail *Tail) (stop func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	tail.init()
	go func() { _ = tail.Start(ctx); close(done) }()
	select {
	case <-tail.ready:
	case <-time.After(2 * time.Second):
		t.Fatal("tail did not become ready")
	}
	return func() {
		cancel()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("Start did not return after cancel")
		}
	}
}

func newTail(r Reader, hub Publisher, clk *clock) *Tail {
	return &Tail{Reader: r, Hub: hub, ChainID: 1, DeploymentID: "d", Now: clk.now, WatermarkEvery: time.Hour}
}

var tokenHint = realtime.Event{Type: "token"}

func TestTailStartsAtNewestAndSkipsHistory(t *testing.T) {
	reader := &fakeReader{trades: []Trade{trade(1, 0, 0, 1), trade(2, 0, 0, 1)}}
	rec := newRecorder()
	tail := newTail(reader, rec, &clock{t: time.Unix(100, 0)})
	stop := startTail(t, tail)
	defer stop()
	tail.Hint(tokenHint)
	quiet()
	if got := rec.snapshot(); len(got) != 0 {
		t.Fatalf("history published: %v", rec.types())
	}
	reader.update(func(f *fakeReader) { f.trades = append(f.trades, trade(3, 0, 0, 1)) })
	tail.Hint(tokenHint)
	rec.waitCount(t, 1)
	if e := rec.snapshot()[0]; e.Type != "trade" || e.Trade.Block != 3 || e.ChainID != 1 || e.DeploymentID != "d" {
		t.Fatalf("unexpected event %+v", e)
	}
}

func TestTailEmptyDatabasePublishesFirstTrade(t *testing.T) {
	reader := &fakeReader{}
	rec := newRecorder()
	tail := newTail(reader, rec, &clock{t: time.Unix(100, 0)})
	stop := startTail(t, tail)
	defer stop()
	reader.update(func(f *fakeReader) { f.trades = append(f.trades, trade(1, 0, 0, 1)) })
	tail.Hint(tokenHint)
	rec.waitCount(t, 1)
}

func TestTailPublishesOldestFirstAndAdvancesCursor(t *testing.T) {
	reader := &fakeReader{}
	rec := newRecorder()
	tail := newTail(reader, rec, &clock{t: time.Unix(100, 0)})
	stop := startTail(t, tail)
	defer stop()
	reader.update(func(f *fakeReader) { f.trades = append(f.trades, trade(1, 0, 0, 1), trade(1, 0, 1, 1)) })
	tail.Hint(tokenHint)
	rec.waitCount(t, 2)
	reader.update(func(f *fakeReader) { f.trades = append(f.trades, trade(2, 0, 0, 1)) })
	tail.Hint(tokenHint)
	tail.Hint(tokenHint)
	rec.waitCount(t, 3)
	quiet()
	got := rec.snapshot()
	if len(got) != 3 || got[0].Trade.LogIndex != 0 || got[1].Trade.LogIndex != 1 || got[2].Trade.Block != 2 {
		t.Fatalf("unexpected sequence: %v", rec.types())
	}
}

func TestTailPublishesNewTokenAndGraduation(t *testing.T) {
	reader := &fakeReader{}
	rec := newRecorder()
	tail := newTail(reader, rec, &clock{t: time.Unix(100, 0)})
	stop := startTail(t, tail)
	defer stop()
	reader.update(func(f *fakeReader) {
		f.launches = append(f.launches, Launch{Token: common.Address{1}, Name: "Alpha", Symbol: "ALP", Block: 1})
		f.grads = append(f.grads, Graduation{Token: common.Address{1}, Name: "Alpha", Symbol: "ALP", Block: 2})
	})
	tail.Hint(realtime.Event{Type: "launch"})
	rec.waitCount(t, 2)
	byType := map[string]realtime.Event{}
	for _, e := range rec.snapshot() {
		byType[e.Type] = e
	}
	if l := byType["new_token"].Launch; l == nil || l.Name != "Alpha" || l.Symbol != "ALP" {
		t.Fatalf("bad new_token %+v", byType["new_token"])
	}
	if g := byType["graduation"].Graduation; g == nil || g.Name != "Alpha" || g.Symbol != "ALP" {
		t.Fatalf("bad graduation %+v", byType["graduation"])
	}
}

func TestTailRewindsOnReorg(t *testing.T) {
	reader := &fakeReader{trades: []Trade{trade(8, 0, 0, 1)}}
	rec := newRecorder()
	tail := newTail(reader, rec, &clock{t: time.Unix(100, 0)})
	stop := startTail(t, tail)
	defer stop()
	tail.Hint(tokenHint)
	quiet()
	reader.update(func(f *fakeReader) { f.trades = []Trade{trade(6, 0, 0, 2)} })
	tail.Hint(realtime.Event{Type: "reorg", CommonAncestor: 5})
	rec.waitCount(t, 1)
	if e := rec.snapshot()[0]; e.Trade.Block != 6 {
		t.Fatalf("unexpected %+v", e)
	}
	// An ancestor above the cursor must not move it forward.
	tail.Hint(realtime.Event{Type: "reorg", CommonAncestor: 100})
	quiet()
	if len(rec.snapshot()) != 1 {
		t.Fatalf("cursor moved forward: %v", rec.types())
	}
}

func TestTailRateLimitEmitsBurst(t *testing.T) {
	reader := &fakeReader{}
	rec := newRecorder()
	obs := &countObserver{}
	clk := &clock{t: time.Unix(100, 0)}
	tail := newTail(reader, rec, clk)
	tail.Observer = obs
	stop := startTail(t, tail)
	defer stop()
	reader.update(func(f *fakeReader) {
		for i := 0; i < 25; i++ {
			f.trades = append(f.trades, trade(1, int32(i), 0, byte(1+i%2)))
		}
	})
	tail.Hint(tokenHint)
	rec.waitCount(t, 20)
	quiet()
	if obs.burstCount() != 0 {
		t.Fatal("burst emitted before window closed")
	}
	clk.advance(1100 * time.Millisecond)
	tail.Hint(tokenHint)
	rec.waitCount(t, 21)
	quiet()
	trades, bursts, total := 0, 0, 0
	for _, e := range rec.snapshot() {
		switch e.Type {
		case "trade":
			trades++
		case "trade_burst":
			bursts++
			for _, b := range e.Burst {
				total += b.Count
			}
		}
	}
	if trades != 20 || bursts != 1 || total != 5 || obs.burstCount() != 1 {
		t.Fatalf("trades=%d bursts=%d total=%d observer=%d", trades, bursts, total, obs.burstCount())
	}
}

func TestTailFinalityOnTickerWithoutHints(t *testing.T) {
	reader := &fakeReader{}
	rec := newRecorder()
	tail := newTail(reader, rec, &clock{t: time.Unix(100, 0)})
	tail.WatermarkEvery = 10 * time.Millisecond
	stop := startTail(t, tail)
	defer stop()
	quiet()
	if len(rec.snapshot()) != 0 {
		t.Fatal("unexpected events for zero watermarks")
	}
	reader.update(func(f *fakeReader) { f.marks = Watermarks{Safe: 10} })
	rec.waitCount(t, 1)
	quiet()
	got := rec.snapshot()
	if len(got) != 1 || got[0].Type != "finality" || got[0].SafeBlock != 10 {
		t.Fatalf("unexpected %v", rec.types())
	}
}

func TestTailReaderErrorKeepsCursor(t *testing.T) {
	reader := &fakeReader{err: errors.New("boom")}
	rec := newRecorder()
	tail := newTail(reader, rec, &clock{t: time.Unix(100, 0)})
	stop := startTail(t, tail)
	defer stop()
	reader.update(func(f *fakeReader) { f.trades = append(f.trades, trade(1, 0, 0, 1)) })
	tail.Hint(tokenHint)
	quiet()
	if len(rec.snapshot()) != 0 {
		t.Fatal("published despite error")
	}
	reader.update(func(f *fakeReader) { f.err = nil })
	tail.Hint(tokenHint)
	rec.waitCount(t, 1)
	quiet()
	if len(rec.snapshot()) != 1 {
		t.Fatalf("duplicate publish: %v", rec.types())
	}
}

func TestTailStartReturnsOnCancel(t *testing.T) {
	tail := newTail(&fakeReader{}, newRecorder(), &clock{t: time.Unix(100, 0)})
	stop := startTail(t, tail)
	stop()
}

func TestTailTradesReadFailureIsObservedAndRetried(t *testing.T) {
	reader := &fakeReader{}
	rec := newRecorder()
	obs := &countObserver{}
	tail := newTail(reader, rec, &clock{t: time.Unix(100, 0)})
	tail.Observer = obs
	tail.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	stop := startTail(t, tail)
	defer stop()
	reader.update(func(f *fakeReader) {
		f.trades = append(f.trades, trade(1, 0, 0, 1))
		f.tradeErrs = 1
	})
	tail.Hint(tokenHint)
	quiet()
	if got := obs.failures(); len(got) != 1 || got[0] != "trades" {
		t.Fatalf("failures = %v", got)
	}
	if len(rec.snapshot()) != 0 {
		t.Fatal("published despite error")
	}
	tail.Hint(tokenHint)
	rec.waitCount(t, 1)
	quiet()
	if len(rec.snapshot()) != 1 || rec.snapshot()[0].Trade.Block != 1 {
		t.Fatalf("unexpected %v", rec.types())
	}
}

func TestTailReadsWatermarksOnHint(t *testing.T) {
	reader := &fakeReader{}
	rec := newRecorder()
	tail := newTail(reader, rec, &clock{t: time.Unix(100, 0)}) // ticker is an hour away
	stop := startTail(t, tail)
	defer stop()
	reader.update(func(f *fakeReader) { f.marks = Watermarks{Safe: 7, Finalized: 3} })
	tail.Hint(tokenHint)
	rec.waitCount(t, 1)
	if e := rec.snapshot()[0]; e.Type != "finality" || e.SafeBlock != 7 || e.FinalizedBlock != 3 {
		t.Fatalf("unexpected %+v", e)
	}
}

func TestTailFinalityPublishedOnlyWhenAdvancing(t *testing.T) {
	reader := &fakeReader{marks: Watermarks{Safe: 10, Finalized: 5}}
	rec := newRecorder()
	tail := newTail(reader, rec, &clock{t: time.Unix(100, 0)})
	stop := startTail(t, tail)
	defer stop()
	tail.Hint(tokenHint)
	rec.waitCount(t, 1)
	reader.update(func(f *fakeReader) { f.marks = Watermarks{Safe: 8, Finalized: 5} })
	tail.Hint(tokenHint)
	quiet()
	if len(rec.snapshot()) != 1 {
		t.Fatalf("lower watermark published: %v", rec.types())
	}
	reader.update(func(f *fakeReader) { f.marks = Watermarks{Safe: 8, Finalized: 6} })
	tail.Hint(tokenHint)
	rec.waitCount(t, 2)
	if e := rec.snapshot()[1]; e.SafeBlock != 10 || e.FinalizedBlock != 6 {
		t.Fatalf("unexpected %+v", e)
	}
}

func TestTailRewindsGraduationAndLaunchCursors(t *testing.T) {
	reader := &fakeReader{
		launches: []Launch{{Token: common.Address{1}, Name: "A", Symbol: "A", Block: 8}},
		grads:    []Graduation{{Token: common.Address{1}, Name: "A", Symbol: "A", Block: 9}},
	}
	rec := newRecorder()
	tail := newTail(reader, rec, &clock{t: time.Unix(100, 0)})
	stop := startTail(t, tail)
	defer stop()
	reader.update(func(f *fakeReader) {
		f.launches = []Launch{{Token: common.Address{2}, Name: "B", Symbol: "B", Block: 6}}
		f.grads = []Graduation{{Token: common.Address{2}, Name: "B", Symbol: "B", Block: 7}}
	})
	tail.Hint(realtime.Event{Type: "reorg", CommonAncestor: 5})
	rec.waitCount(t, 2)
	quiet()
	got := map[string]int64{}
	for _, e := range rec.snapshot() {
		switch e.Type {
		case "new_token":
			got[e.Type] = e.Launch.Block
		case "graduation":
			got[e.Type] = e.Graduation.Block
		}
	}
	if got["new_token"] != 6 || got["graduation"] != 7 || len(rec.snapshot()) != 2 {
		t.Fatalf("unexpected %v %v", got, rec.types())
	}
}

func TestTailBurstClosedByTimerWithoutFurtherReads(t *testing.T) {
	reader := &fakeReader{}
	rec := newRecorder()
	obs := &countObserver{}
	clk := &clock{t: time.Unix(100, 0)}
	tail := newTail(reader, rec, clk)
	tail.Observer = obs
	tail.PerSecond = 2
	stop := startTail(t, tail)
	defer stop()
	reader.update(func(f *fakeReader) {
		for i := 0; i < 4; i++ {
			f.trades = append(f.trades, trade(1, int32(i), 0, 1))
		}
	})
	tail.Hint(tokenHint)
	rec.waitCount(t, 2)
	quiet()
	if obs.burstCount() != 0 {
		t.Fatal("burst emitted before window closed")
	}
	clk.advance(1100 * time.Millisecond) // no further hint: only the window timer can close it
	rec.waitCount(t, 3)
	quiet()
	bursts, total := 0, 0
	for _, e := range rec.snapshot() {
		if e.Type == "trade_burst" {
			bursts++
			for _, b := range e.Burst {
				total += b.Count
			}
		}
	}
	if bursts != 1 || total != 2 || obs.burstCount() != 1 {
		t.Fatalf("bursts=%d total=%d observer=%d types=%v", bursts, total, obs.burstCount(), rec.types())
	}
}

func TestTailSecondStartReturnsErrStarted(t *testing.T) {
	tail := newTail(&fakeReader{}, newRecorder(), &clock{t: time.Unix(100, 0)})
	stop := startTail(t, tail)
	defer stop()
	done := make(chan error, 1)
	go func() { done <- tail.Start(context.Background()) }()
	select {
	case err := <-done:
		if !errors.Is(err, ErrStarted) {
			t.Fatalf("second Start = %v, want ErrStarted", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("second Start did not return")
	}
}

func TestTailReorgHintBeforeStartDoesNotReplayHistory(t *testing.T) {
	reader := &fakeReader{trades: []Trade{trade(6, 0, 0, 1), trade(8, 0, 0, 1)}}
	rec := newRecorder()
	tail := newTail(reader, rec, &clock{t: time.Unix(100, 0)})
	// Recorded before Newest is read: the newest heads already reflect the reorg, nothing was published.
	tail.Hint(realtime.Event{Type: "reorg", CommonAncestor: 5})
	stop := startTail(t, tail)
	defer stop()
	tail.Hint(tokenHint)
	quiet()
	if got := rec.snapshot(); len(got) != 0 {
		t.Fatalf("history replayed: %v", rec.types())
	}
	reader.update(func(f *fakeReader) { f.trades = append(f.trades, trade(9, 0, 0, 1)) })
	tail.Hint(tokenHint)
	rec.waitCount(t, 1)
	quiet()
	if got := rec.snapshot(); len(got) != 1 || got[0].Trade.Block != 9 {
		t.Fatalf("unexpected %v", rec.types())
	}
}

func TestTailReconnectedRewindsToSafeBlock(t *testing.T) {
	reader := &fakeReader{trades: []Trade{trade(8, 0, 0, 1)}, marks: Watermarks{Safe: 5, Finalized: 3}}
	rec := newRecorder()
	tail := newTail(reader, rec, &clock{t: time.Unix(100, 0)})
	stop := startTail(t, tail)
	defer stop()
	tail.Hint(tokenHint)
	rec.waitCount(t, 1) // finality
	// Blocks above 5 were reorged while the hint connection was down; the reorg hint was lost.
	reader.update(func(f *fakeReader) { f.trades = []Trade{trade(4, 0, 0, 1), trade(6, 0, 0, 2)} })
	tail.Hint(tokenHint)
	quiet()
	if len(rec.snapshot()) != 1 {
		t.Fatalf("published without a rewind: %v", rec.types())
	}
	tail.Reconnected()
	rec.waitCount(t, 2)
	quiet()
	got := rec.snapshot()
	if len(got) != 2 || got[1].Type != "trade" || got[1].Trade.Block != 6 {
		t.Fatalf("unexpected %v", rec.types())
	}
}

func TestTailReconnectedWithoutSafeBlockKeepsCursor(t *testing.T) {
	reader := &fakeReader{trades: []Trade{trade(8, 0, 0, 1)}}
	rec := newRecorder()
	tail := newTail(reader, rec, &clock{t: time.Unix(100, 0)})
	stop := startTail(t, tail)
	defer stop()
	tail.Reconnected()
	quiet()
	if len(rec.snapshot()) != 0 {
		t.Fatalf("unknown safe block rewound to genesis: %v", rec.types())
	}
}
