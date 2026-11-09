import { ImageResponse } from "next/og";
import { formatDisplayAmount } from "@/amounts";
import { formatSignedEth } from "@/portfolio/wallet-model";
import { CARD_REVALIDATE_SECONDS } from "@/share/card-data";
import { loadWalletCardData, type WalletCardData } from "@/share/wallet-card-data";
import { shortAddress } from "@/token/address";

export const alt = "Wallet on Launchpad";
export const size = { width: 1200, height: 630 };
export const contentType = "image/png";
export const revalidate = 300;

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

// An empty list makes this an ISR route: each address renders on first request and is then served
// from the route cache for `revalidate` seconds.
export function generateStaticParams() {
  return [];
}

async function render(card: WalletCardData) {
  // The default bundled font is used. Card text is restricted to glyphs that font covers (see
  // cardText), so satori never loads a fallback font or emoji asset from a third-party host.
  const response = new ImageResponse(
    card.kind === "wallet" ? <WalletCard card={card} /> : <GenericCard />,
    { ...size },
  );
  // ImageResponse renders lazily; buffer it so a render failure falls back to the generic card
  // instead of a broken stream.
  const bytes = await response.arrayBuffer();
  return new Response(bytes, {
    headers: {
      "Content-Type": contentType,
      "Cache-Control": `public, s-maxage=${CARD_REVALIDATE_SECONDS}, stale-while-revalidate=${CARD_REVALIDATE_SECONDS}`,
    },
  });
}

export default async function Image({ params }: { params: Promise<{ address: string }> }) {
  const { address } = await params;
  const card = await loadWalletCardData(address);
  try {
    return await render(card);
  } catch (cause) {
    console.error("wallet share card: render failed, using the generic card", cause);
    return render({ kind: "generic" });
  }
}
