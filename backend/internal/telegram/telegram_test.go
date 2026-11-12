package telegram

import (
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/Contictus/launchtap/backend/internal/inbox"
	"github.com/ethereum/go-ethereum/common"
)

func TestNewCodeIsAStartParameterAndHashesToItsDigest(t *testing.T) {
	code, digest, err := NewCode()
	if err != nil {
		t.Fatal(err)
	}
	if len(code) != 43 {
		t.Fatalf("code length = %d, want 43", len(code))
	}
	for _, c := range code {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			t.Fatalf("code %q has %q outside the start-parameter alphabet", code, c)
		}
	}
	got, err := HashCode(code)
	if err != nil || got != digest {
		t.Fatalf("HashCode = %x, %v; want %x", got, err, digest)
	}
	other, _, _ := NewCode()
	if other == code {
		t.Fatal("two codes are equal")
	}
}

func TestHashCodeRejectsMalformedCodes(t *testing.T) {
	for _, code := range []string{"", "short", strings.Repeat("a", 42), strings.Repeat("a", 44), strings.Repeat("a", 42) + "!"} {
		if _, err := HashCode(code); err != ErrInvalidCode {
			t.Fatalf("HashCode(%q) error = %v, want ErrInvalidCode", code, err)
		}
	}
}

func TestSettingsValidate(t *testing.T) {
	s := DefaultSettings()
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	if s.MinTradeWei.Cmp(MinTradeWei) != 0 || !s.Graduations || !s.LargeTrades {
		t.Fatalf("defaults = %+v", s)
	}
	for _, v := range []*big.Int{nil, new(big.Int).Sub(MinTradeWei, big.NewInt(1)), new(big.Int).Add(MaxTradeWei, big.NewInt(1))} {
		s.MinTradeWei = v
		if err := s.Validate(); err != ErrInvalidThreshold {
			t.Fatalf("Validate(%v) = %v, want ErrInvalidThreshold", v, err)
		}
	}
	s.MinTradeWei = new(big.Int).Set(MaxTradeWei)
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestMessageIsPlainTextWithTruncatedNames(t *testing.T) {
	buy := true
	trader := common.HexToAddress("0x00000000000000000000000000000000000000a1")
	token := common.HexToAddress("0x00000000000000000000000000000000000000b2")
	eth, _ := new(big.Int).SetString("1234500000000000000", 10)
	trade := inbox.Event{
		Position: inbox.Position{Block: 9, TxIndex: 1, LogIndex: 2}, Kind: inbox.KindLargeTrade, Token: token,
		Name: strings.Repeat("N", 80) + "<b>", Symbol: "*PONS*", BlockTime: time.Unix(0, 0), ETHWei: eth,
		SideBuy: &buy, Trader: &trader, Source: "curve",
	}
	got := Message(trade, "https://launch.example")
	want := "Large buy on $*PONS* (" + strings.Repeat("N", 47) + "…)\n" +
		"1.2345 ETH on the curve by 0x0000…00a1\n" +
		"https://launch.example/token/" + strings.ToLower(token.Hex())
	if got != want {
		t.Fatalf("message =\n%s\nwant\n%s", got, want)
	}

	grad := inbox.Event{Kind: inbox.KindGraduation, Token: token, Name: "Pons", Symbol: "PONS", ETHWei: big.NewInt(4e18)}
	if got := Message(grad, "https://launch.example"); !strings.HasPrefix(got, "$PONS (Pons) graduated: 4 ETH moved to the pool\n") {
		t.Fatalf("graduation message = %q", got)
	}
	sell := false
	trade.SideBuy, trade.Trader, trade.Source = &sell, nil, "dex"
	if got := Message(trade, "https://launch.example"); !strings.Contains(got, "Large sell") || !strings.Contains(got, "ETH on the pool\n") {
		t.Fatalf("sell message = %q", got)
	}
}

func TestParseCommand(t *testing.T) {
	cases := []struct {
		text string
		want Command
		arg  string
	}{
		{"/start abc", CommandStart, "abc"},
		{"/start@LaunchBot abc", CommandStart, "abc"},
		{"/start", CommandStart, ""},
		{"/stop", CommandStop, ""},
		{"/stop@LaunchBot", CommandStop, ""},
		{"hello", CommandHelp, ""},
		{"/starts x", CommandHelp, ""},
	}
	for _, c := range cases {
		got, arg := ParseCommand(c.text)
		if got != c.want || arg != c.arg {
			t.Fatalf("ParseCommand(%q) = %v %q, want %v %q", c.text, got, arg, c.want, c.arg)
		}
	}
}
