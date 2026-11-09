package feed

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Contictus/launchtap/backend/internal/realtime"
)

// Publisher receives events produced by the tail (realtime.Hub implements it).
type Publisher interface{ Publish(realtime.Event) }

// Observer reports tail activity without importing a metrics library.
type Observer interface {
	Published(eventType string)
	Burst()
	Lag(time.Duration)
	// ReadFailed counts a failed Reader call. op is one of newest, trades, graduations, launches, watermarks.
	ReadFailed(op string)
}

type noopObserver struct{}

func (noopObserver) ReadFailed(string) {}

func (noopObserver) Published(string)  {}
func (noopObserver) Burst()            {}
func (noopObserver) Lag(time.Duration) {}

const burstWindow = time.Second

// ErrStarted is returned by a second Start call: one Tail runs one worker.
var ErrStarted = errors.New("feed tail already started")

// Tail reads new trades, launches and graduations after an in-memory cursor and publishes them.
// One Tail runs per API process. All reads and cursor changes happen in the Start goroutine.
type Tail struct {
	Reader         Reader
	Hub            Publisher
	ChainID        int64
	DeploymentID   string
	Observer       Observer
	Logger         *slog.Logger // nil uses slog.Default()
	Now            func() time.Time
	Limit          int           // rows per read, default 200
	PerSecond      int           // trade events per 1-second window, default 20
	WatermarkEvery time.Duration // finality poll interval, default 5s

	once    sync.Once
	started atomic.Bool
	signal  chan struct{}
	ready   chan struct{} // closed once the starting cursors are read

	mu     sync.Mutex
	rewind *int64 // lowest pending common ancestor

	// worker-owned state
	trade, grad, launch Cursor
	lastSafe, lastFinal int64
	windowStart         time.Time
	windowCount         int
	overflow            map[string]int
}

func (t *Tail) init() {
	t.once.Do(func() {
		t.signal = make(chan struct{}, 1)
		t.ready = make(chan struct{})
	})
}

func (t *Tail) wake() {
	select {
	case t.signal <- struct{}{}:
	default:
	}
}

// Hint is non-blocking: token and launch hints trigger a read, a reorg hint schedules a cursor rewind.
func (t *Tail) Hint(event realtime.Event) {
	t.init()
	if event.Type == "reorg" {
		t.mu.Lock()
		if t.rewind == nil || event.CommonAncestor < *t.rewind {
			ancestor := event.CommonAncestor
			t.rewind = &ancestor
		}
		t.mu.Unlock()
	}
	t.wake()
}

// Start reads the newest positions, then serves hints and the watermark ticker until ctx ends.
// It returns ErrStarted at once when called a second time.
func (t *Tail) Start(ctx context.Context) error {
	if !t.started.CompareAndSwap(false, true) {
		return ErrStarted
	}
	t.init()
	if t.Observer == nil {
		t.Observer = noopObserver{}
	}
	if t.Logger == nil {
		t.Logger = slog.Default()
	}
	if t.Now == nil {
		t.Now = time.Now
	}
	if t.Limit <= 0 {
		t.Limit = 200
	}
	if t.PerSecond <= 0 {
		t.PerSecond = 20
	}
	if t.WatermarkEvery <= 0 {
		t.WatermarkEvery = 5 * time.Second
	}
	ticker := time.NewTicker(t.WatermarkEvery)
	defer ticker.Stop()
	window := time.NewTimer(time.Hour)
	if !window.Stop() {
		<-window.C
	}
	defer window.Stop()

	for {
		trade, grad, launch, err := t.Reader.Newest(ctx, t.ChainID)
		if err == nil {
			t.trade, t.grad, t.launch = trade, grad, launch
			close(t.ready)
			break
		}
		t.readFailed(ctx, "newest", err)
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-t.signal:
			t.drain(ctx)
			t.publishFinality(ctx)
		case <-ticker.C:
			t.drain(ctx)
			t.publishFinality(ctx)
		case <-window.C:
			t.closeWindowIfDue()
		}
		if t.overflow != nil {
			if wait := t.windowStart.Add(burstWindow).Sub(t.Now()); wait > 0 {
				window.Reset(minDuration(wait, burstWindow))
			} else {
				window.Reset(10 * time.Millisecond)
			}
		}
	}
}

// readFailed logs and counts a Reader error. The cursor is untouched, so the next hint or tick retries.
func (t *Tail) readFailed(ctx context.Context, op string, err error) {
	if ctx.Err() != nil {
		return
	}
	t.Logger.Warn("feed tail read failed", "op", op, "chain_id", t.ChainID, "error", err)
	t.Observer.ReadFailed(op)
}

func minDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}

func (t *Tail) drain(ctx context.Context) {
	t.applyRewind()
	t.closeWindowIfDue()
	t.drainLaunches(ctx)
	t.drainTrades(ctx)
	t.drainGraduations(ctx)
	t.closeWindowIfDue()
}

func (t *Tail) applyRewind() {
	t.mu.Lock()
	pending := t.rewind
	t.rewind = nil
	t.mu.Unlock()
	if pending == nil {
		return
	}
	target := Cursor{Block: *pending, TxIndex: math.MaxInt32, LogIndex: math.MaxInt32}
	for _, cursor := range []*Cursor{&t.trade, &t.grad, &t.launch} {
		if cursorLess(target, *cursor) {
			*cursor = target
		}
	}
}

func cursorLess(a, b Cursor) bool {
	if a.Block != b.Block {
		return a.Block < b.Block
	}
	if a.TxIndex != b.TxIndex {
		return a.TxIndex < b.TxIndex
	}
	return a.LogIndex < b.LogIndex
}

func (t *Tail) publish(event realtime.Event) {
	event.ChainID = t.ChainID
	event.DeploymentID = t.DeploymentID
	t.Hub.Publish(event)
	t.Observer.Published(event.Type)
}

// Reader errors keep the cursor; the next hint or tick retries.
func (t *Tail) drainLaunches(ctx context.Context) {
	for ctx.Err() == nil {
		rows, err := t.Reader.LaunchesAfter(ctx, t.ChainID, t.launch, t.Limit)
		if err != nil {
			t.readFailed(ctx, "launches", err)
			return
		}
		for i := range rows {
			row := rows[i]
			t.publish(realtime.Event{Type: "new_token", Token: row.Token.Hex(), Launch: &row})
			t.launch = Cursor{Block: row.Block, TxIndex: row.TxIndex, LogIndex: row.LogIndex}
		}
		if len(rows) < t.Limit {
			return
		}
	}
}

func (t *Tail) drainGraduations(ctx context.Context) {
	for ctx.Err() == nil {
		rows, err := t.Reader.GraduationsAfter(ctx, t.ChainID, t.grad, t.Limit)
		if err != nil {
			t.readFailed(ctx, "graduations", err)
			return
		}
		for i := range rows {
			row := rows[i]
			t.publish(realtime.Event{Type: "graduation", Token: row.Token.Hex(), Graduation: &row})
			t.grad = Cursor{Block: row.Block, TxIndex: row.TxIndex, LogIndex: row.LogIndex}
		}
		if len(rows) < t.Limit {
			return
		}
	}
}

func (t *Tail) drainTrades(ctx context.Context) {
	for ctx.Err() == nil {
		rows, err := t.Reader.TradesAfter(ctx, t.ChainID, t.trade, t.Limit)
		if err != nil {
			t.readFailed(ctx, "trades", err)
			return
		}
		for i := range rows {
			row := rows[i]
			now := t.Now()
			t.closeWindowIfDue()
			if t.windowStart.IsZero() {
				t.windowStart, t.windowCount = now, 0
			}
			if t.windowCount < t.PerSecond {
				t.windowCount++
				t.publish(realtime.Event{Type: "trade", Token: row.Token.Hex(), Trade: &row})
				t.Observer.Lag(now.Sub(row.Time))
			} else {
				if t.overflow == nil {
					t.overflow = make(map[string]int)
				}
				t.overflow[row.Token.Hex()]++
			}
			t.trade = Cursor{Block: row.Block, TxIndex: row.TxIndex, LogIndex: row.LogIndex}
		}
		if len(rows) < t.Limit {
			return
		}
	}
}

// closeWindowIfDue ends the current 1-second window and emits one trade_burst if trades overflowed it.
func (t *Tail) closeWindowIfDue() {
	if t.windowStart.IsZero() || t.Now().Sub(t.windowStart) < burstWindow {
		return
	}
	if len(t.overflow) > 0 {
		counts := make([]realtime.BurstCount, 0, len(t.overflow))
		for token, count := range t.overflow {
			counts = append(counts, realtime.BurstCount{Token: token, Count: count})
		}
		sort.Slice(counts, func(i, j int) bool { return counts[i].Token < counts[j].Token })
		t.publish(realtime.Event{
			Type:       "trade_burst",
			Burst:      counts,
			BurstStart: t.windowStart,
			BurstEnd:   t.windowStart.Add(burstWindow),
		})
		t.Observer.Burst()
	}
	t.overflow = nil
	t.windowStart, t.windowCount = time.Time{}, 0
}

func (t *Tail) publishFinality(ctx context.Context) {
	marks, err := t.Reader.Watermarks(ctx, t.ChainID, t.DeploymentID)
	if err != nil {
		t.readFailed(ctx, "watermarks", err)
		return
	}
	// Publish only when a watermark advances; a lower value after a reorg is not published.
	if marks.Safe <= t.lastSafe && marks.Finalized <= t.lastFinal {
		return
	}
	t.lastSafe, t.lastFinal = max(t.lastSafe, marks.Safe), max(t.lastFinal, marks.Finalized)
	t.publish(realtime.Event{Type: "finality", SafeBlock: t.lastSafe, FinalizedBlock: t.lastFinal})
}
