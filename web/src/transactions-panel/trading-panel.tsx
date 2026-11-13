"use client";

import { useEffect, useState } from "react";
import { formatBaseUnits } from "@/amounts";
import { ApiClient } from "@/api/client";
import type { Abi } from "viem";
import { snipeTaxLine } from "@/anti-snipe";
import { reviewedV4Addresses } from "@/engine";
import { poolMatchesApi } from "@/v4-swap";
import { browserAbis } from "@/contracts/generated";
import { addressExplorerUrl } from "@/wallet/explorer";
import {
  decodeTransactionError,
  minimumOutput,
  parseRuntimeQuantity,
  parseQuoteQuantity,
  reviewedRouterAddress,
  transactionDeadline,
  type ReviewedWriteIntent,
} from "@/transactions";
import { curvePriceImpact } from "@/price-impact";
import { UsdLine } from "@/market/usd-line";
import { Badge, Button, UnavailableState } from "@/components/primitives";
import {
  DEFAULT_TTL,
  QUOTE_DEBOUNCE_MS,
  type Address,
  eth,
  safeAddress,
  currentUnixSeconds,
  TradeSideTabs,
  ReadinessGate,
  useTaxSecondsLeft,
  TransactionProgress,
} from "./shared";
import {
  ReviewActions,
  ReviewNotice,
  TradeAmountFields,
  useReviewedTrade,
  WrongNetworkBanner,
} from "./reviewed-trade";
import { CurveClaimRow, useCurveClaims } from "./curve-claims";
import { V2CreatorPanel } from "./v2-creator-panel";
import { V4SwapPanel } from "./v4-swap-panel";
import { GraduatedSwapPanel } from "./graduated-swap-panel";

export function TradingPanel({
  token,
}: {
  token: {
    address: string;
    curve: string;
    pair: string;
    phase: string;
    name: string;
    symbol: string;
    engineVersion?: number;
    antiSnipeEndsAt?: string | null;
    v2PoolId?: string | null;
    /** Last indexed spot price, wei per whole token; only used for the price-impact estimate. */
    spotPriceWei?: string | null;
  };
}) {
  return (
    <ReadinessGate>
      <TradingPanelReady token={token} />
    </ReadinessGate>
  );
}

function TradingPanelReady({
  token,
}: {
  token: {
    address: string;
    curve: string;
    pair: string;
    phase: string;
    name: string;
    symbol: string;
    engineVersion?: number;
    antiSnipeEndsAt?: string | null;
    v2PoolId?: string | null;
    /** Last indexed spot price, wei per whole token; only used for the price-impact estimate. */
    spotPriceWei?: string | null;
  };
}) {
  const tokenAddress = safeAddress(token.address);
  const curveAddress = safeAddress(token.curve);
  const safeTokenAddress =
    tokenAddress ?? ("0x0000000000000000000000000000000000000000" as Address);
  const safeCurveAddress =
    curveAddress ?? ("0x0000000000000000000000000000000000000000" as Address);
  const graduated = token.phase.toLowerCase() === "graduated";
  // Engine 2 trades the curve through BondingCurveV2 (Plan v2-C Task 3); any other engine
  // value leaves the panel unavailable instead of guessing an ABI.
  const engineVersion = token.engineVersion ?? 1;
  const curveAbi = (engineVersion === 2 ? browserAbis.curveV2 : browserAbis.curve) as Abi;
  const trade = useReviewedTrade(tokenAddress, !graduated);
  const {
    account,
    readiness,
    publicClient,
    walletClient,
    configuration,
    chainId,
    transaction,
    approvalTransaction,
    state,
    approvalHash,
    side,
    input,
    slippage,
    inputUnits,
    slippageBps,
    executionBusy,
    confirming,
    reviewIntent,
    reviewNotice,
  } = trade;
  // fee is protocol + creator; the confirmation lists the two shares separately.
  const [quote, setQuote] = useState<{
    output: bigint;
    fee: bigint;
    protocolFee: bigint;
    source: "backend" | "contract";
  } | null>(null);
  const [quoteError, setQuoteError] = useState<string | null>(null);
  const [taxQuote, setTaxQuote] = useState<{ snipeTax: bigint; taxBps: number } | null>(null);
  // ETH a buy actually spends (input minus a graduation-cap refund), or the tokens a sell spends.
  const [quoteInputUsed, setQuoteInputUsed] = useState<bigint | null>(null);
  const [reviewDetails, setReviewDetails] = useState<{
    side: "buy" | "sell";
    inputUnits: bigint;
    output: bigint;
    fee: bigint;
    protocolFee: bigint;
  } | null>(null);
  const curveExplorer = addressExplorerUrl(configuration, safeCurveAddress);
  const claims = useCurveClaims({
    configuration,
    account,
    readiness,
    chainId,
    publicClient,
    walletClient,
    tokenAddress,
    curveAddress,
    safeCurveAddress,
    curveAbi,
    engineVersion,
    graduated,
  });
  const claimRow = <CurveClaimRow claims={claims} />;
  const reviewedRouter = reviewedRouterAddress(configuration.deployment!, configuration.chainId);
  const reviewedV4 =
    engineVersion === 2
      ? reviewedV4Addresses(configuration.deployment!, configuration.chainId)
      : null;
  // Plan v2-C D5: trade only the pool whose locally computed id equals the indexed one.
  const v4PoolReady = Boolean(
    reviewedV4 &&
    tokenAddress &&
    poolMatchesApi(
      tokenAddress,
      reviewedV4.hook,
      token.v2PoolId ? { pool_id: token.v2PoolId } : null,
    ),
  );
  const reviewedWeth = safeAddress(configuration.deployment?.weth);

  useEffect(() => {
    let cancelled = false;
    if (
      !inputUnits ||
      inputUnits <= 0n ||
      configuration.status !== "ready" ||
      !configuration.apiBaseUrl
    )
      return;
    const client = new ApiClient({ baseUrl: configuration.apiBaseUrl });
    const controller = new AbortController();
    const timer = setTimeout(() => {
      void client
        .getQuote(token.address, { amount: inputUnits.toString(), side }, controller.signal)
        .then((result) => {
          if (!cancelled) {
            if (parseQuoteQuantity(result.input, "quote input") !== inputUnits)
              throw new Error("QuoteChanged");
            const output = parseQuoteQuantity(result.output, "quote output");
            const refund = parseQuoteQuantity(result.refund, "quote refund");
            if (refund > inputUnits) throw new Error("QuoteChanged");
            parseQuoteQuantity(result.next_virtual_eth, "next virtual ETH");
            parseQuoteQuantity(result.next_virtual_token, "next virtual token");
            parseQuoteQuantity(result.reserve_source_block.toString(), "quote source block");
            const protocolFee = parseQuoteQuantity(result.protocol_fee, "protocol fee");
            setQuote({
              output,
              fee: protocolFee + parseQuoteQuantity(result.creator_fee, "creator fee"),
              protocolFee,
              source: "backend",
            });
            setTaxQuote({
              snipeTax: parseQuoteQuantity(result.snipe_tax, "snipe tax"),
              taxBps: result.tax_bps,
            });
            setQuoteInputUsed(side === "buy" ? inputUnits - refund : inputUnits);
            setQuoteError(null);
          }
        })
        .catch(() => {
          if (!cancelled) {
            setQuote(null);
            setTaxQuote(null);
            setQuoteInputUsed(null);
            setQuoteError("Informational quote unavailable. Refresh before trading.");
          }
        });
    }, QUOTE_DEBOUNCE_MS);
    return () => {
      cancelled = true;
      clearTimeout(timer);
      controller.abort();
    };
  }, [configuration.apiBaseUrl, configuration.status, inputUnits, side, token.address]);

  const activeQuote = inputUnits && inputUnits > 0n ? quote : null;
  const taxSecondsLeft = useTaxSecondsLeft(
    engineVersion === 2 && !graduated ? (token.antiSnipeEndsAt ?? null) : null,
    publicClient,
  );
  const taxLine =
    activeQuote && side === "buy" && taxQuote
      ? snipeTaxLine({ ...taxQuote, remainingSeconds: taxSecondsLeft })
      : null;
  const minimum =
    activeQuote && slippageBps !== null ? minimumOutput(activeQuote.output, slippageBps) : null;
  // Shown only for an untaxed quote: the anti-snipe tax is a separate charge, not price movement.
  const priceImpact =
    activeQuote && quoteInputUsed !== null && taxQuote !== null && taxQuote.snipeTax === 0n
      ? curvePriceImpact({
          side,
          input: quoteInputUsed,
          output: activeQuote.output,
          fee: activeQuote.fee,
          spotPriceWad:
            token.spotPriceWei && /^[1-9][0-9]*$/.test(token.spotPriceWei)
              ? BigInt(token.spotPriceWei)
              : null,
        })
      : null;
  const readCurveIntent = async (
    accountAddress: Address,
    quoteSide: "buy" | "sell",
    quantity: bigint,
    toleranceBps: bigint,
    deadline: bigint,
  ) => {
    const intentChainId = configuration.chainId;
    if (intentChainId === null) throw new Error("ChainUnavailable");
    const contractQuote = await publicClient!.readContract({
      address: safeCurveAddress,
      abi: curveAbi,
      functionName: quoteSide === "buy" ? "quoteBuy" : "quoteSell",
      args: [quantity],
    });
    const output =
      quoteSide === "buy"
        ? parseRuntimeQuantity((contractQuote as readonly unknown[])[1], "contract output")
        : parseRuntimeQuantity((contractQuote as readonly unknown[])[0], "contract output");
    const protocolFee = parseRuntimeQuantity(
      (contractQuote as readonly unknown[])[2],
      "protocol fee",
    );
    const fee =
      protocolFee + parseRuntimeQuantity((contractQuote as readonly unknown[])[3], "creator fee");
    // BondingCurveV2.quoteBuy returns the anti-snipe tax as its fifth value; sells are untaxed.
    if (engineVersion === 2 && quoteSide === "buy") {
      const taxBps = await publicClient!.readContract({
        address: safeCurveAddress,
        abi: curveAbi,
        functionName: "currentTaxBps",
      });
      setTaxQuote({
        snipeTax: parseRuntimeQuantity((contractQuote as readonly unknown[])[4], "snipe tax"),
        taxBps: Number(taxBps),
      });
    } else setTaxQuote({ snipeTax: 0n, taxBps: 0 });
    setQuoteInputUsed(
      quoteSide === "buy"
        ? parseRuntimeQuantity((contractQuote as readonly unknown[])[0], "contract input used")
        : quantity,
    );
    const exactMinimum = minimumOutput(output, toleranceBps);
    const args =
      quoteSide === "buy"
        ? ([accountAddress, accountAddress, exactMinimum, deadline] as const)
        : ([quantity, accountAddress, exactMinimum, deadline] as const);
    return {
      intent: {
        account: accountAddress,
        target: safeCurveAddress,
        value: quoteSide === "buy" ? quantity : 0n,
        args,
        deadline,
        minimumOutput: exactMinimum,
        chainId: intentChainId,
        functionName: quoteSide,
      } satisfies ReviewedWriteIntent,
      output,
      fee,
      protocolFee,
    };
  };
  const showContractQuote = (
    quoteSide: "buy" | "sell",
    quantity: bigint,
    fresh: { output: bigint; fee: bigint; protocolFee: bigint },
  ) => {
    setQuote({
      output: fresh.output,
      fee: fresh.fee,
      protocolFee: fresh.protocolFee,
      source: "contract",
    });
    setReviewDetails({
      side: quoteSide,
      inputUnits: quantity,
      output: fresh.output,
      fee: fresh.fee,
      protocolFee: fresh.protocolFee,
    });
  };
  const readPhase = () =>
    publicClient!.readContract({
      address: safeCurveAddress,
      abi: curveAbi,
      functionName: "phase",
    });
  const prepareTradeReview = async () => {
    if (!trade.walletReady || !inputUnits || slippageBps === null || trade.unresolved) return;
    try {
      await trade.assertWalletForReview();
      if (Number(await readPhase()) !== 0) throw new Error("WrongPhase");
      const deadline = transactionDeadline(currentUnixSeconds(), DEFAULT_TTL);
      const fresh = await readCurveIntent(
        account.address as Address,
        side,
        inputUnits,
        slippageBps,
        deadline,
      );
      showContractQuote(side, inputUnits, fresh);
      trade.openReview(fresh.intent);
      setQuoteError(null);
    } catch (cause) {
      trade.closeReview(false);
      setQuoteError(decodeTransactionError(cause).message);
    }
  };
  const execute = async () => {
    if (graduated) return;
    const resumingApproval = side === "sell" && approvalHash !== undefined;
    if (
      !publicClient ||
      !walletClient ||
      !account.address ||
      !inputUnits ||
      slippageBps === null ||
      !reviewIntent ||
      !reviewDetails ||
      !transaction.ready ||
      (reviewDetails.side === "sell" && !approvalTransaction.ready) ||
      (!resumingApproval && trade.unresolved) ||
      chainId !== configuration.chainId ||
      !readiness.selectedAccountVerified
    ) {
      trade.setReviewNotice(
        "Wallet, network, or review changed. Refresh the review before signing.",
      );
      return;
    }
    const reviewed = reviewIntent;
    const reviewedSide = reviewDetails.side;
    const reviewedInput = reviewDetails.inputUnits;
    const requote = async () => {
      const fresh = await readCurveIntent(
        reviewed.account as Address,
        reviewedSide,
        reviewedInput,
        slippageBps,
        reviewed.deadline!,
      );
      return {
        intent: fresh.intent,
        replace: () => showContractQuote(reviewedSide, reviewedInput, fresh),
      };
    };
    await trade.runExecution(async (run) => {
      // A failure before anything is submitted keeps reporting the last known hash.
      run.setFallbackHash(state.hash as Address | undefined);
      // Independent reads go out together; the checks keep their original order.
      const [phase, tokenBalance, nativeBalance] = await Promise.all([
        readPhase(),
        publicClient.readContract({
          address: safeTokenAddress,
          abi: browserAbis.token,
          functionName: "balanceOf",
          args: [reviewed.account as Address],
        }),
        reviewedSide === "buy"
          ? publicClient.getBalance({ address: reviewed.account as Address })
          : Promise.resolve(null),
      ]);
      if (Number(phase) !== 0) throw new Error("WrongPhase");
      if (reviewedSide === "sell" && tokenBalance < reviewedInput)
        throw new Error("ERC20InsufficientBalance");
      if (nativeBalance !== null && nativeBalance < reviewedInput)
        throw new Error("InsufficientETHBalance");
      await run.ensureWallet(reviewed);
      if (!run.unchanged(reviewed, await requote())) return;
      if (reviewedSide === "sell") {
        const allowance = await publicClient.readContract({
          address: safeTokenAddress,
          abi: browserAbis.token,
          functionName: "allowance",
          args: [reviewed.account as Address, safeCurveAddress],
        });
        if (allowance < reviewedInput) {
          await run.approve(reviewed, {
            address: safeTokenAddress,
            abi: browserAbis.token as Abi,
            functionName: "approve",
            args: [safeCurveAddress, reviewedInput],
          });
          run.approvalsDone(
            "The curve allowance is set for this exact sell. Review again to sign.",
          );
          return;
        }
      }
      await run.sign(reviewed, {
        abi: curveAbi,
        requote,
        beforeSimulate: async () => {
          if (Number(await readPhase()) !== 0) throw new Error("WrongPhase");
        },
      });
    });
  };
  const canTrade = Boolean(
    inputUnits &&
    inputUnits > 0n &&
    minimum !== null &&
    readiness.selectedAccountVerified &&
    chainId === configuration.chainId,
  );
  const canSubmit = canTrade && confirming && (side !== "sell" || approvalTransaction.ready);
  if (!tokenAddress || !curveAddress || (engineVersion !== 1 && engineVersion !== 2))
    return (
      <UnavailableState
        title="Trading unavailable"
        description="This token contract is not valid for the selected network."
      />
    );
  if (graduated)
    return (
      <section
        className="workspace-panel transaction-panel"
        aria-labelledby="graduated-trade-title"
      >
        <div className="panel-head">
          <div>
            <p className="panel-kicker">Graduated route</p>
            <h2 id="graduated-trade-title">Router handoff</h2>
          </div>
          <Badge tone="success">Graduated</Badge>
        </div>
        {engineVersion === 2 ? (
          reviewedV4 && v4PoolReady ? (
            <V4SwapPanel token={token} addresses={reviewedV4} />
          ) : (
            <UnavailableState
              title="Pool unavailable"
              description="No reviewed Uniswap V4 deployment is enabled for this chain, or the indexed pool does not match the reviewed pool key. Trading is safely disabled."
            />
          )
        ) : reviewedRouter && reviewedWeth ? (
          <GraduatedSwapPanel token={token} router={reviewedRouter} weth={reviewedWeth} />
        ) : (
          <UnavailableState
            title="Router unavailable"
            description="No enabled reviewed Uniswap v2 router is configured for this chain. Trading is safely disabled."
          />
        )}
        {engineVersion === 2 ? claimRow : null}
        {engineVersion === 2 ? (
          <V2CreatorPanel
            tokenAddress={tokenAddress}
            curveAddress={curveAddress}
            locker={reviewedV4?.locker ?? null}
            graduated={graduated}
          />
        ) : null}
      </section>
    );
  return (
    <section className="workspace-panel transaction-panel" aria-labelledby="trade-title">
      <div className="panel-head">
        <div>
          <h2 id="trade-title">Trade {token.symbol}</h2>
        </div>
        <Badge tone={token.phase === "graduated" ? "success" : "accent"}>{token.phase}</Badge>
      </div>
      <WrongNetworkBanner trade={trade} />
      <TradeSideTabs
        side={side}
        label="Trade side"
        disabled={executionBusy}
        onChange={(next) => trade.changeSide(next)}
      >
        <div className="transaction-form">
          <TradeAmountFields
            trade={trade}
            symbol={token.symbol}
            inputHint="Integer base units are used for signing; decimals are presentation only."
            slippageHint="Minimum output is rounded down on-chain. At most 50%."
          />
          {quoteError ? (
            <p className="transaction-error" role="alert">
              {quoteError}
            </p>
          ) : null}
          {quote ? (
            <div className="quote-summary">
              <span>
                Estimated output{" "}
                <strong className="mono">{formatBaseUnits(quote.output, 18, 6)}</strong>
                {side === "sell" ? <UsdLine wei={quote.output} /> : null}
              </span>
              <span>
                Fees <strong className="mono">{eth(quote.fee)}</strong>
                <UsdLine wei={quote.fee} />
              </span>
              {taxLine ? (
                <span>
                  Anti-snipe tax ({taxLine.rate}){" "}
                  <strong className="mono">{eth(taxLine.amount)}</strong>
                  {taxLine.timeLeft ? <small> ends in {taxLine.timeLeft}</small> : null}
                </span>
              ) : null}
              <span>
                Minimum output{" "}
                <strong className="mono">
                  {minimum === null ? "—" : formatBaseUnits(minimum, 18, 6)}
                </strong>
                {side === "sell" && minimum !== null ? <UsdLine wei={minimum} /> : null}
              </span>
              {priceImpact ? (
                <span>
                  Price impact <strong className="mono">{priceImpact.percent}</strong>
                </span>
              ) : null}
              {priceImpact?.high ? (
                <p className="transaction-warning" role="status">
                  This trade moves the price by {priceImpact.percent}. A smaller amount gets a
                  better average price.
                </p>
              ) : null}
              <small>
                {quote.source === "backend"
                  ? "Informational API quote; contract quote is authoritative before signing."
                  : "Authoritative contract quote, read immediately before simulation."}
                {taxLine
                  ? " The tax only decreases, so a later block never gives less than the minimum output."
                  : null}
                {priceImpact
                  ? " Price impact is measured against the last indexed spot price."
                  : null}
              </small>
            </div>
          ) : null}
        </div>
        <p className="transaction-risk">
          Non-custodial: your selected wallet signs directly. Transactions are irreversible and may
          lose value. Contract{" "}
          {curveExplorer ? (
            <a href={curveExplorer} target="_blank" rel="noreferrer" className="mono">
              {token.curve}
            </a>
          ) : (
            <span className="mono">{token.curve}</span>
          )}
          .
        </p>
        {confirming ? (
          <div className="transaction-confirmation" role="region" aria-label="Trade confirmation">
            <h3>Confirm {reviewDetails?.side ?? side}</h3>
            <ReviewNotice notice={reviewNotice} />
            <dl>
              <dt>Token</dt>
              <dd>
                {token.name} ({token.symbol})
              </dd>
              <dt>Input</dt>
              <dd className="mono">
                {reviewDetails ? formatBaseUnits(reviewDetails.inputUnits, 18, 6) : input} ·{" "}
                {side === "buy" ? "ETH" : token.symbol}
              </dd>
              <dt>Wallet</dt>
              <dd className="mono">{reviewIntent?.account ?? "Unavailable"}</dd>
              <dt>Chain ID</dt>
              <dd className="mono">{reviewIntent?.chainId ?? "Unavailable"}</dd>
              <dt>Minimum output</dt>
              <dd className="mono">
                {reviewIntent?.minimumOutput === undefined
                  ? "—"
                  : formatBaseUnits(reviewIntent.minimumOutput, 18, 6)}
              </dd>
              <dt>Protocol fee</dt>
              <dd className="mono">{reviewDetails ? eth(reviewDetails.protocolFee) : "—"}</dd>
              <dt>Creator fee</dt>
              <dd className="mono">
                {reviewDetails ? eth(reviewDetails.fee - reviewDetails.protocolFee) : "—"}
              </dd>
              <dt>Network fee</dt>
              <dd>Shown and paid in your wallet</dd>
              <dt>Target</dt>
              <dd className="mono">{reviewIntent?.target ?? "Unavailable"}</dd>
              <dt>Function and arguments</dt>
              <dd className="mono">
                {reviewIntent
                  ? `${reviewIntent.functionName}(${reviewIntent.args.map(String).join(", ")})`
                  : "Unavailable"}
              </dd>
              <dt>Native value</dt>
              <dd className="mono">
                {reviewIntent
                  ? `${eth(reviewIntent.value)} (${reviewIntent.value.toString()} wei)`
                  : "Unavailable"}
              </dd>
              <dt>Slippage</dt>
              <dd className="mono">{slippageBps === null ? "Invalid" : `${slippage}%`}</dd>
              <dt>Deadline</dt>
              <dd className="mono">
                {reviewIntent?.deadline?.toString() ?? "Unavailable"} (Unix seconds)
              </dd>
              <dt>Network</dt>
              <dd>{configuration.deployment?.name}</dd>
              <dt>Contract</dt>
              <dd className="mono">
                {curveExplorer ? (
                  <a href={curveExplorer} target="_blank" rel="noreferrer">
                    {token.curve}
                  </a>
                ) : (
                  token.curve
                )}
              </dd>
              {side === "sell" ? (
                <>
                  <dt>Approval spender</dt>
                  <dd className="mono">
                    {curveExplorer ? (
                      <a href={curveExplorer} target="_blank" rel="noreferrer">
                        {token.curve}
                      </a>
                    ) : (
                      token.curve
                    )}{" "}
                    (exact sell amount only)
                  </dd>
                </>
              ) : null}
            </dl>
            <ReviewActions trade={trade} canSign={canSubmit} onSign={() => void execute()} />
          </div>
        ) : (
          <>
            <ReviewNotice notice={reviewNotice} />
            <Button
              variant="primary"
              size="lg"
              disabled={
                executionBusy ||
                !canTrade ||
                !transaction.ready ||
                (side === "sell" && !approvalTransaction.ready) ||
                trade.unresolved
              }
              onClick={() => void prepareTradeReview()}
            >
              {side === "sell" && approvalHash ? "Resume sell" : `Review ${side}`}
            </Button>
          </>
        )}
      </TradeSideTabs>
      <TransactionProgress
        state={state}
        tone={
          state.status === "reverted" || state.status === "rejected-signature"
            ? "danger"
            : state.status === "indexing"
              ? "warning"
              : "success"
        }
      />
      <TransactionProgress
        state={approvalTransaction.state}
        tone="warning"
        prefix="Approval "
        refreshNotice={false}
      />
      {claimRow}
      {engineVersion === 2 ? (
        <V2CreatorPanel
          tokenAddress={tokenAddress}
          curveAddress={curveAddress}
          locker={reviewedV4?.locker ?? null}
          graduated={graduated}
        />
      ) : null}
    </section>
  );
}
