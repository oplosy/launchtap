package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Contictus/launchtap/backend/internal/inbox"
	"github.com/Contictus/launchtap/backend/internal/watchlist"
	"github.com/ethereum/go-ethereum/common"
)

const testToken = "123456:SECRET-token"

func TestClientSendsPlainMessagesAndDecodesErrors(t *testing.T) {
	var got map[string]any
	status, body := http.StatusOK, `{"ok":true,"result":{}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/bot"+testToken+"/sendMessage" || r.Method != http.MethodPost {
			t.Errorf("request %s %s", r.Method, r.URL.Path)
		}
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &got)
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	defer srv.Close()
	c := Client{BaseURL: srv.URL, Token: testToken, HTTP: srv.Client()}

	if err := c.SendMessage(context.Background(), 42, "hi <b>"); err != nil {
		t.Fatal(err)
	}
	if got["chat_id"] != float64(42) || got["text"] != "hi <b>" || got["parse_mode"] != nil {
		t.Fatalf("payload = %v", got)
	}
	if opts, _ := got["link_preview_options"].(map[string]any); opts["is_disabled"] != true {
		t.Fatalf("link previews not disabled: %v", got)
	}

	status, body = http.StatusTooManyRequests, `{"ok":false,"error_code":429,"description":"Too Many Requests","parameters":{"retry_after":7}}`
	err := c.SendMessage(context.Background(), 42, "x")
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Code != 429 || apiErr.RetryAfter != 7*time.Second {
		t.Fatalf("error = %v", err)
	}
	if strings.Contains(err.Error(), "SECRET") {
		t.Fatalf("error leaks the token: %v", err)
	}
}

func TestClientTransportErrorsDoNotLeakTheToken(t *testing.T) {
	c := Client{BaseURL: "http://127.0.0.1:1", Token: testToken, HTTP: &http.Client{Timeout: time.Second}}
	err := c.SendMessage(context.Background(), 1, "x")
	if err == nil || strings.Contains(err.Error(), "SECRET") {
		t.Fatalf("error = %v", err)
	}
}

func TestClientGetUpdatesKeepsPrivateTextMessages(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(raw), `"offset":5`) || !strings.Contains(string(raw), `"allowed_updates":["message"]`) {
			t.Errorf("body = %s", raw)
		}
		_, _ = io.WriteString(w, `{"ok":true,"result":[
			{"update_id":5,"message":{"chat":{"id":7,"type":"private"},"text":"/start abc"}},
			{"update_id":6,"message":{"chat":{"id":-9,"type":"group"},"text":"/stop"}},
			{"update_id":7}]}`)
	}))
	defer srv.Close()
	c := Client{BaseURL: srv.URL, Token: testToken, HTTP: srv.Client()}
	updates, err := c.GetUpdates(context.Background(), 5, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if len(updates) != 3 || updates[0].Message == nil || updates[0].Message.ChatID != 7 || updates[0].Message.ChatType != "private" ||
		updates[0].Message.Text != "/start abc" || updates[1].Message.ChatType != "group" || updates[2].Message != nil || updates[2].UpdateID != 7 {
		t.Fatalf("updates = %+v", updates)
	}
}

type sent struct {
	chat int64
	text string
}

type fakeBot struct {
	sent []sent
	errs map[int64]error
	// onSend runs before each successful send is recorded (to interleave a revocation).
	onSend func(chatID int64)
}

func (b *fakeBot) SendMessage(_ context.Context, chatID int64, text string) error {
	if b.onSend != nil {
		b.onSend(chatID)
	}
	if err := b.errs[chatID]; err != nil {
		return err
	}
	b.sent = append(b.sent, sent{chatID, text})
	return nil
}

type fakeStore struct {
	codes    map[[32]byte]int64 // digest -> bound chat after claim (0 = unclaimed)
	links    []Link
	pending  map[int64][]inbox.Event
	advanced map[int64]inbox.Position
	unlinked []int64
}

func (s *fakeStore) ClaimCode(_ context.Context, _ int64, _ string, d [32]byte, chat int64) (bool, error) {
	bound, ok := s.codes[d]
	if !ok || bound != 0 {
		return false, nil
	}
	s.codes[d] = chat
	return true, nil
}

func (s *fakeStore) UnlinkChat(_ context.Context, _ int64, _ string, chat int64) (bool, error) {
	s.unlinked = append(s.unlinked, chat)
	for i, l := range s.links {
		if l.ChatID == chat {
			s.links = append(s.links[:i], s.links[i+1:]...)
			return true, nil
		}
	}
	return false, nil
}

func (s *fakeStore) Links(context.Context, int64, string) ([]Link, error) {
	return append([]Link(nil), s.links...), nil
}

func (s *fakeStore) Pending(_ context.Context, l Link, limit int) ([]inbox.Event, error) {
	events := s.pending[l.ChatID]
	if len(events) > limit {
		events = events[:limit]
	}
	return events, nil
}

func (s *fakeStore) Advance(_ context.Context, l Link, p inbox.Position) (bool, error) {
	for _, current := range s.links {
		if current.ChatID == l.ChatID && current.Owner == l.Owner {
			s.advanced[l.ChatID] = p
			return true, nil
		}
	}
	return false, nil
}

func event(block int64) inbox.Event {
	return inbox.Event{
		Position: inbox.Position{Block: block}, Kind: inbox.KindGraduation,
		Token: common.HexToAddress("0x10"), Name: "Pons", Symbol: "PONS", ETHWei: big.NewInt(1e18),
	}
}

func newWorker(store *fakeStore, bot *fakeBot) (*Worker, *[]time.Duration) {
	var slept []time.Duration
	return &Worker{
		Store: store, Bot: bot, ChainID: 1, DeploymentID: "test-1", WebOrigin: "https://launch.example",
		Sleep: func(_ context.Context, d time.Duration) error { slept = append(slept, d); return nil },
	}, &slept
}

func TestDeliverSendsInOrderAndAdvancesAfterEachSend(t *testing.T) {
	owner := watchlist.Owner{ChainID: 1, DeploymentID: "test-1", PrivyDID: "did:a"}
	store := &fakeStore{
		links:    []Link{{Owner: owner, ChatID: 7, Settings: DefaultSettings()}, {Owner: owner, ChatID: 8}, {Owner: owner, ChatID: 9}},
		pending:  map[int64][]inbox.Event{7: {event(3), event(4)}, 8: {event(5), event(6)}, 9: {event(7)}},
		advanced: map[int64]inbox.Position{},
	}
	bot := &fakeBot{errs: map[int64]error{
		8: &APIError{Code: 403, Description: "Forbidden: bot was blocked by the user"},
	}}
	w, _ := newWorker(store, bot)
	if err := w.DeliverOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(bot.sent) != 3 || bot.sent[0].chat != 7 || bot.sent[1].chat != 7 || bot.sent[2].chat != 9 {
		t.Fatalf("sent = %+v", bot.sent)
	}
	if store.advanced[7].Block != 4 || store.advanced[9].Block != 7 {
		t.Fatalf("advanced = %+v", store.advanced)
	}
	if _, ok := store.advanced[8]; ok || len(store.unlinked) != 1 || store.unlinked[0] != 8 {
		t.Fatalf("blocked chat: advanced=%v unlinked=%v", store.advanced, store.unlinked)
	}
}

// A /stop or unlink that lands while a batch is being sent ends that link's batch: no alert after
// the one already in flight reaches the revoked chat.
func TestDeliverStopsBatchWhenLinkIsRevokedMidPass(t *testing.T) {
	owner := watchlist.Owner{ChainID: 1, DeploymentID: "test-1", PrivyDID: "did:a"}
	store := &fakeStore{
		links:    []Link{{Owner: owner, ChatID: 7}, {Owner: owner, ChatID: 9}},
		pending:  map[int64][]inbox.Event{7: {event(3), event(4), event(5)}, 9: {event(6)}},
		advanced: map[int64]inbox.Position{},
	}
	bot := &fakeBot{}
	bot.onSend = func(chatID int64) {
		if chatID == 7 && len(bot.sent) == 0 {
			_, _ = store.UnlinkChat(context.Background(), 1, "test-1", 7)
		}
	}
	w, _ := newWorker(store, bot)
	if err := w.DeliverOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(bot.sent) != 2 || bot.sent[0].chat != 7 || bot.sent[1].chat != 9 {
		t.Fatalf("sent = %+v, want one in-flight send to 7 then 9", bot.sent)
	}
}

func TestDeliverStopsThePassOnRateLimitAndKeepsTheCursorOnOtherErrors(t *testing.T) {
	owner := watchlist.Owner{ChainID: 1, DeploymentID: "test-1", PrivyDID: "did:a"}
	store := &fakeStore{
		links:    []Link{{Owner: owner, ChatID: 7}, {Owner: owner, ChatID: 8}, {Owner: owner, ChatID: 9}},
		pending:  map[int64][]inbox.Event{7: {event(3)}, 8: {event(4)}, 9: {event(5)}},
		advanced: map[int64]inbox.Position{},
	}
	bot := &fakeBot{errs: map[int64]error{
		7: errors.New("connection reset"),
		8: &APIError{Code: 429, RetryAfter: 3 * time.Second},
	}}
	w, slept := newWorker(store, bot)
	if err := w.DeliverOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(bot.sent) != 0 || len(store.advanced) != 0 || len(store.unlinked) != 0 {
		t.Fatalf("sent=%v advanced=%v unlinked=%v", bot.sent, store.advanced, store.unlinked)
	}
	if len(*slept) == 0 || (*slept)[len(*slept)-1] != 3*time.Second {
		t.Fatalf("slept = %v", *slept)
	}
}

func TestHandleUpdateLinksUnlinksAndIgnoresGroups(t *testing.T) {
	code, digest, _ := NewCode()
	store := &fakeStore{codes: map[[32]byte]int64{digest: 0}, advanced: map[int64]inbox.Position{}}
	bot := &fakeBot{}
	w, _ := newWorker(store, bot)
	ctx := context.Background()
	msg := func(chat int64, typ, text string) Update {
		return Update{Message: &IncomingMessage{ChatID: chat, ChatType: typ, Text: text}}
	}

	w.HandleUpdate(ctx, msg(-5, "group", "/start "+code))
	if len(bot.sent) != 0 || store.codes[digest] != 0 {
		t.Fatal("a group chat was handled")
	}
	w.HandleUpdate(ctx, msg(7, "private", "/start "+code))
	if store.codes[digest] != 7 || !strings.HasPrefix(bot.sent[0].text, "Connected.") {
		t.Fatalf("claim: codes=%v sent=%v", store.codes, bot.sent)
	}
	w.HandleUpdate(ctx, msg(7, "private", "/start "+code))
	w.HandleUpdate(ctx, msg(7, "private", "/start not-a-code"))
	if !strings.Contains(bot.sent[1].text, "invalid or expired") || !strings.Contains(bot.sent[2].text, "invalid or expired") {
		t.Fatalf("reuse replies = %v", bot.sent)
	}
	w.HandleUpdate(ctx, msg(7, "private", "/stop"))
	if len(store.unlinked) != 1 || !strings.HasPrefix(bot.sent[3].text, "This chat is not connected") {
		t.Fatalf("stop: unlinked=%v sent=%v", store.unlinked, bot.sent)
	}
	w.HandleUpdate(ctx, msg(7, "private", "hello"))
	if !strings.Contains(bot.sent[4].text, "https://launch.example/notifications") {
		t.Fatalf("help = %q", bot.sent[4].text)
	}
	w.HandleUpdate(ctx, Update{UpdateID: 1})
	if len(bot.sent) != 5 {
		t.Fatal("an update without a message was answered")
	}
}

// A 400 for a chat that no longer exists unlinks it; any other 400 refuses that one message, so the
// cursor moves past it instead of retrying it on every pass.
func TestDeliverHandlesBadRequestWithoutRetryingForever(t *testing.T) {
	owner := watchlist.Owner{ChainID: 1, DeploymentID: "test-1", PrivyDID: "did:a"}
	store := &fakeStore{
		links:    []Link{{Owner: owner, ChatID: 7}, {Owner: owner, ChatID: 8}},
		pending:  map[int64][]inbox.Event{7: {event(3), event(4)}, 8: {event(5)}},
		advanced: map[int64]inbox.Position{},
	}
	bot := &fakeBot{errs: map[int64]error{
		7: &APIError{Code: 400, Description: "Bad Request: message text is empty"},
		8: &APIError{Code: 400, Description: "Bad Request: chat not found"},
	}}
	w, _ := newWorker(store, bot)
	if err := w.DeliverOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if store.advanced[7].Block != 4 {
		t.Fatalf("refused messages were not skipped: advanced=%v", store.advanced)
	}
	if _, ok := store.advanced[8]; ok || len(store.unlinked) != 1 || store.unlinked[0] != 8 {
		t.Fatalf("missing chat: advanced=%v unlinked=%v", store.advanced, store.unlinked)
	}
}
