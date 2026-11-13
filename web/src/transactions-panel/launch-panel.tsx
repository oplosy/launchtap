"use client";

import Link from "next/link";
import { useEffect, useMemo, useState } from "react";
import { parseDecimal } from "@/amounts";
import type { Abi } from "viem";
import { launchRulesV2 } from "@/launch-v2";
import { browserAbis, type ReviewedDeployment } from "@/contracts/generated";
import { addressExplorerUrl } from "@/wallet/explorer";
import { WalletConnectButton } from "@/wallet/connection-context";
import {
  calculateLaunchValue,
  transactionDeadline,
  validateLaunchInput,
  createTransactionState,
  transitionTransaction,
  type ReviewedWriteIntent,
} from "@/transactions";
import { Button, ErrorState, InfoTip, Input, SafeImage } from "@/components/primitives";
import {
  DEFAULT_TTL,
  type Address,
  currentUnixSeconds,
  ReadinessGate,
  TransactionProgress,
} from "./shared";
import { ReviewNotice, useReviewedWrite, WrongNetworkBanner } from "./reviewed-trade";
import { LaunchConfirmation, LaunchPreview, launchBlockers } from "./launch-views";

export function LaunchPanel() {
  return (
    <ReadinessGate>
      <LaunchPanelReady />
    </ReadinessGate>
  );
}

function LaunchPanelReady() {
  const write = useReviewedWrite("launch", null);
  const {
    account,
    readiness,
    publicClient,
    configuration,
    state,
    confirming,
    reviewIntent,
    reviewNotice,
  } = write;
  const [name, setName] = useState("");
  const [symbol, setSymbol] = useState("");
  const [imageUrl, setImageUrl] = useState("");
  const [xUrl, setXUrl] = useState("");
  const [telegramUrl, setTelegramUrl] = useState("");
  const [buy, setBuy] = useState("0");
  const [error, setError] = useState<string | null>(null);
  const [launchFee, setLaunchFee] = useState<bigint | null>(null);
  const [defaultsRead, setDefaultsRead] = useState(false);
  const [launchesPaused, setLaunchesPaused] = useState<boolean | null>(null);
  const [tradingPaused, setTradingPaused] = useState<boolean | null>(null);
  const [engineEnabled, setEngineEnabled] = useState<boolean | null>(null);
  const deployment = configuration.deployment as ReviewedDeployment;
  // One deployment serves one engine (Plan v2-B B2). LaunchFactoryV2 has no per-engine switch;
  // its launch reverts on its own pause, so the engine-enabled read is v1 only.
  const engineVersion = deployment.engineVersion;
  const factoryAbi = (engineVersion === 2 ? browserAbis.factoryV2 : browserAbis.factory) as Abi;
  const [v2Rules, setV2Rules] = useState<ReturnType<typeof launchRulesV2> | null>(null);
  const factoryExplorer = deployment.factory
    ? addressExplorerUrl(configuration, deployment.factory as Address)
    : null;
  const readFactory = (functionName: string, args?: readonly unknown[]) =>
    publicClient!.readContract({
      address: deployment.factory as Address,
      abi: factoryAbi,
      functionName,
      args,
    } as never) as Promise<unknown>;
  /** v1 factories gate each engine; a v2 factory has a single engine and only its pause. */
  const readEngineEnabled = () =>
    engineVersion === 2 ? Promise.resolve(true) : readFactory("engineEnabled", [1]);
  useEffect(() => {
    if (!publicClient || !deployment.factory) return;
    const read = (functionName: string, args?: readonly unknown[]) =>
      publicClient.readContract({
        address: deployment.factory as Address,
        abi: factoryAbi,
        functionName,
        args,
      } as never) as Promise<unknown>;
    void Promise.all([
      read("launchFee"),
      read("futureDefaults"),
      read("launchesPaused"),
      read("tradingPaused"),
      engineVersion === 2 ? Promise.resolve(true) : read("engineEnabled", [1]),
    ])
      .then(([fee, defaults, paused, trading, enabled]) => {
        setLaunchFee(fee as bigint);
        // Plan v2-C D8: the exact profile the next launch snapshots; a failed read blocks launch.
        if (engineVersion === 2)
          setV2Rules(launchRulesV2(defaults as Parameters<typeof launchRulesV2>[0]));
        setDefaultsRead(true);
        setLaunchesPaused(Boolean(paused));
        setTradingPaused(Boolean(trading));
        setEngineEnabled(Boolean(enabled));
      })
      .catch(() => setError("Factory configuration unavailable; launching is disabled."));
  }, [deployment.factory, engineVersion, factoryAbi, publicClient]);
  const buyUnits = useMemo(() => {
    try {
      return parseDecimal(buy || "0");
    } catch {
      return null;
    }
  }, [buy]);
  const value =
    launchFee !== null && buyUnits !== null ? calculateLaunchValue(launchFee, buyUnits) : null;
  const validMetadata = validateLaunchInput({
    name,
    symbol,
    imageUrl,
    xUrl,
    telegramUrl,
    developerBuyGross: buyUnits ?? -1n,
  });
  // Shown under a field once it has content; an empty required field only keeps review disabled.
  const fieldError = (field: keyof typeof validMetadata, input: string) =>
    input.trim() ? validMetadata[field] : undefined;
  const canReview =
    Object.keys(validMetadata).length === 0 &&
    value !== null &&
    buyUnits !== null &&
    defaultsRead &&
    (engineVersion !== 2 || v2Rules !== null) &&
    launchesPaused === false &&
    engineEnabled === true &&
    Boolean(deployment.factory) &&
    configuration.chainId !== null &&
    write.walletReady &&
    !write.unresolved;
  /** The exact `launch` call the confirmation shows and the wallet later signs. */
  const reviewLaunch = () => {
    if (!canReview || !account.address) return;
    const deadline = transactionDeadline(currentUnixSeconds(), DEFAULT_TTL);
    write.openReview({
      account: account.address,
      target: deployment.factory as Address,
      value: value!,
      args: [
        {
          name: name.trim(),
          symbol: symbol.trim(),
          engineVersion,
          developerBuyGross: buyUnits!,
          minDeveloperTokensOut: 0n,
          deadline,
        },
      ] as const,
      deadline,
      chainId: configuration.chainId!,
      functionName: "launch",
    } satisfies ReviewedWriteIntent);
  };
  const submit = async () => {
    if (!canReview || !reviewIntent || !publicClient) return;
    const reviewed = reviewIntent;
    const reviewedBuy = (reviewed.args[0] as { developerBuyGross: bigint }).developerBuyGross;
    await write.runExecution(async (run) => {
      // Throws unless the wallet and configuration are ready to sign.
      write.transaction.setState(
        transitionTransaction(
          createTransactionState("disconnected"),
          { type: "validate" },
          readiness.transactionReadiness,
        ),
      );
      const [latestLaunchFee, latestLaunchesPaused, latestTradingPaused, latestEngineEnabled] =
        await Promise.all([
          readFactory("launchFee") as Promise<bigint>,
          readFactory("launchesPaused"),
          readFactory("tradingPaused"),
          readEngineEnabled(),
        ]);
      if (!latestEngineEnabled) throw new Error("EngineDisabled");
      if (latestLaunchesPaused) throw new Error("LaunchesPaused");
      if (reviewedBuy > 0n && latestTradingPaused) throw new Error("TradingPaused");
      const exactValue = calculateLaunchValue(latestLaunchFee, reviewedBuy);
      const nativeBalance = await publicClient.getBalance({
        address: reviewed.account as Address,
      });
      if (nativeBalance < exactValue) throw new Error("InsufficientETHBalance");
      if (exactValue !== reviewed.value) throw new Error("LaunchValueMismatch");
      await run.ensureWallet(reviewed);
      await run.sign(reviewed, {
        abi: factoryAbi,
        // After simulation and right before signing: a pause or a fee change stops the launch.
        requote: async () => {
          const [fee, paused, enabled] = await Promise.all([
            readFactory("launchFee") as Promise<bigint>,
            readFactory("launchesPaused"),
            readEngineEnabled(),
          ]);
          if (paused) throw new Error("LaunchesPaused");
          if (!enabled) throw new Error("EngineDisabled");
          return { intent: { ...reviewed, value: calculateLaunchValue(fee, reviewedBuy) } };
        },
      });
    });
  };
  /** Any edit after review invalidates the reviewed call; the user reviews again. */
  const edit = (setter: (value: string) => void) => (value: string) => {
    setter(value);
    write.closeReview();
  };
  return (
    <section
      className="workspace-panel transaction-panel launch-panel"
      aria-labelledby="launch-title"
    >
      {/* Named for assistive tech and the page outline; the form speaks for itself visually. */}
      <h1 id="launch-title" className="sr-only">
        Launch token
      </h1>
      {error ? (
        <ErrorState title="Launch unavailable" description={error} />
      ) : (
        <>
          <WrongNetworkBanner write={write} />
          <div className="launch-layout">
            <div className="launch-main">
              <div className="transaction-form">
                <section className="launch-section" aria-labelledby="launch-step-image">
                  <h2 id="launch-step-image">Image</h2>
                  <Input
                    label="Token image"
                    value={imageUrl}
                    onChange={(event) => edit(setImageUrl)(event.target.value)}
                    placeholder="https://…"
                    error={fieldError("imageUrl", imageUrl)}
                    info="An https image URL. It becomes the token's artwork on cards and its page."
                  />
                  {imageUrl ? (
                    <SafeImage
                      src={imageUrl}
                      alt="Token image preview"
                      className="launch-image-preview"
                    />
                  ) : null}
                </section>
                <section className="launch-section" aria-labelledby="launch-step-name">
                  <h2 id="launch-step-name">Name and symbol</h2>
                  <div className="launch-field-row">
                    <Input
                      label="Name"
                      value={name}
                      onChange={(event) => edit(setName)(event.target.value)}
                      maxLength={64}
                      placeholder="Token name"
                      error={fieldError("name", name)}
                    />
                    <Input
                      label="Ticker"
                      value={symbol}
                      onChange={(event) => edit(setSymbol)(event.target.value.toUpperCase())}
                      maxLength={16}
                      placeholder="Symbol"
                      error={fieldError("symbol", symbol)}
                      info="Up to 16 characters, shown in capitals."
                    />
                  </div>
                </section>
                <section className="launch-section" aria-labelledby="launch-step-links">
                  <h2 id="launch-step-links">
                    Links
                    <InfoTip
                      align="start"
                      label="links"
                      text="Optional. Add a description from the token page after launch."
                    />
                  </h2>
                  <div className="launch-field-row">
                    <Input
                      label="X profile"
                      value={xUrl}
                      onChange={(event) => edit(setXUrl)(event.target.value)}
                      placeholder="https://x.com/handle"
                      error={fieldError("xUrl", xUrl)}
                    />
                    <Input
                      label="Telegram"
                      value={telegramUrl}
                      onChange={(event) => edit(setTelegramUrl)(event.target.value)}
                      placeholder="https://t.me/community"
                      error={fieldError("telegramUrl", telegramUrl)}
                    />
                  </div>
                </section>
                <section className="launch-section" aria-labelledby="launch-step-buy">
                  <h2 id="launch-step-buy">Initial buy</h2>
                  <div className="launch-field-row">
                    <div className="ui-field">
                      <span>Paired asset</span>
                      <div className="ui-input-wrap ui-select-wrap">
                        <select
                          className="ui-input ui-input-with-info"
                          defaultValue="ETH"
                          aria-label="Paired asset"
                        >
                          <option>ETH</option>
                        </select>
                        <InfoTip
                          label="Paired asset"
                          text="Graduates to Uniswap once the curve raises 4.2 ETH."
                        />
                      </div>
                    </div>
                    <Input
                      label="Developer buy (ETH)"
                      value={buy}
                      onChange={(event) => edit(setBuy)(event.target.value)}
                      inputMode="decimal"
                      placeholder="0 ETH, optional"
                      error={
                        buyUnits === null
                          ? "Enter an ETH amount, like 0.1."
                          : fieldError("developerBuyGross", buy)
                      }
                      info={
                        engineVersion === 2
                          ? "Optional. Launch value is the launch fee plus this buy. The developer buy is exempt from the anti-snipe tax."
                          : "Optional. Launch value is the launch fee plus this buy."
                      }
                    />
                  </div>
                </section>
              </div>
              {/* Right column on wide screens: live preview, terms and the launch action stay in view. */}
              <div className="launch-aside">
                <LaunchPreview
                  imageUrl={imageUrl}
                  name={name}
                  symbol={symbol}
                  launchFee={launchFee}
                  value={value}
                  v2Rules={engineVersion === 2 ? v2Rules : undefined}
                />
                {/*
                  The preview card already shows the fee and the exact launch value; factory state
                  only earns a line when it blocks the launch or the developer buy.
                */}
                {launchBlockers(defaultsRead, launchesPaused, tradingPaused, engineEnabled).map(
                  (blocker) => (
                    <p key={blocker} className="transaction-error" role="alert">
                      {blocker}
                    </p>
                  ),
                )}
                <Link className="launch-help" href={"/docs#flow" as never}>
                  <span className="launch-help-mark" aria-hidden="true">
                    ?
                  </span>
                  How launching works
                </Link>
                {confirming && reviewIntent ? (
                  <LaunchConfirmation
                    write={write}
                    reviewed={reviewIntent}
                    launchFee={launchFee}
                    networkName={deployment.name}
                    factory={deployment.factory as Address}
                    factoryExplorer={factoryExplorer}
                    notice={reviewNotice}
                    canSign={canReview}
                    onSign={() => void submit()}
                  />
                ) : !readiness.address ? (
                  <WalletConnectButton
                    className="launch-wallet-button"
                    variant="primary"
                    size="lg"
                  />
                ) : (
                  <>
                    <ReviewNotice notice={reviewNotice} />
                    <Button
                      variant="primary"
                      size="lg"
                      disabled={!canReview}
                      onClick={reviewLaunch}
                    >
                      Review launch
                    </Button>
                  </>
                )}
              </div>
            </div>
          </div>
        </>
      )}
      <TransactionProgress
        state={state}
        tone={state.status === "reverted" ? "danger" : "warning"}
      />
    </section>
  );
}
