"use client";

import { formatSignificantPrice } from "@/amounts";
import { UsdLine } from "@/market/usd-line";
import { addressExplorerUrl } from "@/wallet/explorer";
import { type LaunchFacts } from "./launch-facts";
import { type V2Facts } from "./v2-facts";
import { amount, safeTokenAmount } from "./token-format";

/** Engine-2 launch, pool and fee recipient facts (Plan v2-C Task 8), from the indexed API. */
export function V2FactsPanel({
  facts,
  configuration,
}: {
  facts: V2Facts;
  configuration: Parameters<typeof addressExplorerUrl>[0];
}) {
  const link = (address: string) => {
    const href = addressExplorerUrl(configuration, address as `0x${string}`);
    return href ? (
      <a className="mono" href={href} target="_blank" rel="noreferrer" title={address}>
        {address}
      </a>
    ) : (
      <span className="mono" title={address}>
        {address}
      </span>
    );
  };
  return (
    <section className="token-v2-facts" aria-labelledby="v2-facts-title">
      <div className="token-section-head">
        <h2 id="v2-facts-title">Engine v2</h2>
      </div>
      <dl className="token-summary-grid">
        <div>
          <dt>Anti-snipe tax</dt>
          <dd title={facts.antiSnipe}>{facts.antiSnipe}</dd>
        </div>
        <div>
          <dt>Tax ends</dt>
          <dd>
            <time dateTime={facts.antiSnipeEndsAt}>{facts.antiSnipeEndsAt}</time>
          </dd>
        </div>
        <div>
          <dt>Pool fee creator share</dt>
          <dd>{facts.lpCreatorShare}</dd>
        </div>
        <div>
          <dt>Fee recipient</dt>
          <dd>{link(facts.feeRecipient)}</dd>
        </div>
        {facts.pendingFeeRecipient ? (
          <div>
            <dt>Pending recipient</dt>
            <dd>{link(facts.pendingFeeRecipient)}</dd>
          </div>
        ) : null}
        {facts.pool ? (
          <>
            <div>
              <dt>Pool id</dt>
              <dd className="mono" title={facts.pool.poolId}>
                {facts.pool.poolId}
              </dd>
            </div>
            <div>
              <dt>Pool hook</dt>
              <dd>{link(facts.pool.hook)}</dd>
            </div>
          </>
        ) : null}
        {facts.lockerOwed.map((owed) => (
          <div key={owed.account}>
            <dt title={owed.account}>Locker owes {owed.account.slice(0, 10)}…</dt>
            <dd>
              {amount(owed.eth)} + {safeTokenAmount(owed.token)} tokens
            </dd>
          </div>
        ))}
      </dl>
      {facts.pool ? (
        <p className="token-aside-note">
          Pool liquidity is not indexed yet; price comes from the indexed spot price.
        </p>
      ) : null}
    </section>
  );
}

/**
 * Engine-1 launch facts: what LaunchToken and BondingCurveV1 guarantee by construction, with
 * the curve and pair as evidence links, and the pair the curve seeds at graduation.
 */
export function LaunchFactsPanel({
  token,
  facts,
  configuration,
}: {
  token: {
    curve: string;
    pair: string;
    phase: string;
    total_supply: string;
    curve_tokens: string;
    lp_tokens: string;
  };
  facts: LaunchFacts;
  configuration: Parameters<typeof addressExplorerUrl>[0];
}) {
  const graduated = token.phase.toLowerCase() === "graduated";
  const link = (address: string) => {
    const href = addressExplorerUrl(configuration, address as `0x${string}`);
    return href ? (
      <a className="mono" href={href} target="_blank" rel="noreferrer" title={address}>
        {address}
      </a>
    ) : (
      <span className="mono" title={address}>
        {address}
      </span>
    );
  };
  const openingPrice = formatSignificantPrice(facts.graduation.openingPriceWad);
  return (
    <section className="token-launch-facts" aria-labelledby="launch-facts-title">
      <div className="token-section-head">
        <h2 id="launch-facts-title">Launch facts</h2>
      </div>
      <dl className="token-summary-grid">
        <div>
          <dt>Fixed supply</dt>
          <dd>{safeTokenAmount(token.total_supply)}</dd>
        </div>
        <div>
          <dt>On the curve</dt>
          <dd>{safeTokenAmount(token.curve_tokens)}</dd>
        </div>
        <div>
          <dt>Seeded into the pool</dt>
          <dd>{safeTokenAmount(token.lp_tokens)}</dd>
        </div>
        <div>
          <dt>Curve trade fee</dt>
          <dd title={facts.feeSplit}>{facts.tradeFee}</dd>
        </div>
        <div>
          <dt>Pool opens at</dt>
          <dd>{openingPrice === null ? "Unavailable" : `${openingPrice} ETH`}</dd>
        </div>
        <div>
          <dt>Market cap at graduation</dt>
          <dd>
            {amount(facts.graduation.marketCapWei)}
            <UsdLine wei={facts.graduation.marketCapWei} />
          </dd>
        </div>
        {facts.graduation.remainingEthWei !== null ? (
          <div>
            <dt>Left to graduate</dt>
            <dd>{amount(facts.graduation.remainingEthWei)}</dd>
          </div>
        ) : null}
        <div>
          <dt>Curve</dt>
          <dd>{link(token.curve)}</dd>
        </div>
        <div>
          <dt>Pool</dt>
          <dd>{link(token.pair)}</dd>
        </div>
      </dl>
      <ul className="token-launch-guarantees">
        <li>The whole supply was minted once at launch; the token has no mint function.</li>
        <li>The token contract has no owner, no transfer tax and no blacklist.</li>
        <li>
          {graduated
            ? "Until graduation only the curve could move tokens to or from itself or the pool, so nobody could seed the pool early."
            : "Until graduation only the curve can move tokens to or from itself or the pool, so nobody can seed the pool early."}
        </li>
        <li>
          {graduated
            ? "At graduation the pool's LP tokens were minted to 0x…dEaD, so the liquidity can never be withdrawn."
            : "At graduation the pool's LP tokens are minted to 0x…dEaD, so the liquidity can never be withdrawn."}
        </li>
        <li>Curve fee split: {facts.feeSplit}.</li>
      </ul>
    </section>
  );
}

export function Metric({ label, value, eth }: { label: string; value: string; eth?: string }) {
  return (
    <div>
      <dt>{label}</dt>
      <dd>
        {value}
        {eth ? <UsdLine wei={eth} /> : null}
      </dd>
    </div>
  );
}
