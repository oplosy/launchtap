//go:build integration

package postgrestest

import (
	"context"
	"math/big"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Contictus/launchtap/backend/internal/inbox"
	storepostgres "github.com/Contictus/launchtap/backend/internal/store/postgres"
	"github.com/Contictus/launchtap/backend/internal/telegram"
	"github.com/Contictus/launchtap/backend/internal/watchlist"
)

type recordingBot struct{ texts []string }

func (b *recordingBot) SendMessage(_ context.Context, _ int64, text string) error {
	b.texts = append(b.texts, text)
	return nil
}

func eventBlocks(events []inbox.Event) []int64 {
	out := make([]int64, len(events))
	for i, e := range events {
		out[i] = e.Position.Block
	}
	return out
}

// docs/specs/2026-11-12-telegram-alerts-design.md acceptance (integration).
func TestTelegramLinkDeliveryAndRelink(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	l := newInboxFixture(t, ctx, time.Now().UTC().Truncate(time.Second).Add(-2*time.Hour))
	for block := int64(11); block <= 15; block++ {
		l.block(block, "safe")
	}
	setSafe := func(n int64) {
		l.exec(`INSERT INTO sync_state (chain_id, deployment_id, observed_number, observed_hash, observed_at, safe_number, safe_hash, safe_at)
			VALUES ($1, $2, 15, $5, now(), $3, $4, now())
			ON CONFLICT (chain_id, deployment_id) DO UPDATE SET safe_number = EXCLUDED.safe_number, safe_hash = EXCLUDED.safe_hash`,
			walletChainID, inboxDeployment, n, l.blockHash(n), l.blockHash(15))
	}
	setSafe(12)
	l.watch(inboxDeployment, "did:me", positionToken, l.blockTime(10))
	l.tradeOn(positionToken, 12, 0, 0x81, 2*halfETH)  // at the cursor block: never sent
	l.tradeOn(positionToken, 13, 0, 0x82, 6*halfETH/5) // 0.6 ETH
	l.tradeOn(positionToken, 14, 0, 0x83, 4*halfETH)   // 2 ETH
	l.tradeOn(positionToken, 15, 0, 0x84, 6*halfETH)   // 3 ETH, above the safe head at first

	store := storepostgres.TelegramStore{Pool: l.f.pool}
	me := watchlist.Owner{ChainID: walletChainID, DeploymentID: inboxDeployment, PrivyDID: "did:me"}
	if status, err := store.Status(ctx, me); err != nil || status.Linked {
		t.Fatalf("status before linking = %+v, %v", status, err)
	}
	if err := store.UpdateSettings(ctx, me, telegram.DefaultSettings()); err != telegram.ErrNotLinked {
		t.Fatalf("settings before linking: %v", err)
	}

	claim := func(o watchlist.Owner, chat int64) bool {
		t.Helper()
		code, digest, err := telegram.NewCode()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.CreateLinkCode(ctx, o, digest); err != nil {
			t.Fatal(err)
		}
		d, _ := telegram.HashCode(code)
		ok, err := store.ClaimCode(ctx, walletChainID, inboxDeployment, d, chat)
		if err != nil {
			t.Fatal(err)
		}
		// A code is single use.
		if again, err := store.ClaimCode(ctx, walletChainID, inboxDeployment, d, chat); err != nil || again {
			t.Fatalf("second claim = %v, %v", again, err)
		}
		return ok
	}

	// An expired code links nothing; a replaced code is dead.
	_, stale, _ := telegram.NewCode()
	if _, err := store.CreateLinkCode(ctx, me, stale); err != nil {
		t.Fatal(err)
	}
	_, fresh, _ := telegram.NewCode()
	if _, err := store.CreateLinkCode(ctx, me, fresh); err != nil {
		t.Fatal(err)
	}
	l.exec(`UPDATE telegram_link_codes SET expires_at = now() - interval '1 second'`)
	for _, d := range [][32]byte{stale, fresh} {
		if ok, err := store.ClaimCode(ctx, walletChainID, inboxDeployment, d, 7); err != nil || ok {
			t.Fatalf("dead code claimed: %v %v", ok, err)
		}
	}

	if !claim(me, 7) {
		t.Fatal("valid code did not link")
	}
	links, err := store.Links(ctx, walletChainID, inboxDeployment)
	if err != nil || len(links) != 1 || links[0].ChatID != 7 || links[0].Cursor.Block != 12 {
		t.Fatalf("links = %+v, %v", links, err)
	}
	link := links[0]
	if events, err := store.Pending(ctx, link, 10); err != nil || len(events) != 0 {
		t.Fatalf("pending at the safe head = %v, %v", eventBlocks(events), err)
	}

	setSafe(14)
	if events, err := store.Pending(ctx, link, 10); err != nil || !slices.Equal(eventBlocks(events), []int64{13, 14}) {
		t.Fatalf("pending = %v, %v", eventBlocks(events), err)
	}
	strict := telegram.Settings{Graduations: true, LargeTrades: true, MinTradeWei: big.NewInt(2 * halfETH)}
	if err := store.UpdateSettings(ctx, me, strict); err != nil {
		t.Fatal(err)
	}
	if status, err := store.Status(ctx, me); err != nil || !status.Linked || status.Settings.MinTradeWei.Cmp(strict.MinTradeWei) != 0 {
		t.Fatalf("status = %+v, %v", status, err)
	}
	link.Settings = strict
	if events, err := store.Pending(ctx, link, 10); err != nil || !slices.Equal(eventBlocks(events), []int64{14}) {
		t.Fatalf("pending over 1 ETH = %v, %v", eventBlocks(events), err)
	}
	link.Settings = telegram.Settings{Graduations: true, MinTradeWei: big.NewInt(halfETH)}
	if events, err := store.Pending(ctx, link, 10); err != nil || len(events) != 0 {
		t.Fatalf("pending with trades off = %v, %v", eventBlocks(events), err)
	}
	if err := store.UpdateSettings(ctx, me, telegram.DefaultSettings()); err != nil {
		t.Fatal(err)
	}

	// The worker sends each safe event once, in order.
	bot := &recordingBot{}
	w := &telegram.Worker{Store: store, Bot: bot, ChainID: walletChainID, DeploymentID: inboxDeployment, WebOrigin: "https://launch.example",
		Sleep: func(context.Context, time.Duration) error { return nil }}
	if err := w.DeliverOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if err := w.DeliverOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if len(bot.texts) != 2 || !strings.HasPrefix(bot.texts[0], "Large buy") || !strings.Contains(bot.texts[0], "0.6 ETH") || !strings.Contains(bot.texts[1], "2 ETH") {
		t.Fatalf("sent = %q", bot.texts)
	}
	setSafe(15)
	if err := w.DeliverOnce(ctx); err != nil || len(bot.texts) != 3 || !strings.Contains(bot.texts[2], "3 ETH") {
		t.Fatalf("after safe head moved: %q, %v", bot.texts, err)
	}
	links, _ = store.Links(ctx, walletChainID, inboxDeployment)
	if links[0].Cursor.Block != 15 {
		t.Fatalf("cursor = %+v", links[0].Cursor)
	}
	// The cursor never moves back.
	if err := store.Advance(ctx, links[0], inbox.Position{Block: 13}); err != nil {
		t.Fatal(err)
	}
	if links, _ = store.Links(ctx, walletChainID, inboxDeployment); links[0].Cursor.Block != 15 {
		t.Fatalf("cursor moved back: %+v", links[0].Cursor)
	}

	// Re-linking moves the account to a new chat, keeps settings and restarts at the safe head;
	// a stale Advance for the old chat is ignored.
	if err := store.UpdateSettings(ctx, me, strict); err != nil {
		t.Fatal(err)
	}
	old := links[0]
	setSafe(14)
	if !claim(me, 99) {
		t.Fatal("re-link failed")
	}
	if err := store.Advance(ctx, old, inbox.Position{Block: 20}); err != nil {
		t.Fatal(err)
	}
	links, _ = store.Links(ctx, walletChainID, inboxDeployment)
	if len(links) != 1 || links[0].ChatID != 99 || links[0].Cursor.Block != 14 || links[0].Settings.MinTradeWei.Cmp(strict.MinTradeWei) != 0 {
		t.Fatalf("after re-link = %+v", links)
	}

	// Another account claiming the same chat takes it over.
	other := watchlist.Owner{ChainID: walletChainID, DeploymentID: inboxDeployment, PrivyDID: "did:other"}
	if !claim(other, 99) {
		t.Fatal("chat move failed")
	}
	links, _ = store.Links(ctx, walletChainID, inboxDeployment)
	if len(links) != 1 || links[0].Owner.PrivyDID != "did:other" {
		t.Fatalf("after chat move = %+v", links)
	}
	if status, _ := store.Status(ctx, me); status.Linked {
		t.Fatal("old account still linked")
	}

	// /stop and the API unlink.
	if ok, err := store.UnlinkChat(ctx, walletChainID, inboxDeployment, 99); err != nil || !ok {
		t.Fatalf("unlink chat = %v, %v", ok, err)
	}
	if ok, _ := store.UnlinkChat(ctx, walletChainID, inboxDeployment, 99); ok {
		t.Fatal("second unlink reported a link")
	}
	if !claim(other, 5) {
		t.Fatal("link after unlink failed")
	}
	_, pending, _ := telegram.NewCode()
	if _, err := store.CreateLinkCode(ctx, other, pending); err != nil {
		t.Fatal(err)
	}
	if err := store.Unlink(ctx, other); err != nil {
		t.Fatal(err)
	}
	if err := store.Unlink(ctx, other); err != nil {
		t.Fatal(err)
	}
	if ok, err := store.ClaimCode(ctx, walletChainID, inboxDeployment, pending, 5); err != nil || ok {
		t.Fatalf("pending code survived unlink: %v %v", ok, err)
	}
	if links, _ = store.Links(ctx, walletChainID, inboxDeployment); len(links) != 0 {
		t.Fatalf("links after unlink = %+v", links)
	}
}
