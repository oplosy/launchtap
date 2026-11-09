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
	PerSecond      int           // trade, new_token and graduation events per 1-second window, default 20
	WatermarkEvery time.Duration // finality poll interval, default 5s

	once    sync.Once
	started atomic.Bool
	signal  chan struct{}
	ready   chan struct{} // closed once the starting cursors are read

	mu     sync.Mutex
	rewind *int64 // lowest pending common ancestor
	resync bool   // rewind to the safe watermark on the next drain

	// worker-owned state
	trade, grad, launch Cursor
	lastSafe, lastFinal int64
	windowStart         time.Time
	windowCount         int
	overflow            map[string]int // folded trades per token
	overflowLaunches    int
	overflowGraduations int
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

// Reconnected schedules a rewind of every cursor to the current safe block and wakes the worker. Call it
// each time the hint connection is listening again after a disconnect: a reorg hint sent while it was
// down is lost, and rows above the safe block may have been replaced. Rows above the safe block are
// published again; clients dedupe them by transaction hash and log index.
func (t *Tail) Reconnected() {
	t.init()
	t.mu.Lock()
	t.resync = true
	t.mu.Unlock()
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
		// A rewind recorded before this read is dropped: nothing has been published yet and the heads read
		// below already reflect it. One recorded while the read runs is kept, so it is applied relative to
		// those heads and replays at most the reorged range.
		t.mu.Lock()
		t.rewind, t.resync = nil, false
		t.mu.Unlock()
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
		if t.overflowPending() {
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
	t.applyRewind(ctx)
	t.closeWindowIfDue()
	t.drainMerged(ctx)
	t.closeWindowIfDue()
}

// feedItem is one row read by the tail, with its chain position.
type feedItem struct {
	at    Cursor
	event realtime.Event
}

// feedStream pages one table after its cursor. buf holds rows read but not yet published; more is true
// while the last page was full, so rows beyond buf may still be unread.
type feedStream struct {
	op      string
	cursor  *Cursor
	read    func(context.Context, Cursor) ([]feedItem, error)
	buf     []feedItem
	fetched bool
	more    bool
}

// drainMerged publishes launches, trades and graduations merged in (block, tx_index, log_index) order.
// An item is published only when every other stream has either a buffered row at or after it or no
// unread rows, so it never overtakes a row of another stream. A read error stops the drain with every
// cursor at its last published row; the next hint or tick retries. Ties (impossible for distinct logs)
// fall back to launch, trade, graduation.
func (t *Tail) drainMerged(ctx context.Context) {
	streams := []*feedStream{
		{op: "launches", cursor: &t.launch, read: t.readLaunches},
		{op: "trades", cursor: &t.trade, read: t.readTrades},
		{op: "graduations", cursor: &t.grad, read: t.readGraduations},
	}
	for ctx.Err() == nil {
		for _, s := range streams {
			if len(s.buf) > 0 || (s.fetched && !s.more) {
				continue
			}
			rows, err := s.read(ctx, *s.cursor)
			if err != nil {
				t.readFailed(ctx, s.op, err)
				return
			}
			s.buf, s.fetched, s.more = rows, true, len(rows) >= t.Limit
		}
		var next *feedStream
		for _, s := range streams {
			if len(s.buf) > 0 && (next == nil || cursorLess(s.buf[0].at, next.buf[0].at)) {
				next = s
			}
		}
		if next == nil {
			return
		}
		item := next.buf[0]
		next.buf = next.buf[1:]
		t.emit(item.event)
		*next.cursor = item.at
	}
}

func (t *Tail) readLaunches(ctx context.Context, after Cursor) ([]feedItem, error) {
	rows, err := t.Reader.LaunchesAfter(ctx, t.ChainID, after, t.Limit)
	if err != nil {
		return nil, err
	}
	items := make([]feedItem, len(rows))
	for i := range rows {
		row := rows[i]
		items[i] = feedItem{Cursor{row.Block, row.TxIndex, row.LogIndex}, realtime.Event{Type: "new_token", Token: row.Token.Hex(), Launch: &row}}
	}
	return items, nil
}

func (t *Tail) readTrades(ctx context.Context, after Cursor) ([]feedItem, error) {
	rows, err := t.Reader.TradesAfter(ctx, t.ChainID, after, t.Limit)
	if err != nil {
		return nil, err
	}
	items := make([]feedItem, len(rows))
	for i := range rows {
		row := rows[i]
		items[i] = feedItem{Cursor{row.Block, row.TxIndex, row.LogIndex}, realtime.Event{Type: "trade", Token: row.Token.Hex(), Trade: &row}}
	}
	return items, nil
}

func (t *Tail) readGraduations(ctx context.Context, after Cursor) ([]feedItem, error) {
	rows, err := t.Reader.GraduationsAfter(ctx, t.ChainID, after, t.Limit)
	if err != nil {
		return nil, err
	}
	items := make([]feedItem, len(rows))
	for i := range rows {
		row := rows[i]
		items[i] = feedItem{Cursor{row.Block, row.TxIndex, row.LogIndex}, realtime.Event{Type: "graduation", Token: row.Token.Hex(), Graduation: &row}}
	}
	return items, nil
}

// emit publishes one feed event. Trades, launches and graduations share one budget per 1-second window;
// events beyond it are folded into the window's trade_burst.
func (t *Tail) emit(event realtime.Event) {
	now := t.Now()
	t.closeWindowIfDue()
	if t.windowStart.IsZero() {
		t.windowStart, t.windowCount = now, 0
	}
	if t.windowCount < t.PerSecond {
		t.windowCount++
		t.publish(event)
		if event.Trade != nil {
			t.Observer.Lag(now.Sub(event.Trade.Time))
		}
		return
	}
	switch event.Type {
	case "trade":
		if t.overflow == nil {
			t.overflow = make(map[string]int)
		}
		t.overflow[event.Trade.Token.Hex()]++
	case "new_token":
		t.overflowLaunches++
	case "graduation":
		t.overflowGraduations++
	}
}

func (t *Tail) overflowPending() bool {
	return len(t.overflow) > 0 || t.overflowLaunches > 0 || t.overflowGraduations > 0
}

func (t *Tail) applyRewind(ctx context.Context) {
	t.mu.Lock()
	pending, resync := t.rewind, t.resync
	t.rewind, t.resync = nil, false
	t.mu.Unlock()
	if resync {
		marks, err := t.Reader.Watermarks(ctx, t.ChainID, t.DeploymentID)
		switch {
		case err != nil:
			// Keep the resync pending; the next hint or tick retries it.
			t.readFailed(ctx, "watermarks", err)
			t.mu.Lock()
			t.resync = true
			t.mu.Unlock()
		case marks.Safe > 0: // an unknown safe block (0) would replay all history
			if pending == nil || marks.Safe < *pending {
				pending = &marks.Safe
			}
		}
	}
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

// closeWindowIfDue ends the current 1-second window and emits one trade_burst if events overflowed it.
func (t *Tail) closeWindowIfDue() {
	if t.windowStart.IsZero() || t.Now().Sub(t.windowStart) < burstWindow {
		return
	}
	if t.overflowPending() {
		counts := make([]realtime.BurstCount, 0, len(t.overflow))
		for token, count := range t.overflow {
			counts = append(counts, realtime.BurstCount{Token: token, Count: count})
		}
		sort.Slice(counts, func(i, j int) bool { return counts[i].Token < counts[j].Token })
		t.publish(realtime.Event{
			Type:             "trade_burst",
			Burst:            counts,
			BurstStart:       t.windowStart,
			BurstEnd:         t.windowStart.Add(burstWindow),
			BurstLaunches:    t.overflowLaunches,
			BurstGraduations: t.overflowGraduations,
		})
		t.Observer.Burst()
	}
	t.overflow, t.overflowLaunches, t.overflowGraduations = nil, 0, 0
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
