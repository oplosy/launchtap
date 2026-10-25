import { ArrowSquareOut } from "@/components/icons";
import { SafeExternalLink } from "@/components/primitives";
import { publicConfiguration } from "@/config/public";
import { DocsNav } from "@/docs/docs-nav";

const SECTIONS = [
  { id: "flow", label: "Launch flow" },
  { id: "curve", label: "Curve and graduation" },
  { id: "economics", label: "Fees and economics" },
  { id: "risk", label: "Risks and custody" },
  { id: "contracts", label: "Contracts and finality" },
] as const;

export default function DocsPage() {
  const configuration = publicConfiguration();
  const explorer = configuration.deployment?.explorerBase ?? null;
  return (
    <div className="page-stack docs-page">
      <section className="page-hero">
        <div>
          <div className="section-kicker">Docs</div>
          <h1>Understand the route.</h1>
          <p className="hero-summary">
            A fixed-supply, non-custodial launch flow on Robinhood Chain. Read the contract rules
            before signing anything.
          </p>
        </div>
      </section>
      <div className="docs-layout">
        <DocsNav sections={SECTIONS} />
        <article className="docs-article">
          <section id="flow">
            <h2>Launch flow</h2>
            <p>
              A creator submits a fixed-supply token launch through the reviewed factory. The curve
              phase accepts buys and sells, and the selected wallet signs every transaction. After
              graduation, the route moves to the reviewed Uniswap v2 pair.
            </p>
            <p>
              Launchpad reads indexed state from the API and uses RPC for live quotes, simulation,
              and wallet writes. Mined is not the same as indexed, safe, or finalized.
            </p>
          </section>
          <section id="curve">
            <h2>Curve and graduation</h2>
            <p>
              V1 uses a 1,000,000,000 token total supply: 800,000,000 tokens are allocated to the
              curve and 200,000,000 to the post-graduation liquidity position. Graduation occurs at
              4.2 ETH of real curve ETH. The initial LP position is burned after routing to the
              Uniswap v2 pool.
            </p>
            <p>
              These are protocol parameters, not a promise that a token will graduate, retain
              liquidity, or produce a return.
            </p>
          </section>
          <section id="economics">
            <h2>Fees and economics</h2>
            <dl className="docs-facts">
              <div>
                <dt>V1 launch fee</dt>
                <dd>0.0005 ETH, charged by the reviewed factory</dd>
              </div>
              <div>
                <dt>Curve trade fee</dt>
                <dd>1% of the gross trade amount</dd>
              </div>
              <div>
                <dt>Fee split</dt>
                <dd>0.5% protocol · 0.5% creator</dd>
              </div>
              <div>
                <dt>Launch supply</dt>
                <dd>1B tokens, fixed supply</dd>
              </div>
              <div>
                <dt>Currency display</dt>
                <dd>ETH-native; no USD enrichment is available</dd>
              </div>
            </dl>
            <p>
              Quotes are informational until the contract simulation and signed transaction execute.
              Slippage, deadlines, gas, and refunds must be reviewed in the wallet.
            </p>
          </section>
          <section id="risk">
            <h2>Risks and non-custody</h2>
            <p>
              Launchpad never takes custody of ETH or tokens. A wallet signature authorizes an
              on-chain transaction; it does not guarantee inclusion or finality. Smart-contract
              bugs, reorgs, RPC failures, malicious metadata, price volatility, loss of keys, and
              irreversible transfers are possible.
            </p>
            <p>
              Do not treat market-cap, volume, liquidity, or graduation indicators as investment
              advice or a promise of liquidity or returns. Verify the chain, contract address,
              calldata, minimum output, fee, deadline, and recipient before signing.
            </p>
          </section>
          <section id="contracts">
            <h2>Contracts and finality</h2>
            <p>
              Indexed reads are bound to a canonical chain snapshot with block number, block hash,
              and a finality label. Reorgs can invalidate a cursor or replace derived values; the
              API then requires a fresh snapshot.
            </p>
            <p>
              Use the explorer to inspect the deployed addresses and transaction receipts. The
              browser fails closed when a reviewed deployment manifest or public configuration is
              missing.
            </p>
            {explorer ? (
              <SafeExternalLink href={explorer} className="docs-explorer">
                Open the reviewed deployment explorer <ArrowSquareOut size={14} />
              </SafeExternalLink>
            ) : (
              <p className="discovery-notice">
                Explorer links are unavailable until a reviewed deployment is configured.
              </p>
            )}
          </section>
        </article>
      </div>
    </div>
  );
}
