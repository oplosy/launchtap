package main

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"math"
	"math/big"
	"math/rand/v2"
	"slices"
	"time"
)

// Synthetic engine-1 launch parameters. They only need to be internally consistent: a curve
// that graduates at graduationETH never sells more than curveTokens, and the pool opens near the
// curve's last price.
var (
	wad            = big.NewInt(1_000_000_000_000_000_000)
	totalSupply    = new(big.Int).Mul(big.NewInt(1_000_000_000), wad)
	curveTokens    = new(big.Int).Mul(big.NewInt(800_000_000), wad)
	lpTokens       = new(big.Int).Mul(big.NewInt(200_000_000), wad)
	virtualETH     = new(big.Int).Set(wad)
	virtualToken   = new(big.Int).Set(totalSupply)
	graduationETH  = new(big.Int).Mul(big.NewInt(4), wad)
	launchFee      = big.NewInt(1_000_000_000_000_000)
	curveConstant  = new(big.Int).Mul(virtualETH, virtualToken)
	minBuyWei      = 5e14 // 0.0005 ETH
	maxBuyWei      = 1e17 // 0.1 ETH
	zeroAddress    address
	routerAddress  = derivedAddress("router", 0)
	treasuryWallet = derivedAddress("treasury", 0)
	wethAddress    = derivedAddress("weth", 0)
)

const (
	tradeFeeBPS      = 100
	protocolShareBPS = 5000
	launchShare      = 0.9 // launches happen in the first 90 % of the span
	sellProbability  = 0.4
)

type address [20]byte
type hash [32]byte

func derivedAddress(kind string, n int) address {
	sum := sha256.Sum256(fmt.Appendf(nil, "volumeseed/%s/%d", kind, n))
	return address(sum[:20])
}

func derivedHash(kind string, n int64) hash {
	return sha256.Sum256(fmt.Appendf(nil, "volumeseed/%s/%d", kind, n))
}

// Params sizes the synthetic history. Every value is reproducible from Seed.
type Params struct {
	ChainID   int64
	Tokens    int
	Wallets   int
	Positions int
	Seed      uint64
	End       time.Time
	Span      time.Duration
}

func (p Params) validate() error {
	switch {
	case p.ChainID <= 0:
		return fmt.Errorf("chain id must be positive")
	case p.Tokens < 1 || p.Wallets < 1 || p.Positions < 1:
		return fmt.Errorf("tokens, wallets and positions must be positive")
	case p.Span <= 0:
		return fmt.Errorf("span must be positive")
	}
	return nil
}

type coords struct {
	BlockNumber int64
	BlockHash   hash
	BlockTime   time.Time
	TxHash      hash
}

type block struct {
	Number     int64
	Hash       hash
	ParentHash hash
	Time       time.Time
}

type launchRow struct {
	coords
	Token, Curve, Creator, Pair address
	Name, Symbol                string
}

type tradeRow struct {
	coords
	LogIndex                       int32
	Token, Trader                  address
	IsBuy                          bool
	ETHGross, TokenAmount          *big.Int
	ProtocolFee, CreatorFee        *big.Int
	NewETHReserve, NewTokenReserve *big.Int
}

type transferRow struct {
	coords
	LogIndex        int32
	Token, From, To address
	Value           *big.Int
}

type graduationRow struct {
	coords
	Token, Pair                            address
	ETHToPool, TokensToPool, LiquidityBurn *big.Int
}

type poolSyncRow struct {
	coords
	LogIndex           int32
	Pair               address
	Reserve0, Reserve1 *big.Int
}

type poolMintRow struct {
	coords
	LogIndex         int32
	Pair             address
	Amount0, Amount1 *big.Int
}

type poolSwapRow struct {
	coords
	LogIndex                                     int32
	Pair, Trader                                 address
	Amount0In, Amount1In, Amount0Out, Amount1Out *big.Int
}

// sink receives the generated ledger in block order.
type sink interface {
	block(block) error
	launch(launchRow) error
	trade(tradeRow) error
	transfer(transferRow) error
	graduation(graduationRow) error
	poolSync(poolSyncRow) error
	poolMint(poolMintRow) error
	poolSwap(poolSwapRow) error
}

type eventKind uint8

const (
	launchEvent eventKind = iota
	buyEvent
	sellEvent
)

type event struct {
	key      float64
	kind     eventKind
	token    int
	position int
}

type position struct {
	token, wallet int
	buyETH        *big.Int
	sellFraction  float64 // 0 when the position never sells
	bought        *big.Int
}

type tokenState struct {
	token, curve, pair address
	tokenIsToken0      bool
	graduated          bool
	ethReserve         *big.Int // curve: virtual reserve; pool: real reserve
	tokenReserve       *big.Int
}

// Summary describes what generate wrote.
type Summary struct {
	Blocks, Launches, CurveTrades, PoolSwaps, Graduations, Transfers int64
	// HottestToken has the most trades; it graduated when any token did.
	HottestToken address
	// QuietCurveToken is a curve-phase token with at least one trade.
	QuietCurveToken address
	// BusiestWallet has the most positions.
	BusiestWallet address
	LastBlock     block
}

// generate writes a reproducible synthetic history to out: Tokens launches, Positions buys of
// which ~40 % are later partly or fully sold, token and wallet popularity skewed so a few tokens
// and wallets carry most of the activity, and tokens that cross graduationETH moving to a pair.
func generate(p Params, out sink) (Summary, error) {
	if err := p.validate(); err != nil {
		return Summary{}, err
	}
	random := rand.New(rand.NewPCG(p.Seed, p.Seed^0x9e3779b97f4a7c15))
	launchKeys := make([]float64, p.Tokens)
	events := make([]event, 0, p.Tokens+p.Positions*2)
	for i := range launchKeys {
		launchKeys[i] = random.Float64() * launchShare
		events = append(events, event{key: launchKeys[i], kind: launchEvent, token: i})
	}
	positions := make([]position, p.Positions)
	tokenTrades := make([]int, p.Tokens)
	walletPositions := make([]int, p.Wallets)
	for i := range positions {
		token := min(int(float64(p.Tokens)*math.Pow(random.Float64(), 3)), p.Tokens-1)
		wallet := min(int(float64(p.Wallets)*math.Pow(random.Float64(), 2)), p.Wallets-1)
		buyWei := math.Exp(math.Log(minBuyWei) + random.Float64()*(math.Log(maxBuyWei)-math.Log(minBuyWei)))
		buyKey := launchKeys[token] + (1-launchKeys[token])*random.Float64()
		pos := position{token: token, wallet: wallet, buyETH: big.NewInt(int64(buyWei))}
		events = append(events, event{key: buyKey, kind: buyEvent, token: token, position: i})
		tokenTrades[token]++
		if random.Float64() < sellProbability {
			pos.sellFraction = 0.3 + 0.7*random.Float64()
			sellKey := buyKey + (1-buyKey)*random.Float64()
			events = append(events, event{key: sellKey, kind: sellEvent, token: token, position: i})
			tokenTrades[token]++
		}
		positions[i] = pos
		walletPositions[wallet]++
	}
	slices.SortStableFunc(events, func(a, b event) int {
		if a.key != b.key {
			if a.key < b.key {
				return -1
			}
			return 1
		}
		return int(a.kind) - int(b.kind)
	})

	tokens := make([]tokenState, p.Tokens)
	g := generator{p: p, out: out, start: p.End.Add(-p.Span)}
	for _, ev := range events {
		var err error
		switch ev.kind {
		case launchEvent:
			tokens[ev.token], err = g.launch(ev, random)
		case buyEvent:
			err = g.buy(ev, &tokens[ev.token], &positions[ev.position])
		case sellEvent:
			err = g.sell(ev, &tokens[ev.token], &positions[ev.position])
		}
		if err != nil {
			return Summary{}, err
		}
	}

	g.summary.HottestToken = tokens[argMax(tokenTrades)].token
	for i := len(tokens) - 1; i >= 0; i-- {
		if !tokens[i].graduated && tokenTrades[i] > 0 {
			g.summary.QuietCurveToken = tokens[i].token
			break
		}
	}
	g.summary.BusiestWallet = derivedAddress("wallet", argMax(walletPositions))
	return g.summary, nil
}

func argMax(values []int) int {
	best := 0
	for i, v := range values {
		if v > values[best] {
			best = i
		}
	}
	return best
}

type generator struct {
	p       Params
	out     sink
	start   time.Time
	summary Summary
}

// nextBlock emits the next block at the event's position in the span; one transaction per block.
func (g *generator) nextBlock(key float64) (coords, error) {
	n := g.summary.LastBlock.Number + 1
	at := g.start.Add(time.Duration(key * float64(g.p.Span))).Truncate(time.Second)
	if at.Before(g.summary.LastBlock.Time) {
		at = g.summary.LastBlock.Time
	}
	b := block{Number: n, Hash: derivedHash("block", n), ParentHash: derivedHash("block", n-1), Time: at}
	if err := g.out.block(b); err != nil {
		return coords{}, err
	}
	g.summary.LastBlock = b
	g.summary.Blocks++
	return coords{BlockNumber: n, BlockHash: b.Hash, BlockTime: at, TxHash: derivedHash("tx", n)}, nil
}

func (g *generator) launch(ev event, random *rand.Rand) (tokenState, error) {
	c, err := g.nextBlock(ev.key)
	if err != nil {
		return tokenState{}, err
	}
	state := tokenState{
		token: derivedAddress("token", ev.token), curve: derivedAddress("curve", ev.token), pair: derivedAddress("pair", ev.token),
		ethReserve: new(big.Int).Set(virtualETH), tokenReserve: new(big.Int).Set(virtualToken),
	}
	state.tokenIsToken0 = bytes.Compare(state.token[:], wethAddress[:]) < 0
	creator := derivedAddress("wallet", random.IntN(g.p.Wallets))
	if err := g.out.transfer(transferRow{coords: c, LogIndex: 0, Token: state.token, From: zeroAddress, To: state.curve, Value: new(big.Int).Set(totalSupply)}); err != nil {
		return tokenState{}, err
	}
	g.summary.Transfers++
	name := fmt.Sprintf("Volume %d", ev.token+1)
	if err := g.out.launch(launchRow{coords: c, Token: state.token, Curve: state.curve, Creator: creator, Pair: state.pair, Name: name, Symbol: fmt.Sprintf("V%d", ev.token+1)}); err != nil {
		return tokenState{}, err
	}
	g.summary.Launches++
	return state, nil
}

func fees(gross *big.Int) (protocol, creator *big.Int) {
	fee := new(big.Int).Quo(new(big.Int).Mul(gross, big.NewInt(tradeFeeBPS)), big.NewInt(10_000))
	protocol = new(big.Int).Quo(new(big.Int).Mul(fee, big.NewInt(protocolShareBPS)), big.NewInt(10_000))
	return protocol, new(big.Int).Sub(fee, protocol)
}

func ceilDiv(a, b *big.Int) *big.Int {
	q, r := new(big.Int).QuoRem(a, b, new(big.Int))
	if r.Sign() > 0 {
		q.Add(q, big.NewInt(1))
	}
	return q
}

func (g *generator) buy(ev event, state *tokenState, pos *position) error {
	c, err := g.nextBlock(ev.key)
	if err != nil {
		return err
	}
	trader := derivedAddress("wallet", pos.wallet)
	if state.graduated {
		pos.bought, err = g.poolSwap(c, state, trader, pos.buyETH, nil)
		return err
	}
	protocol, creator := fees(pos.buyETH)
	net := new(big.Int).Sub(pos.buyETH, protocol)
	net.Sub(net, creator)
	newETH := new(big.Int).Add(state.ethReserve, net)
	newToken := ceilDiv(curveConstant, newETH)
	pos.bought = new(big.Int).Sub(state.tokenReserve, newToken)
	state.ethReserve, state.tokenReserve = newETH, newToken
	if err := g.curveTrade(c, state, trader, true, pos.buyETH, pos.bought, protocol, creator); err != nil {
		return err
	}
	if state.ethReserve.Cmp(graduationETH) >= 0 {
		return g.graduate(ev, state)
	}
	return nil
}

func (g *generator) sell(ev event, state *tokenState, pos *position) error {
	amount := new(big.Int).Quo(new(big.Int).Mul(pos.bought, big.NewInt(int64(pos.sellFraction*1e6))), big.NewInt(1e6))
	if amount.Sign() == 0 {
		return nil
	}
	c, err := g.nextBlock(ev.key)
	if err != nil {
		return err
	}
	trader := derivedAddress("wallet", pos.wallet)
	if state.graduated {
		_, err = g.poolSwap(c, state, trader, nil, amount)
		return err
	}
	newToken := new(big.Int).Add(state.tokenReserve, amount)
	newETH := ceilDiv(curveConstant, newToken)
	gross := new(big.Int).Sub(state.ethReserve, newETH)
	protocol, creator := fees(gross)
	state.ethReserve, state.tokenReserve = newETH, newToken
	return g.curveTrade(c, state, trader, false, gross, amount, protocol, creator)
}

func (g *generator) curveTrade(c coords, state *tokenState, trader address, isBuy bool, gross, tokens, protocol, creator *big.Int) error {
	from, to := state.curve, trader
	if !isBuy {
		from, to = trader, state.curve
	}
	if err := g.out.transfer(transferRow{coords: c, LogIndex: 0, Token: state.token, From: from, To: to, Value: tokens}); err != nil {
		return err
	}
	g.summary.Transfers++
	g.summary.CurveTrades++
	return g.out.trade(tradeRow{
		coords: c, LogIndex: 1, Token: state.token, Trader: trader, IsBuy: isBuy, ETHGross: gross, TokenAmount: tokens,
		ProtocolFee: protocol, CreatorFee: creator,
		NewETHReserve: new(big.Int).Set(state.ethReserve), NewTokenReserve: new(big.Int).Set(state.tokenReserve),
	})
}

// graduate moves the curve's collected ETH and the LP allocation into the pair in its own block.
func (g *generator) graduate(ev event, state *tokenState) error {
	c, err := g.nextBlock(ev.key)
	if err != nil {
		return err
	}
	ethToPool := new(big.Int).Sub(state.ethReserve, virtualETH)
	tokensToPool := new(big.Int).Set(lpTokens)
	liquidity := new(big.Int).Sqrt(new(big.Int).Mul(ethToPool, tokensToPool))
	if err := g.out.transfer(transferRow{coords: c, LogIndex: 0, Token: state.token, From: state.curve, To: state.pair, Value: tokensToPool}); err != nil {
		return err
	}
	g.summary.Transfers++
	state.graduated = true
	state.ethReserve, state.tokenReserve = ethToPool, tokensToPool
	reserve0, reserve1 := state.ordered(state.tokenReserve, state.ethReserve)
	if err := g.out.poolSync(poolSyncRow{coords: c, LogIndex: 1, Pair: state.pair, Reserve0: reserve0, Reserve1: reserve1}); err != nil {
		return err
	}
	if err := g.out.poolMint(poolMintRow{coords: c, LogIndex: 2, Pair: state.pair, Amount0: reserve0, Amount1: reserve1}); err != nil {
		return err
	}
	g.summary.Graduations++
	return g.out.graduation(graduationRow{coords: c, Token: state.token, Pair: state.pair, ETHToPool: ethToPool, TokensToPool: tokensToPool, LiquidityBurn: liquidity})
}

// ordered returns (token, eth) amounts in pair order.
func (s *tokenState) ordered(tokenAmount, ethAmount *big.Int) (*big.Int, *big.Int) {
	if s.tokenIsToken0 {
		return new(big.Int).Set(tokenAmount), new(big.Int).Set(ethAmount)
	}
	return new(big.Int).Set(ethAmount), new(big.Int).Set(tokenAmount)
}

// poolSwap trades against the constant-product pair (no LP fee): ethIn buys tokens, tokensIn
// sells them. It emits the token transfer, the Sync and the Swap and returns the tokens bought.
func (g *generator) poolSwap(c coords, state *tokenState, trader address, ethIn, tokensIn *big.Int) (*big.Int, error) {
	k := new(big.Int).Mul(state.ethReserve, state.tokenReserve)
	zero := new(big.Int)
	var tokenIn, tokenOut, ethOut *big.Int
	from, to := state.pair, trader
	if ethIn != nil {
		newETH := new(big.Int).Add(state.ethReserve, ethIn)
		newToken := ceilDiv(k, newETH)
		tokenOut = new(big.Int).Sub(state.tokenReserve, newToken)
		tokenIn, ethOut = zero, zero
		state.ethReserve, state.tokenReserve = newETH, newToken
	} else {
		newToken := new(big.Int).Add(state.tokenReserve, tokensIn)
		newETH := ceilDiv(k, newToken)
		ethOut = new(big.Int).Sub(state.ethReserve, newETH)
		tokenIn, tokenOut, ethIn = tokensIn, zero, zero
		from, to = trader, state.pair
		state.ethReserve, state.tokenReserve = newETH, newToken
	}
	moved := tokenOut
	if tokenIn.Sign() > 0 {
		moved = tokenIn
	}
	if err := g.out.transfer(transferRow{coords: c, LogIndex: 0, Token: state.token, From: from, To: to, Value: moved}); err != nil {
		return nil, err
	}
	g.summary.Transfers++
	reserve0, reserve1 := state.ordered(state.tokenReserve, state.ethReserve)
	if err := g.out.poolSync(poolSyncRow{coords: c, LogIndex: 1, Pair: state.pair, Reserve0: reserve0, Reserve1: reserve1}); err != nil {
		return nil, err
	}
	in0, in1 := state.ordered(tokenIn, ethIn)
	out0, out1 := state.ordered(tokenOut, ethOut)
	g.summary.PoolSwaps++
	return tokenOut, g.out.poolSwap(poolSwapRow{coords: c, LogIndex: 2, Pair: state.pair, Trader: trader, Amount0In: in0, Amount1In: in1, Amount0Out: out0, Amount1Out: out1})
}
