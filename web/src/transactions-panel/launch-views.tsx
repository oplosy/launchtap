"use client";

import type { launchRulesV2 } from "@/launch-v2";
import type { ReviewedWriteIntent } from "@/transactions";
import { type Address, eth } from "./shared";
import { ReviewActions, ReviewNotice, type ReviewedWrite } from "./reviewed-trade";

/**
 * Reasons the launch (or its developer buy) cannot go through right now, read from the factory.
 * Empty until the factory has been read: a failed read already shows its own error.
 */
export function launchBlockers(
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

export function LaunchPreview({
  previewUrl,
  name,
  symbol,
  launchFee,
  value,
  v2Rules,
}: {
  /** A `blob:` URL of the chosen file; never a remote URL. */
  previewUrl: string | null;
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
        {previewUrl ? (
          // A local object URL of the file being uploaded; CSP allows blob: images.
          /* eslint-disable-next-line @next/next/no-img-element */
          <img src={previewUrl} alt="Token preview" />
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

/** The reviewed `launch` call exactly as the wallet will be asked to sign it. */
export function LaunchConfirmation({
  write,
  reviewed,
  launchFee,
  networkName,
  factory,
  factoryExplorer,
  notice,
  canSign,
  onSign,
}: {
  write: ReviewedWrite;
  reviewed: ReviewedWriteIntent;
  launchFee: bigint | null;
  networkName: string;
  factory: Address;
  factoryExplorer: string | null;
  notice: string | null;
  canSign: boolean;
  onSign: () => void;
}) {
  const params = reviewed.args[0] as { name: string; symbol: string; developerBuyGross: bigint };
  return (
    <div className="transaction-confirmation">
      <h3>Confirm launch</h3>
      <ReviewNotice notice={notice} />
      <dl>
        <dt>Token</dt>
        <dd>
          {params.name || "—"} ({params.symbol || "—"})
        </dd>
        <dt>Launch value</dt>
        <dd className="mono">{eth(reviewed.value)}</dd>
        <dt>Fee</dt>
        <dd className="mono">{launchFee === null ? "Unavailable" : eth(launchFee)}</dd>
        <dt>Developer buy / minimum output</dt>
        <dd className="mono">{eth(params.developerBuyGross)} / 0 tokens</dd>
        <dt>Network</dt>
        <dd>{networkName}</dd>
        <dt>Contract</dt>
        <dd className="mono">
          {factoryExplorer ? (
            <a href={factoryExplorer} target="_blank" rel="noreferrer">
              {factory}
            </a>
          ) : (
            factory
          )}
        </dd>
        <dt>Deadline</dt>
        <dd className="mono">{reviewed.deadline?.toString() ?? "Unavailable"} (Unix seconds)</dd>
      </dl>
      <ReviewActions write={write} label="Sign launch" canSign={canSign} onSign={onSign} />
    </div>
  );
}
