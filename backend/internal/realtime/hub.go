// Package realtime distributes best-effort refresh hints to API clients.
package realtime

import (
	"encoding/json"
	"errors"
	"math/big"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum/common"
)

var ErrCapacity = errors.New("SSE subscriber capacity reached")

type Event struct {
	Type           string `json:"type"`
	ChainID        int64  `json:"chain_id"`
	DeploymentID   string `json:"deployment_id"`
	Token          string `json:"token,omitempty"`
	AsOfBlock      int64  `json:"as_of_block,omitempty"`
	AsOfBlockHash  string `json:"as_of_block_hash,omitempty"`
	CommonAncestor int64  `json:"common_ancestor,omitempty"`

	// Feed events (trade, graduation, new_token, trade_burst, finality) are produced in-process by the
	// feed tail and never decoded from NOTIFY.
	Trade      *Trade       `json:"trade,omitempty"`
	Graduation *Graduation  `json:"graduation,omitempty"`
	Launch     *Launch      `json:"launch,omitempty"`
	Burst      []BurstCount `json:"burst,omitempty"`
	BurstStart time.Time    `json:"burst_start,omitempty"`
	BurstEnd   time.Time    `json:"burst_end,omitempty"`
	// BurstLaunches and BurstGraduations count new_token and graduation events folded into a trade_burst.
	BurstLaunches    int   `json:"burst_launches,omitempty"`
	BurstGraduations int   `json:"burst_graduations,omitempty"`
	SafeBlock        int64 `json:"safe_block,omitempty"`
	FinalizedBlock   int64 `json:"finalized_block,omitempty"`
}

// BurstCount is the number of trades of one token that were rolled into a trade_burst event.
type BurstCount struct {
	Token string `json:"token"`
	Count int    `json:"count"`
}

type Trade struct {
	Token        common.Address
	Name, Symbol string
	Source       string
	Trader       *common.Address
	Buy          bool
	ETHVolume    *big.Int
	TokenVolume  *big.Int
	Block        int64
	TxIndex      int32
	TxHash       common.Hash
	LogIndex     int32
	Time         time.Time
	Finality     string
}

type Graduation struct {
	Token, Pair  common.Address
	Name, Symbol string
	TxHash       common.Hash
	Block        int64
	TxIndex      int32
	LogIndex     int32
}

type Launch struct {
	Token, Creator common.Address
	Name, Symbol   string
	TxHash         common.Hash
	Block          int64
	TxIndex        int32
	LogIndex       int32
}

func Decode(data []byte) (Event, error) {
	var event Event
	if err := json.Unmarshal(data, &event); err != nil {
		return Event{}, err
	}
	if event.Type != "launch" && event.Type != "token" && event.Type != "reorg" {
		return Event{}, errors.New("unknown refresh event type")
	}
	if event.ChainID <= 0 || event.DeploymentID == "" {
		return Event{}, errors.New("refresh event scope is invalid")
	}
	return event, nil
}

type Subscription struct {
	C      <-chan Event
	cancel func()
	once   sync.Once
}

func (s *Subscription) Close() { s.once.Do(s.cancel) }

type Hub struct {
	mu          sync.Mutex
	subscribers map[uint64]chan Event
	nextID      uint64
	maximum     int
	buffer      int
	evictions   atomic.Uint64
	active      atomic.Int64
}

func NewHub(maximum, buffer int) *Hub {
	if maximum <= 0 {
		maximum = 1000
	}
	if buffer <= 0 {
		buffer = 16
	}
	return &Hub{subscribers: make(map[uint64]chan Event), maximum: maximum, buffer: buffer}
}

func (h *Hub) Subscribe() (*Subscription, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.subscribers) >= h.maximum {
		return nil, ErrCapacity
	}
	h.nextID++
	id := h.nextID
	channel := make(chan Event, h.buffer)
	h.subscribers[id] = channel
	h.active.Add(1)
	return &Subscription{C: channel, cancel: func() { h.remove(id) }}, nil
}

func (h *Hub) Publish(event Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for id, channel := range h.subscribers {
		select {
		case channel <- event:
		default:
			delete(h.subscribers, id)
			close(channel)
			h.active.Add(-1)
			h.evictions.Add(1)
		}
	}
}

func (h *Hub) remove(id uint64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	channel, ok := h.subscribers[id]
	if !ok {
		return
	}
	delete(h.subscribers, id)
	close(channel)
	h.active.Add(-1)
}

func (h *Hub) Active() int64     { return h.active.Load() }
func (h *Hub) Evictions() uint64 { return h.evictions.Load() }
