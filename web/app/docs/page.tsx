import { ArrowSquareOut } from "@/components/icons";
import { SafeExternalLink } from "@/components/primitives";
import { publicConfiguration } from "@/config/public";
import { DocsNav } from "@/docs/docs-nav";

// Every number on this page mirrors the engine v1 launch parameters in
// docs/specs/2026-09-01-contract-core-design.md §3 and the curve math in CurveMath.sol.
const SECTIONS = [
  { id: "overview", label: "How it works" },
  { id: "flow", label: "Launching a token" },
  { id: "trading", label: "Trading on the curve" },
  { id: "curve", label: "The bonding curve" },
  { id: "graduation", label: "Graduation" },
  { id: "economics", label: "Fees and economics" },
  { id: "engine-v2", label: "Engine v2 rules" },
  { id: "token", label: "Token rules" },
  { id: "governance", label: "Pauses and governance" },
  { id: "contracts", label: "Data and finality" },
  { id: "risk", label: "Risks and custody" },
] as const;

export default function DocsPage() {
  const configuration = publicConfiguration();
  const explorer = configuration.deployment?.explorerBase ?? null;
  return (
    <div className="page-stack docs-page">
      <h1 className="sr-only">Docs</h1>
      <div className="docs-layout">
        <DocsNav sections={SECTIONS} />
        <article className="docs-article">
          <section id="overview">
            <h2>How it works</h2>
            <p>
              Every token starts its life on a bonding curve: a contract that sells and buys the
              token at a price set by a formula, not by an order book. The price rises as tokens are
              bought and falls as they are sold back. There is no presale, no team allocation, and
              no way to mint more tokens later.
            </p>
            <p>
              When buyers have put 4.2 ETH into the curve, the token graduates. The curve hands its
              ETH and a reserved block of tokens to a Uniswap v2 pool, the initial liquidity
              position is sent to a burn address, and from then on the token trades on Uniswap like
              any other ERC-20.
            </p>
            <ol className="docs-steps">
              <li>
                <strong>Launch.</strong> A creator deploys the token and its curve in one
                transaction.
              </li>
              <li>
                <strong>Curve phase.</strong> Anyone buys from and sells back to the curve.
              </li>
              <li>
                <strong>Graduation.</strong> The buy that reaches 4.2 ETH moves liquidity to
                Uniswap.
              </li>
              <li>
                <strong>Open market.</strong> The token trades on its Uniswap v2 pool.
              </li>
            </ol>
          </section>

          <section id="flow">
            <h2>Launching a token</h2>
            <p>
              A launch is a single transaction to the factory contract, signed by the creator&apos;s
              wallet. The factory creates the token, mints the whole supply to a new curve contract,
              and resolves the token&apos;s canonical Uniswap v2 pair up front, so the pool address
              is known before the first trade.
            </p>
            <h3>What you provide</h3>
            <ul className="docs-list">
              <li>
                <strong>Name and ticker.</strong> Written into the token contract and cannot be
                changed after launch.
              </li>
              <li>
                <strong>Image and links.</strong> An HTTPS image URL, an X profile, and a Telegram
                link. These are off-chain metadata; the creator can change them and add a
                description later from the token page.
              </li>
              <li>
                <strong>Developer buy (optional).</strong> ETH spent on the creator&apos;s own
                tokens in the launch transaction itself, before anyone else can trade. It is capped
                at 1% of supply (10,000,000 tokens), which costs about 0.0134 ETH at the starting
                price.
              </li>
            </ul>
            <h3>What it costs</h3>
            <p>
              The launch fee is 0.0005 ETH. The wallet sends exactly the launch fee plus the
              developer buy amount; the factory rejects any other value, so an accidental overpay
              cannot happen. Gas is paid on top.
            </p>
            <h3>Editing after launch</h3>
            <p>
              Only metadata can change. The creator signs in with the creating wallet and edits the
              image, description, and links from the token page; the server checks that the signer
              is the on-chain creator. Supply, curve parameters, fees, and the pool address are
              fixed at launch.
            </p>
          </section>

          <section id="trading">
            <h2>Trading on the curve</h2>
            <p>
              Before graduation, the curve contract is the only market. A buy sends ETH and receives
              tokens; a sell sends tokens and receives ETH. Each trade is priced against the curve
              state at the moment it executes, not when you opened the form.
            </p>
            <h3>Slippage and deadlines</h3>
            <p>
              Other trades can land before yours and move the price. Every trade therefore carries a
              minimum output (minimum tokens for a buy, minimum ETH for a sell) derived from your
              slippage setting, and a deadline 15 minutes after you sign. The contract reverts the
              trade if it would pay out less than the minimum or if the deadline has passed. The
              default slippage is 5%, with 1% and 10% presets.
            </p>
            <h3>Selling</h3>
            <p>
              You can only sell back tokens the curve has actually sold, so the curve&apos;s ETH
              always covers every holder. Sells stay open even if trading is paused for an incident.
            </p>
            <h3>Refunds</h3>
            <p>
              If a buy would go past the graduation target, the curve takes only the ETH needed to
              reach it and returns the rest in the same transaction. If that transfer fails (for
              example, a contract wallet that rejects ETH), the amount is credited to you and can be
              claimed later under Claims and refunds on your profile. Failed sell payouts are
              handled the same way.
            </p>
          </section>

          <section id="curve">
            <h2>The bonding curve</h2>
            <p>
              The curve is a constant-product market, like a Uniswap pool, but its starting ETH
              reserve is virtual: an accounting value of 1.4 ETH that nobody deposited. That virtual
              reserve sets the opening price without anyone seeding liquidity.
            </p>
            <pre className="docs-formula">
              <code>
                {
                  "x · y = K\nx = virtual ETH reserve  (starts at 1.4 ETH)\ny = virtual token reserve  (starts at ~1.0667B)\nprice = x / y"
                }
              </code>
            </pre>
            <p>
              A buy adds ETH (after the fee) to x and takes tokens out of y so that x · y stays
              equal to K. A sell does the reverse. Rounding always favours the curve, so it never
              pays out more than the formula allows.
            </p>
            <h3>Supply</h3>
            <dl className="docs-facts">
              <div>
                <dt>Total supply</dt>
                <dd>1,000,000,000 tokens, 18 decimals</dd>
              </div>
              <div>
                <dt>Sold on the curve</dt>
                <dd>800,000,000 tokens (80%)</dd>
              </div>
              <div>
                <dt>Reserved for the pool</dt>
                <dd>200,000,000 tokens (20%)</dd>
              </div>
            </dl>
            <h3>Price along the curve</h3>
            <dl className="docs-facts">
              <div>
                <dt>Starting price</dt>
                <dd>≈ 0.0000000013 ETH per token (1.31 ETH fully diluted)</dd>
              </div>
              <div>
                <dt>Price at graduation</dt>
                <dd>≈ 0.000000021 ETH per token (21 ETH fully diluted), 16× the start</dd>
              </div>
              <div>
                <dt>First 1 ETH buys</dt>
                <dd>≈ 442M tokens, over half of the curve&apos;s allocation</dd>
              </div>
            </dl>
            <p>
              Early buys get far more tokens per ETH than late ones. The figures above assume a
              fresh curve and include the 1% trade fee.
            </p>
          </section>

          <section id="graduation">
            <h2>Graduation</h2>
            <p>
              Graduation happens inside the buy that brings the curve to exactly 4.2 ETH of real
              ETH, which is also the moment all 800,000,000 curve tokens are sold. No one can
              trigger it early, delay it, or force it, including the protocol administrators.
            </p>
            <h3>What happens in that transaction</h3>
            <ol className="docs-steps">
              <li>The curve closes and the token is marked graduated.</li>
              <li>The 4.2 ETH is wrapped into WETH.</li>
              <li>The 200,000,000 reserved tokens and 4.2 WETH go to the Uniswap v2 pair.</li>
              <li>
                The pool mints its LP tokens straight to the burn address{" "}
                <span className="mono">0x…dEaD</span>. No one, including the creator, can withdraw
                that liquidity.
              </li>
            </ol>
            <p>
              The pool opens at 4.2 ETH for 200,000,000 tokens, the same price as the curve&apos;s
              final price, so there is no jump between the two markets. If someone sends WETH to the
              empty pool beforehand, it stays there as extra pool ETH and can only raise the opening
              price.
            </p>
            <p>
              &ldquo;LP burned&rdquo; refers to this initial position only. Anyone can add their own
              liquidity to the pool later and keeps the LP tokens for it.
            </p>
          </section>

          <section id="economics">
            <h2>Fees and economics</h2>
            <dl className="docs-facts">
              <div>
                <dt>Launch fee</dt>
                <dd>0.0005 ETH per launch, paid to the protocol treasury</dd>
              </div>
              <div>
                <dt>Curve trade fee</dt>
                <dd>1% of the ETH amount of every buy and sell</dd>
              </div>
              <div>
                <dt>Fee split</dt>
                <dd>0.5% to the creator · 0.5% to the protocol</dd>
              </div>
              <div>
                <dt>After graduation</dt>
                <dd>No protocol fee; only Uniswap v2&apos;s own 0.3% pool fee applies</dd>
              </div>
              <div>
                <dt>Currency display</dt>
                <dd>ETH-native; no USD enrichment is available</dd>
              </div>
            </dl>
            <p>
              On a buy the fee is taken from the ETH you send before it reaches the curve; on a sell
              it is taken from the ETH the curve pays out. Creator fees collect in the token&apos;s
              curve contract and the creator claims them under Claims and refunds on their profile.
              Claims keep working while trading is paused and after graduation.
            </p>
            <p>
              Quotes are informational until the contract simulation and signed transaction execute.
              Review the minimum output, deadline, and gas in the wallet before confirming.
            </p>
          </section>

          <section id="engine-v2">
            <h2>Engine v2 rules</h2>
            <p>
              A deployment launches through one engine, shown as a badge on the create page. Engine
              v2 keeps the same curve and graduation threshold and changes what happens around them.
            </p>
            <dl className="docs-facts">
              <div>
                <dt>Anti-snipe tax</dt>
                <dd>
                  A buy tax that starts high at launch and decays to zero over a short window. The
                  create page reads the exact profile the next launch snapshots; the token page
                  shows the snapshot and the time left.
                </dd>
              </div>
              <div>
                <dt>Developer buy</dt>
                <dd>The buy made in the launch transaction is exempt from the anti-snipe tax</dd>
              </div>
              <div>
                <dt>Pool</dt>
                <dd>Graduation opens a Uniswap V4 ETH pool with a 1% fee</dd>
              </div>
              <div>
                <dt>Pool fee split</dt>
                <dd>
                  50% to the creator&apos;s fee recipient · 50% to the protocol by default; the
                  create page shows the exact split
                </dd>
              </div>
              <div>
                <dt>Liquidity</dt>
                <dd>The graduation position is locked forever in the fee locker</dd>
              </div>
            </dl>
            <p>
              The tax only decreases, so a buy mined later than quoted never receives less than its
              minimum output. Anyone can add liquidity to the V4 pool; that liquidity earns its
              share of the pool fee, which dilutes the locked position&apos;s share.
            </p>
            <p>
              Fee collection is permissionless: claiming from the locker collects the pool fees
              first, so no separate step is needed. The creator is the launch identity; the fee
              recipient is the payout address. Changing the recipient takes two steps: the current
              recipient proposes an address, and the change happens only when that address accepts.
              Fees already credited stay with the account they were credited to.
            </p>
          </section>

          <section id="token">
            <h2>Token rules</h2>
            <p>
              Every token is a standard ERC-20 with 18 decimals and a fixed supply. The full supply
              is minted once, when the token is created, and the contract has no mint or burn
              function afterwards.
            </p>
            <p>
              One restriction applies before graduation: tokens can move freely between wallets, but
              only the curve can move tokens into or out of the curve and the Uniswap pair. This
              stops anyone from seeding the pool early or corrupting the curve&apos;s inventory. The
              restriction lifts permanently at graduation.
            </p>
          </section>

          <section id="governance">
            <h2>Pauses and governance</h2>
            <p>
              A multisig can pause new launches and pause curve buys during an incident. Pausing
              buys never blocks sells, so holders can always exit to the curve, and neither pause
              blocks fee or refund claims.
            </p>
            <p>
              Changes to defaults for future launches, such as fees or treasury, go through a
              timelock. Nothing can change an existing launch: its parameters, curve, pool, creator,
              and fee recipients are fixed when it is created. There is no upgrade, rescue, token
              sweep, or reserve withdrawal function, and no one can force a graduation or redirect
              its liquidity.
            </p>
          </section>

          <section id="contracts">
            <h2>Data and finality</h2>
            <p>
              Lists, charts, and token pages come from Launchpad&apos;s own indexer. Live quotes,
              simulation, and wallet transactions go straight to the chain over RPC.
            </p>
            <p>
              Indexed reads carry a block number, block hash, and a finality label: provisional,
              safe, or finalized. A mined transaction is not yet safe or finalized, and a chain
              reorganisation can replace recent data; when that happens the page reloads from a
              fresh snapshot.
            </p>
            <p>
              Use the explorer to inspect the deployed addresses and transaction receipts. The site
              refuses to trade when a reviewed deployment manifest or public configuration is
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

          <section id="risk">
            <h2>Risks and custody</h2>
            <p>
              Launchpad never holds your ETH or tokens. Your wallet signs every transaction, and the
              contracts hold the curve funds. A signature authorizes a transaction; it does not
              guarantee that it is included or final.
            </p>
            <ul className="docs-list">
              <li>Most tokens never graduate, and a token can lose most of its value quickly.</li>
              <li>Anyone can launch a token with any name or image, including copies of others.</li>
              <li>Smart-contract bugs, reorgs, and RPC outages are possible.</li>
              <li>Lost keys and on-chain transfers cannot be reversed.</li>
            </ul>
            <p>
              Market cap, volume, and graduation progress are not investment advice or a promise of
              liquidity or returns. Before signing, check the chain, contract address, amount,
              minimum output, fee, deadline, and recipient in your wallet.
            </p>
          </section>
        </article>
      </div>
    </div>
  );
}
