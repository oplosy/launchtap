"use client";

import Link from "next/link";
import { useEffect, useMemo, useRef, useState } from "react";
import { useAccount, usePublicClient, useWalletClient } from "wagmi";
import { parseDecimal } from "@/amounts";
import type { Abi } from "viem";
import { launchRulesV2 } from "@/launch-v2";
import { browserAbis, type ReviewedDeployment } from "@/contracts/generated";
import { publicConfiguration } from "@/config/public";
import { addressExplorerUrl } from "@/wallet/explorer";
import { useWalletReadiness } from "@/wallet/readiness";
import { WalletConnectButton } from "@/wallet/connection-context";
import {
  calculateLaunchValue,
  classifyTransactionFailure,
  decodeTransactionError,
  sameWriteIntent,
  transactionDeadline,
  validateLaunchInput,
  createTransactionState,
  transitionTransaction,
} from "@/transactions";
import { Badge, Button, ErrorState, InfoTip, Input, SafeImage } from "@/components/primitives";
import {
  DEFAULT_TTL,
  type Address,
  eth,
  statusLabel,
  hasUnresolvedSubmission,
  currentUnixSeconds,
  ErrorCopy,
  usePersistedTransactionState,
  ReadinessGate,
} from "./shared";

/**
 * Reasons the launch (or its developer buy) cannot go through right now, read from the factory.
 * Empty until the factory has been read: a failed read already shows its own error.
 */
function launchBlockers(
  defaultsRead: boolean,
  launchesPaused: boolean | null,
  tradingPaused: boolean | null,
  engineEnabled: boolean | null,
): string[] {
  const blockers: string[] = [];
  if (!defaultsRead) return blockers;
  if (launchesPaused !== false) blockers.push("Launches are paused by the protocol.");
  if (engineEnabled !== true) blockers.push("Engine v1 is disabled, so launching is unavailable.");
  if (tradingPaused) blockers.push("Buying is paused, so a developer buy cannot go through.");
  return blockers;
}

export function LaunchPanel() {
  return (
    <ReadinessGate>
      <LaunchPanelReady />
    </ReadinessGate>
  );
}

function LaunchPreview({
  imageUrl,
  name,
  symbol,
  launchFee,
  value,
  v2Rules,
}: {
  imageUrl: string;
  name: string;
  symbol: string;
  launchFee: bigint | null;
  value: bigint | null;
  /** Engine 2 only; `null` while `futureDefaults()` is unread. */
  v2Rules?: ReturnType<typeof launchRulesV2> | null;
}) {
  const v2 = v2Rules !== undefined;
  return (
    <aside className="launch-preview-card" aria-label="Token preview">
      <div className="launch-preview-image">
        {imageUrl.trim() ? (
          <SafeImage src={imageUrl} alt="Token preview" />
        ) : (
          <span aria-hidden="true">◈</span>
        )}
      </div>
      <div className="launch-preview-identity">
        <p className="launch-preview-name">{name.trim() || "Your token"}</p>
        <p className="mono">{symbol.trim() || "ticker"}</p>
      </div>
      <dl>
        <div>
          <dt>Launch fee</dt>
          <dd>{launchFee === null ? "Unavailable" : eth(launchFee)}</dd>
        </div>
        <div>
          <dt>Paired with</dt>
          <dd>ETH</dd>
        </div>
        <div>
          <dt>Trade fee</dt>
          <dd>1.00%</dd>
        </div>
        <div>
          <dt>Launch value</dt>
          <dd>{value === null ? "Unavailable" : eth(value)}</dd>
        </div>
        <div>
          <dt>Graduation</dt>
          <dd>4.2 ETH</dd>
        </div>
        {v2 ? (
          <>
            <div>
              <dt>Anti-snipe tax</dt>
              <dd>
                {v2Rules
                  ? `${v2Rules.taxStart}, decaying to 0 over ${v2Rules.taxWindow}`
                  : "Unavailable"}
              </dd>
            </div>
            <div>
              <dt>Pool fee</dt>
              <dd>
                {v2Rules
                  ? `1% · ${v2Rules.lpCreatorShare} creator / ${v2Rules.lpProtocolShare} protocol`
                  : "Unavailable"}
              </dd>
            </div>
            <div>
              <dt>Liquidity</dt>
              <dd>Uniswap V4 position, locked forever</dd>
            </div>
          </>
        ) : (
          <div>
            <dt>Liquidity</dt>
            <dd>Initial LP burned</dd>
          </div>
        )}
      </dl>
    </aside>
  );
}

function LaunchPanelReady() {
  const configuration = publicConfiguration();
  const readiness = useWalletReadiness();
  const account = useAccount();
  const launchChainId = readiness.chainId;
  const publicClient = usePublicClient();
  const { data: walletClient } = useWalletClient();
  const transaction = usePersistedTransactionState(
    "launch",
    configuration.chainId,
    account.address,
    null,
    configuration.apiBaseUrl,
  );
  const { state, setState } = transaction;
  const [name, setName] = useState("");
  const [symbol, setSymbol] = useState("");
  const [imageUrl, setImageUrl] = useState("");
  const [xUrl, setXUrl] = useState("");
  const [telegramUrl, setTelegramUrl] = useState("");
  const [buy, setBuy] = useState("0");
  const [confirming, setConfirming] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [launchFee, setLaunchFee] = useState<bigint | null>(null);
  const [defaultsRead, setDefaultsRead] = useState(false);
  const [launchesPaused, setLaunchesPaused] = useState<boolean | null>(null);
  const [tradingPaused, setTradingPaused] = useState<boolean | null>(null);
  const [engineEnabled, setEngineEnabled] = useState<boolean | null>(null);
  const executionLockRef = useRef(false);
  const deployment = configuration.deployment as ReviewedDeployment;
  // One deployment serves one engine (Plan v2-B B2). LaunchFactoryV2 has no per-engine switch;
  // its launch reverts on its own pause, so the engine-enabled read is v1 only.
  const engineVersion = deployment.engineVersion;
  const factoryAbi = (engineVersion === 2 ? browserAbis.factoryV2 : browserAbis.factory) as Abi;
  const [v2Rules, setV2Rules] = useState<ReturnType<typeof launchRulesV2> | null>(null);
  const factoryExplorer = deployment.factory
    ? addressExplorerUrl(configuration, deployment.factory as Address)
    : null;
  const switchNetwork = () => void readiness.switchNetwork();
  useEffect(() => {
    if (!publicClient || !deployment.factory) return;
    void Promise.all([
      publicClient.readContract({
        address: deployment.factory as Address,
        abi: factoryAbi,
        functionName: "launchFee",
      }),
      publicClient.readContract({
        address: deployment.factory as Address,
        abi: factoryAbi,
        functionName: "futureDefaults",
      }),
      publicClient.readContract({
        address: deployment.factory as Address,
        abi: factoryAbi,
        functionName: "launchesPaused",
      }),
      publicClient.readContract({
        address: deployment.factory as Address,
        abi: factoryAbi,
        functionName: "tradingPaused",
      }),
      engineVersion === 2
        ? Promise.resolve(true)
        : publicClient.readContract({
            address: deployment.factory as Address,
            abi: factoryAbi,
            functionName: "engineEnabled",
            args: [1],
          }),
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
  const submit = async () => {
    if (executionLockRef.current) return;
    if (
      Object.keys(validMetadata).length ||
      !publicClient ||
      !walletClient ||
      !account.address ||
      !deployment.factory ||
      value === null ||
      launchFee === null ||
      !readiness.selectedAccountVerified ||
      !transaction.ready ||
      hasUnresolvedSubmission(state)
    )
      return;
    setConfirming(false);
    executionLockRef.current = true;
    let submittedHash: Address | undefined;
    let failurePhase: "preflight" | "simulation" | "write" | "receipt" = "preflight";
    try {
      setState(
        transitionTransaction(
          createTransactionState("disconnected"),
          { type: "validate" },
          readiness.transactionReadiness,
        ),
      );
      const deadline = transactionDeadline(currentUnixSeconds(), DEFAULT_TTL);
      const args = [
        {
          name: name.trim(),
          symbol: symbol.trim(),
          engineVersion,
          developerBuyGross: buyUnits!,
          minDeveloperTokensOut: 0n,
          deadline,
        },
      ] as const;
      const [latestLaunchFee, latestLaunchesPaused, latestTradingPaused, latestEngineEnabled] =
        await Promise.all([
          publicClient.readContract({
            address: deployment.factory as Address,
            abi: factoryAbi,
            functionName: "launchFee",
          }) as Promise<bigint>,
          publicClient.readContract({
            address: deployment.factory as Address,
            abi: factoryAbi,
            functionName: "launchesPaused",
          }) as Promise<boolean>,
          publicClient.readContract({
            address: deployment.factory as Address,
            abi: factoryAbi,
            functionName: "tradingPaused",
          }) as Promise<boolean>,
          (engineVersion === 2
            ? Promise.resolve(true)
            : publicClient.readContract({
                address: deployment.factory as Address,
                abi: factoryAbi,
                functionName: "engineEnabled",
                args: [1],
              })) as Promise<boolean>,
        ]);
      if (!latestEngineEnabled) throw new Error("EngineDisabled");
      if (latestLaunchesPaused) throw new Error("LaunchesPaused");
      if (buyUnits! > 0n && latestTradingPaused) throw new Error("TradingPaused");
      const exactValue = calculateLaunchValue(latestLaunchFee, buyUnits!);
      const nativeBalance = await publicClient.getBalance({ address: account.address });
      if (nativeBalance < exactValue) throw new Error("InsufficientETHBalance");
      if (latestLaunchFee !== launchFee || exactValue !== value)
        throw new Error("LaunchValueMismatch");
      const capturedIntent = {
        account: account.address,
        target: deployment.factory,
        value: exactValue,
        args,
        deadline,
      };
      const [simulationAccounts, simulationChainId] = await Promise.all([
        walletClient.getAddresses(),
        publicClient.getChainId(),
      ]);
      if (
        simulationChainId !== configuration.chainId ||
        simulationAccounts[0]?.toLowerCase() !== account.address.toLowerCase()
      )
        throw new Error("Wallet or network changed; review the launch again.");
      failurePhase = "simulation";
      await publicClient.simulateContract({
        address: deployment.factory as Address,
        abi: factoryAbi,
        functionName: "launch",
        account: account.address,
        args,
        value: exactValue,
      } as never);
      const [beforeSignLaunchesPaused, beforeSignEngineEnabled] = await Promise.all([
        publicClient.readContract({
          address: deployment.factory as Address,
          abi: factoryAbi,
          functionName: "launchesPaused",
        }) as Promise<boolean>,
        (engineVersion === 2
          ? Promise.resolve(true)
          : publicClient.readContract({
              address: deployment.factory as Address,
              abi: factoryAbi,
              functionName: "engineEnabled",
              args: [1],
            })) as Promise<boolean>,
      ]);
      if (beforeSignLaunchesPaused) throw new Error("LaunchesPaused");
      if (!beforeSignEngineEnabled) throw new Error("EngineDisabled");
      const [writeAccounts, writeChainId] = await Promise.all([
        walletClient.getAddresses(),
        publicClient.getChainId(),
      ]);
      if (
        writeChainId !== configuration.chainId ||
        writeAccounts[0]?.toLowerCase() !== account.address.toLowerCase()
      )
        throw new Error("Wallet or network changed; review the launch again.");
      const writeIntent = {
        account: account.address,
        target: deployment.factory,
        value: exactValue,
        args,
        deadline,
      };
      if (!sameWriteIntent(capturedIntent, writeIntent))
        throw new Error("Write intent changed; retry safely.");
      setState({ status: "awaiting-signature" });
      failurePhase = "write";
      const hash = await walletClient.writeContract({
        address: deployment.factory as Address,
        abi: factoryAbi,
        functionName: "launch",
        account: account.address,
        args,
        value: exactValue,
      } as never);
      submittedHash = hash as Address;
      setState({ status: "submitted", hash: hash as Address });
      failurePhase = "receipt";
      const receipt = await publicClient.waitForTransactionReceipt({ hash });
      if (receipt.status !== "success") throw new Error("ReceiptReverted");
      setState({ status: "indexing", hash: hash as Address });
    } catch (cause) {
      setState({
        status: classifyTransactionFailure(cause, failurePhase),
        hash: submittedHash,
        error: decodeTransactionError(cause).message,
      });
    } finally {
      executionLockRef.current = false;
    }
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
          {readiness.address && launchChainId !== configuration.chainId ? (
            <div className="transaction-warning" role="alert">
              Wrong network. Switch to {deployment.name} before signing.
              <Button size="sm" onClick={switchNetwork}>
                Switch network
              </Button>
            </div>
          ) : null}
          <div className="launch-layout">
            <div className="launch-main">
              <div className="transaction-form">
                <section className="launch-section" aria-labelledby="launch-step-image">
                  <h2 id="launch-step-image">Image</h2>
                  <Input
                    label="Token image"
                    value={imageUrl}
                    onChange={(event) => setImageUrl(event.target.value)}
                    placeholder="https://…"
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
                      onChange={(event) => setName(event.target.value)}
                      maxLength={64}
                      placeholder="Token name"
                    />
                    <Input
                      label="Ticker"
                      value={symbol}
                      onChange={(event) => setSymbol(event.target.value.toUpperCase())}
                      maxLength={16}
                      placeholder="Symbol"
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
                      onChange={(event) => setXUrl(event.target.value)}
                      placeholder="https://x.com/handle"
                    />
                    <Input
                      label="Telegram"
                      value={telegramUrl}
                      onChange={(event) => setTelegramUrl(event.target.value)}
                      placeholder="https://t.me/community"
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
                      label="Developer buy"
                      value={buy}
                      onChange={(event) => setBuy(event.target.value)}
                      inputMode="decimal"
                      placeholder="0 ETH, optional"
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
                {confirming ? (
                  <div className="transaction-confirmation">
                    <h3>Confirm launch</h3>
                    <dl>
                      <dt>Token</dt>
                      <dd>
                        {name || "—"} ({symbol || "—"})
                      </dd>
                      <dt>Launch value</dt>
                      <dd className="mono">{value === null ? "Unavailable" : eth(value)}</dd>
                      <dt>Fee</dt>
                      <dd className="mono">
                        {launchFee === null ? "Unavailable" : eth(launchFee)}
                      </dd>
                      <dt>Developer buy / minimum output</dt>
                      <dd className="mono">{buy || "0"} ETH / 0 tokens</dd>
                      <dt>Network</dt>
                      <dd>{deployment.name}</dd>
                      <dt>Contract</dt>
                      <dd className="mono">
                        {factoryExplorer ? (
                          <a href={factoryExplorer} target="_blank" rel="noreferrer">
                            {deployment.factory}
                          </a>
                        ) : (
                          deployment.factory
                        )}
                      </dd>
                      <dt>Deadline</dt>
                      <dd className="mono">
                        {transactionDeadline(currentUnixSeconds(), DEFAULT_TTL).toString()} (Unix
                        seconds)
                      </dd>
                    </dl>
                    <div className="transaction-actions">
                      <Button variant="quiet" onClick={() => setConfirming(false)}>
                        Back
                      </Button>
                      <Button
                        variant="primary"
                        loading={state.status === "awaiting-signature"}
                        onClick={() => void submit()}
                      >
                        Sign launch
                      </Button>
                    </div>
                  </div>
                ) : !readiness.address ? (
                  <WalletConnectButton
                    className="launch-wallet-button"
                    variant="primary"
                    size="lg"
                  />
                ) : (
                  <Button
                    variant="primary"
                    size="lg"
                    disabled={
                      Object.keys(validMetadata).length > 0 ||
                      value === null ||
                      !defaultsRead ||
                      (engineVersion === 2 && v2Rules === null) ||
                      launchesPaused !== false ||
                      engineEnabled !== true ||
                      !readiness.selectedAccountVerified ||
                      !transaction.ready ||
                      hasUnresolvedSubmission(state)
                    }
                    onClick={() => setConfirming(true)}
                  >
                    Review launch
                  </Button>
                )}
              </div>
            </div>
          </div>
        </>
      )}
      {state.status !== "disconnected" ? (
        <div className="transaction-progress" role="status">
          <Badge tone={state.status === "reverted" ? "danger" : "warning"}>
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
    </section>
  );
}
