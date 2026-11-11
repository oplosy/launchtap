import { ArrowUpRight } from "@phosphor-icons/react";
import type { components } from "@/api/generated";
import { SafeExternalLink } from "@/components/primitives";
import { parseTokenAddress, shortAddress } from "./address";
import { launchFacts, type SupplyShare } from "./launch-facts";

/** Mirrors `BondingCurveV1.LP_BURN_ADDRESS`: the graduation LP position is minted here. */
const LP_BURN_ADDRESS = "0x000000000000000000000000000000000000dEaD";

type Token = Pick<
  components["schemas"]["TokenDetailDTO"],
  | "address"
  | "creator"
  | "curve"
  | "pair"
  | "engine_version"
  | "total_supply"
  | "curve_tokens"
  | "lp_tokens"
  | "trade_fee_bps"
  | "protocol_share_bps"
  | "graduation_eth"
  | "phase"
>;

const UNAVAILABLE = "Unavailable";

export function LaunchFactsPanel({
  token,
  explorerFor,
}: {
  token: Token;
  explorerFor: (address: `0x${string}`) => string | null;
}) {
  const facts = launchFacts(token);
  const addresses: { label: string; value: string }[] = [
    { label: "Token", value: token.address },
    { label: "Bonding curve", value: token.curve },
    { label: "Uniswap V2 pair", value: token.pair },
    { label: "Creator", value: token.creator },
    { label: "LP recipient (burn)", value: LP_BURN_ADDRESS },
  ];

  return (
    <section className="launch-facts" aria-labelledby="launch-facts-title">
      <div className="token-section-head">
        <div>
          <h2 id="launch-facts-title">Launch facts</h2>
          <p className="section-description">
            Read from the launch contracts. Automated facts, not an audit.
          </p>
        </div>
      </div>
      <dl className="token-summary-grid">
        <Fact label="Total supply (fixed)" value={facts.totalSupply} />
        <Fact label="Sold on the curve" value={shareText(facts.curveShare)} />
        <Fact label="Seeded into the pool" value={shareText(facts.lpShare)} />
        <Fact label="Curve trade fee" value={facts.tradeFee} />
        <Fact
          label="Protocol / creator"
          value={
            facts.protocolFee && facts.creatorFee
              ? `${facts.protocolFee} / ${facts.creatorFee}`
              : null
          }
        />
        <Fact
          label="Graduates at"
          value={facts.graduationEth === null ? null : `${facts.graduationEth} ETH`}
        />
      </dl>
      <ul className="launch-facts-rules">
        <li>{facts.transferRule}</li>
        <li>{facts.pauseRule}</li>
        <li>
          At graduation the liquidity position is minted to the burn address, and LaunchTap takes no
          fee on pool trades.
        </li>
      </ul>
      <dl className="launch-facts-addresses">
        {addresses.map(({ label, value }) => (
          <div key={label}>
            <dt>{label}</dt>
            <dd>
              <AddressLink address={value} explorerFor={explorerFor} />
            </dd>
          </div>
        ))}
        <div>
          <dt>Engine</dt>
          <dd>{facts.engineVersion ?? UNAVAILABLE}</dd>
        </div>
      </dl>
    </section>
  );
}

function shareText(share: SupplyShare): string | null {
  if (share.amount === null) return null;
  return share.percent === null ? share.amount : `${share.amount} (${share.percent})`;
}

function Fact({ label, value }: { label: string; value: string | null }) {
  return (
    <div>
      <dt>{label}</dt>
      <dd>{value ?? UNAVAILABLE}</dd>
    </div>
  );
}

function AddressLink({
  address,
  explorerFor,
}: {
  address: string;
  explorerFor: (address: `0x${string}`) => string | null;
}) {
  const parsed = parseTokenAddress(address);
  if (!parsed) return <span>{UNAVAILABLE}</span>;
  const href = explorerFor(parsed);
  const text = <span className="mono">{shortAddress(address)}</span>;
  if (!href) return <span title={address}>{text}</span>;
  return (
    <SafeExternalLink href={href} className="launch-facts-link">
      {text}
      <ArrowUpRight size={13} aria-hidden="true" />
    </SafeExternalLink>
  );
}
