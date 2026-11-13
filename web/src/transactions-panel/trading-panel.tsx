"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import { useAccount, usePublicClient, useWalletClient } from "wagmi";
import { formatBaseUnits, parseDecimal } from "@/amounts";
import { ApiClient } from "@/api/client";
import type { Abi } from "viem";
import { snipeTaxLine } from "@/anti-snipe";
import { reviewedV4Addresses } from "@/engine";
import { poolMatchesApi } from "@/v4-swap";
import { browserAbis } from "@/contracts/generated";
import { publicConfiguration } from "@/config/public";
import { addressExplorerUrl } from "@/wallet/explorer";
import { useWalletReadiness } from "@/wallet/readiness";
import {
  classifyTransactionFailure,
  decodeTransactionError,
  minimumOutput,
  parseRuntimeQuantity,
  parseSlippageBps,
  parseQuoteQuantity,
  reviewedRouterAddress,
  sameReviewedWriteIntent,
  transactionDeadline,
  type TransactionState,
  type ReviewedWriteIntent,
} from "@/transactions";
import { curvePriceImpact } from "@/price-impact";
import { TradeUsdHint, UsdLine } from "@/market/usd-line";
import { Badge, Button, Input, UnavailableState } from "@/components/primitives";
import {
  DEFAULT_TTL,
  QUOTE_DEBOUNCE_MS,
  type Address,
  eth,
  safeAddress,
  statusLabel,
  hasUnresolvedSubmission,
  currentUnixSeconds,
  ErrorCopy,
  usePersistedTransactionState,
  TradeSideTabs,
  SlippageChips,
  ReadinessGate,
  useTaxSecondsLeft,
} from "./shared";
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
  const [side, setSide] = useState<"buy" | "sell">("buy");
  const [input, setInput] = useState("");
  const [slippage, setSlippage] = useState("5");
  const [executionBusy, setExecutionBusy] = useState(false);
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
  const [confirming, setConfirming] = useState(false);
  const [reviewIntent, setReviewIntent] = useState<ReviewedWriteIntent | null>(null);
  const [reviewDetails, setReviewDetails] = useState<{
    side: "buy" | "sell";
    inputUnits: bigint;
    output: bigint;
    fee: bigint;
    protocolFee: bigint;
  } | null>(null);
  const [reviewNotice, setReviewNotice] = useState<string | null>(null);
  const executionLockRef = useRef(false);
  const configuration = publicConfiguration();
  const curveExplorer = addressExplorerUrl(configuration, safeCurveAddress);
  const readiness = useWalletReadiness();
  const account = useAccount();
  const chainId = readiness.chainId;
  const transaction = usePersistedTransactionState(
    "trade",
    configuration.chainId,
    account.address,
    tokenAddress,
    configuration.apiBaseUrl,
    !graduated,
  );
  const { state, setState } = transaction;
  const approvalTransaction = usePersistedTransactionState(
    "approval",
    configuration.chainId,
    account.address,
    tokenAddress,
    configuration.apiBaseUrl,
    !graduated,
  );
  const setApprovalTransactionState = approvalTransaction.setState;
  const approvalHash = approvalTransaction.ready
    ? (approvalTransaction.state.hash as Address | undefined)
    : undefined;
  const publicClient = usePublicClient();
  const { data: walletClient } = useWalletClient();
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
  const decimals = 18;
  const inputUnits = useMemo(() => {
    try {
      return input.trim() ? parseDecimal(input, decimals) : null;
    } catch {
      return null;
    }
  }, [input]);
  const slippageBps = useMemo(() => {
    return parseSlippageBps(slippage);
  }, [slippage]);

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
  const switchNetwork = () => void readiness.switchNetwork();
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
  const prepareTradeReview = async () => {
    if (
      !account.address ||
      !publicClient ||
      !walletClient ||
      !inputUnits ||
      slippageBps === null ||
      chainId !== configuration.chainId ||
      !readiness.selectedAccountVerified ||
      !transaction.ready ||
      (side === "sell" && !approvalTransaction.ready) ||
      hasUnresolvedSubmission(state)
    )
      return;
    try {
      const [walletAccounts, currentChainId] = await Promise.all([
        walletClient.getAddresses(),
        publicClient.getChainId(),
      ]);
      if (
        currentChainId !== configuration.chainId ||
        walletAccounts[0]?.toLowerCase() !== account.address.toLowerCase()
      )
        throw new Error("Wallet or network changed. Review again after reconnecting.");
      const phase = await publicClient.readContract({
        address: safeCurveAddress,
        abi: curveAbi,
        functionName: "phase",
      });
      if (Number(phase) !== 0) throw new Error("WrongPhase");
      const deadline = transactionDeadline(currentUnixSeconds(), DEFAULT_TTL);
      const fresh = await readCurveIntent(
        account.address as Address,
        side,
        inputUnits,
        slippageBps,
        deadline,
      );
      setQuote({
        output: fresh.output,
        fee: fresh.fee,
        protocolFee: fresh.protocolFee,
        source: "contract",
      });
      setReviewIntent(fresh.intent);
      setReviewDetails({
        side,
        inputUnits,
        output: fresh.output,
        fee: fresh.fee,
        protocolFee: fresh.protocolFee,
      });
      setReviewNotice(null);
      setQuoteError(null);
      setConfirming(true);
    } catch (cause) {
      setReviewIntent(null);
      setConfirming(false);
      setQuoteError(decodeTransactionError(cause).message);
    }
  };
  const replaceTradeReview = (
    intent: ReviewedWriteIntent,
    details: {
      side: "buy" | "sell";
      inputUnits: bigint;
      output: bigint;
      fee: bigint;
      protocolFee: bigint;
    },
  ) => {
    setReviewIntent(intent);
    setReviewDetails(details);
    setQuote({
      output: details.output,
      fee: details.fee,
      protocolFee: details.protocolFee,
      source: "contract",
    });
    setReviewNotice(
      "The wallet write details changed. Review the updated values, then confirm again.",
    );
  };
  const execute = async () => {
    if (graduated) return;
    if (executionLockRef.current) return;
    const resumingApproval = side === "sell" && approvalHash !== undefined;
    if (
      !account.address ||
      !publicClient ||
      !walletClient ||
      !inputUnits ||
      slippageBps === null ||
      !reviewIntent ||
      !reviewDetails ||
      !transaction.ready ||
      (reviewDetails.side === "sell" && !approvalTransaction.ready) ||
      (!resumingApproval && hasUnresolvedSubmission(state)) ||
      chainId !== configuration.chainId ||
      !readiness.selectedAccountVerified
    ) {
      setReviewNotice("Wallet, network, or review changed. Refresh the review before signing.");
      return;
    }
    executionLockRef.current = true;
    setExecutionBusy(true);
    const reviewed = reviewIntent;
    const reviewedSide = reviewDetails.side;
    const reviewedInput = reviewDetails.inputUnits;
    const stateBeforeReview = state;
    let submittedHash: Address | undefined;
    let approvalWrite = false;
    let failurePhase: "preflight" | "simulation" | "write" | "receipt" = "preflight";
    try {
      // Independent reads go out together; the checks keep their original order.
      const [phase, tokenBalance, nativeBalance] = await Promise.all([
        publicClient.readContract({
          address: safeCurveAddress,
          abi: curveAbi,
          functionName: "phase",
        }),
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
      const [walletAccounts, latestChainId] = await Promise.all([
        walletClient.getAddresses(),
        publicClient.getChainId(),
      ]);
      if (
        latestChainId !== reviewed.chainId ||
        walletAccounts[0]?.toLowerCase() !== reviewed.account.toLowerCase() ||
        account.address?.toLowerCase() !== reviewed.account.toLowerCase()
      ) {
        setReviewNotice("The selected wallet or network changed. Return to review before signing.");
        return;
      }
      const fresh = await readCurveIntent(
        reviewed.account as Address,
        reviewedSide,
        reviewedInput,
        slippageBps,
        reviewed.deadline!,
      );
      if (!sameReviewedWriteIntent(reviewed, fresh.intent)) {
        replaceTradeReview(fresh.intent, {
          side: reviewedSide,
          inputUnits: reviewedInput,
          output: fresh.output,
          fee: fresh.fee,
          protocolFee: fresh.protocolFee,
        });
        return;
      }
      if (reviewedSide === "sell") {
        const allowance = await publicClient.readContract({
          address: safeTokenAddress,
          abi: browserAbis.token,
          functionName: "allowance",
          args: [reviewed.account as Address, safeCurveAddress],
        });
        if (allowance < reviewedInput) {
          approvalWrite = true;
          const [approvalAccounts, approvalChainId] = await Promise.all([
            walletClient.getAddresses(),
            publicClient.getChainId(),
          ]);
          if (
            approvalChainId !== reviewed.chainId ||
            approvalAccounts[0]?.toLowerCase() !== reviewed.account.toLowerCase()
          ) {
            setReviewNotice(
              "The selected wallet or network changed. Return to review before signing.",
            );
            return;
          }
          failurePhase = "simulation";
          setApprovalTransactionState({ status: "simulating" });
          const approvalArgs = [safeCurveAddress, reviewedInput] as const;
          await publicClient.simulateContract({
            address: safeTokenAddress,
            abi: browserAbis.token,
            functionName: "approve",
            account: reviewed.account as Address,
            args: approvalArgs,
          } as never);
          setApprovalTransactionState({ status: "awaiting-signature" });
          failurePhase = "write";
          const hash = await walletClient.writeContract({
            address: safeTokenAddress,
            abi: browserAbis.token,
            functionName: "approve",
            account: reviewed.account as Address,
            args: approvalArgs,
          } as never);
          submittedHash = hash as Address;
          setApprovalTransactionState({ status: "submitted", hash: hash as Address });
          failurePhase = "receipt";
          const approvalReceipt = await publicClient.waitForTransactionReceipt({ hash });
          if (approvalReceipt.status !== "success") throw new Error("ReceiptReverted");
          setApprovalTransactionState({ status: "mined", hash: hash as Address });
          setConfirming(false);
          return;
        }
      }
      setState({ status: "simulating" });
      const phaseBeforeSign = await publicClient.readContract({
        address: safeCurveAddress,
        abi: curveAbi,
        functionName: "phase",
      });
      if (Number(phaseBeforeSign) !== 0) throw new Error("WrongPhase");
      failurePhase = "simulation";
      await publicClient.simulateContract({
        address: reviewed.target as Address,
        abi: curveAbi,
        functionName: reviewed.functionName as "buy" | "sell",
        account: reviewed.account as Address,
        args: reviewed.args as never,
        value: reviewed.value,
      } as never);
      const [finalAccounts, finalChainId] = await Promise.all([
        walletClient.getAddresses(),
        publicClient.getChainId(),
      ]);
      if (
        finalChainId !== reviewed.chainId ||
        finalAccounts[0]?.toLowerCase() !== reviewed.account.toLowerCase() ||
        !readiness.selectedAccountVerified
      ) {
        setReviewNotice("The selected wallet or network changed during simulation. Review again.");
        setState(stateBeforeReview);
        return;
      }
      const finalQuote = await readCurveIntent(
        reviewed.account as Address,
        reviewedSide,
        reviewedInput,
        slippageBps,
        reviewed.deadline!,
      );
      if (!sameReviewedWriteIntent(reviewed, finalQuote.intent)) {
        replaceTradeReview(finalQuote.intent, {
          side: reviewedSide,
          inputUnits: reviewedInput,
          output: finalQuote.output,
          fee: finalQuote.fee,
          protocolFee: finalQuote.protocolFee,
        });
        setState(stateBeforeReview);
        return;
      }
      const [writeAccounts, writeChainId] = await Promise.all([
        walletClient.getAddresses(),
        publicClient.getChainId(),
      ]);
      if (
        writeChainId !== reviewed.chainId ||
        writeAccounts[0]?.toLowerCase() !== reviewed.account.toLowerCase() ||
        !readiness.selectedAccountVerified
      ) {
        setReviewNotice("The selected wallet or network changed before signing. Review again.");
        setState(stateBeforeReview);
        return;
      }
      setState({ status: "awaiting-signature" });
      failurePhase = "write";
      const hash = await walletClient.writeContract({
        address: reviewed.target as Address,
        abi: curveAbi,
        functionName: reviewed.functionName as "buy" | "sell",
        account: reviewed.account as Address,
        args: reviewed.args as never,
        value: reviewed.value,
      } as never);
      submittedHash = hash as Address;
      setConfirming(false);
      setReviewIntent(null);
      setState({ status: "submitted", hash: hash as Address });
      failurePhase = "receipt";
      const receipt = await publicClient.waitForTransactionReceipt({ hash });
      if (receipt.status !== "success") throw new Error("ReceiptReverted");
      setState({ status: "mined", hash: hash as Address });
      setState({ status: "indexing", hash: hash as Address });
    } catch (error) {
      if (error instanceof Error && /wallet or network changed/i.test(error.message)) {
        setReviewNotice("The selected wallet or network changed. Return to review before signing.");
        return;
      }
      const message = classifyTransactionFailure(error, failurePhase);
      const failure = {
        status: message,
        hash: submittedHash ?? state.hash,
        error: decodeTransactionError(error).message,
      } satisfies TransactionState;
      if (approvalWrite) setApprovalTransactionState(failure);
      else setState(failure);
    } finally {
      executionLockRef.current = false;
      setExecutionBusy(false);
    }
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
      {readiness.address && chainId !== configuration.chainId ? (
        <div className="transaction-warning" role="alert">
          Wrong network. Switch to {configuration.deployment?.name ?? "the reviewed network"} before
          signing.
          <Button size="sm" onClick={switchNetwork}>
            Switch network
          </Button>
        </div>
      ) : null}
      <TradeSideTabs
        side={side}
        label="Trade side"
        disabled={executionBusy}
        onChange={(next) => {
          setSide(next);
          setInput("");
          setConfirming(false);
          setReviewIntent(null);
          setReviewNotice(null);
        }}
      >
        <div className="transaction-form">
          <Input
            label={side === "buy" ? "ETH input" : `${token.symbol} input`}
            value={input}
            disabled={executionBusy}
            onChange={(event) => {
              setInput(event.target.value);
              setConfirming(false);
              setReviewIntent(null);
            }}
            inputMode="decimal"
            placeholder="0.00"
            hint="Integer base units are used for signing; decimals are presentation only."
          />
          {side === "buy" ? <TradeUsdHint ethInput={input} /> : <TradeUsdHint />}
          <Input
            label="Slippage tolerance (%)"
            value={slippage}
            disabled={executionBusy}
            onChange={(event) => {
              setSlippage(event.target.value);
              setConfirming(false);
              setReviewIntent(null);
            }}
            inputMode="decimal"
            hint="Minimum output is rounded down on-chain. At most 50%."
          />
          <SlippageChips
            value={slippage}
            disabled={executionBusy}
            onPick={(value) => {
              setSlippage(value);
              setConfirming(false);
              setReviewIntent(null);
            }}
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
            {reviewNotice ? (
              <p className="transaction-warning" role="status">
                {reviewNotice}
              </p>
            ) : null}
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
            <div className="transaction-actions">
              <Button
                variant="quiet"
                disabled={executionBusy}
                onClick={() => {
                  setConfirming(false);
                  setReviewIntent(null);
                  setReviewNotice(null);
                }}
              >
                Back
              </Button>
              <Button
                variant="primary"
                loading={state.status === "simulating" || state.status === "awaiting-signature"}
                disabled={executionBusy || !canSubmit || !reviewIntent || !transaction.ready}
                onClick={() => void execute()}
              >
                Sign {side}
              </Button>
            </div>
          </div>
        ) : (
          <Button
            variant="primary"
            size="lg"
            disabled={
              executionBusy ||
              !canTrade ||
              !transaction.ready ||
              (side === "sell" && !approvalTransaction.ready) ||
              hasUnresolvedSubmission(state)
            }
            onClick={() => void prepareTradeReview()}
          >
            {side === "sell" && approvalHash ? "Resume sell" : `Review ${side}`}
          </Button>
        )}
      </TradeSideTabs>
      {state.status !== "disconnected" ? (
        <div className="transaction-progress" role="status">
          <Badge
            tone={
              state.status === "reverted" || state.status === "rejected-signature"
                ? "danger"
                : state.status === "indexing"
                  ? "warning"
                  : "success"
            }
          >
            {statusLabel(state.status)}
          </Badge>
          {state.hash ? <span className="mono">{state.hash}</span> : null}
          {state.error ? <ErrorCopy error={new Error(state.error)} /> : null}
          {state.canonicalRefreshUnavailable ? (
            <span className="transaction-error" role="alert">
              Canonical status refresh unavailable. Showing the last known status.
            </span>
          ) : null}
        </div>
      ) : null}
      {approvalTransaction.state.status !== "disconnected" ? (
        <div className="transaction-progress" role="status">
          <Badge tone="warning">Approval {statusLabel(approvalTransaction.state.status)}</Badge>
          {approvalTransaction.state.hash ? (
            <span className="mono">{approvalTransaction.state.hash}</span>
          ) : null}
          {approvalTransaction.state.error ? (
            <ErrorCopy error={new Error(approvalTransaction.state.error)} />
          ) : null}
        </div>
      ) : null}
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
