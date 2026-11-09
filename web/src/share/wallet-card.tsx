/**
 * Wallet share-card rendering (1200x630 PNG through next/og). Kept out of the route file so the
 * wallet-kind card can be rendered in a unit test; the route keeps its segment config.
 */
import { ImageResponse } from "next/og";
import { formatDisplayAmount } from "@/amounts";
import { formatSignedEth } from "@/portfolio/wallet-model";
import { shortAddress } from "@/token/address";
import { CARD_REVALIDATE_SECONDS } from "./card-data";
import type { WalletCardData } from "./wallet-card-data";

export const WALLET_CARD_SIZE = { width: 1200, height: 630 };
export const WALLET_CARD_CONTENT_TYPE = "image/png";
/**
 * Downstream (CDN) lifetime of a generic card served because the API failed, so a recovered API
 * shows up within half a minute. Next's own in-process ISR cache still keeps the route's 300 s
 * `revalidate`: a route handler cannot shorten it per response, so this header only bounds caches
 * in front of the web server.
 */
export const GENERIC_FAILURE_MAX_AGE_SECONDS = 30;

const palette = {
  bg: "#0a0a0b",
  text: "#f4f4f5",
  muted: "#a1a1aa",
  accent: "#6ee7b7",
  loss: "#fca5a5",
};

function Mark() {
  return (
    <div style={{ display: "flex", fontSize: 32, color: palette.muted, letterSpacing: 2 }}>
      LAUNCHPAD
    </div>
  );
}

function GenericCard() {
  return (
    <div
      style={{
        width: "100%",
        height: "100%",
        display: "flex",
        flexDirection: "column",
        alignItems: "center",
        justifyContent: "center",
        background: palette.bg,
        color: palette.text,
      }}
    >
      <div style={{ display: "flex", fontSize: 96, fontWeight: 700 }}>Launchpad</div>
      <div style={{ display: "flex", fontSize: 36, color: palette.muted, marginTop: 16 }}>
        Fixed-supply token launches, from curve to liquidity
      </div>
    </div>
  );
}

/** Card text is limited to font-covered glyphs, so the signed ETH string uses an ASCII minus. */
function pnlText(wei: string) {
  return formatSignedEth(wei).replace("−", "-");
}

function valueText(wei: string) {
  return `${formatDisplayAmount(BigInt(wei), 18, 4)} ETH`;
}

function WalletCard({ card }: { card: Extract<WalletCardData, { kind: "wallet" }> }) {
  const pnl = pnlText(card.realizedPnlWei);
  const loss = pnl.startsWith("-");
  return (
    <div
      style={{
        width: "100%",
        height: "100%",
        display: "flex",
        flexDirection: "column",
        justifyContent: "space-between",
        background: palette.bg,
        color: palette.text,
        padding: 64,
      }}
    >
      <div style={{ display: "flex", flexDirection: "column" }}>
        <div style={{ display: "flex", fontSize: 28, color: palette.muted }}>Wallet</div>
        <div style={{ display: "flex", fontSize: 72, fontWeight: 700 }}>
          {shortAddress(card.address)}
        </div>
      </div>
      <div style={{ display: "flex", flexDirection: "column" }}>
        <div style={{ display: "flex", fontSize: 28, color: palette.muted }}>Realized PnL</div>
        <div
          style={{
            display: "flex",
            fontSize: 72,
            fontWeight: 700,
            color: loss ? palette.loss : palette.accent,
          }}
        >
          {pnl}
        </div>
        <div style={{ display: "flex", fontSize: 28, color: palette.muted, marginTop: 24 }}>
          Portfolio value
        </div>
        <div style={{ display: "flex", fontSize: 48, fontWeight: 700 }}>
          {valueText(card.portfolioValueWei)}
        </div>
      </div>
      <div style={{ display: "flex", alignItems: "center" }}>
        <div style={{ display: "flex", fontSize: 36, color: palette.accent }}>
          {card.topSymbols.map((symbol) => `$${symbol}`).join("  ")}
        </div>
        <div style={{ display: "flex", flexGrow: 1, justifyContent: "flex-end" }}>
          <Mark />
        </div>
      </div>
    </div>
  );
}

/** Renders the card and buffers it, so a render failure surfaces here, not mid-stream. */
export async function renderWalletCard(card: WalletCardData): Promise<Response> {
  // The default bundled font is used. Card text is restricted to glyphs that font covers (see
  // cardText), so satori never loads a fallback font or emoji asset from a third-party host.
  const response = new ImageResponse(
    card.kind === "wallet" ? <WalletCard card={card} /> : <GenericCard />,
    { ...WALLET_CARD_SIZE },
  );
  // ImageResponse renders lazily; buffer it so a render failure falls back to the generic card
  // instead of a broken stream.
  const bytes = await response.arrayBuffer();
  const cacheControl =
    card.kind === "generic" && card.apiFailure
      ? `public, s-maxage=${GENERIC_FAILURE_MAX_AGE_SECONDS}`
      : `public, s-maxage=${CARD_REVALIDATE_SECONDS}, stale-while-revalidate=${CARD_REVALIDATE_SECONDS}`;
  return new Response(bytes, {
    headers: { "Content-Type": WALLET_CARD_CONTENT_TYPE, "Cache-Control": cacheControl },
  });
}
