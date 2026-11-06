import { ImageResponse } from "next/og";
import { loadCardData, type CardData } from "@/share/card-data";

export const alt = "Token on Launchpad";
export const size = { width: 1200, height: 630 };
export const contentType = "image/png";
export const revalidate = 300;

const palette = {
  bg: "#0a0a0b",
  panel: "#16161a",
  text: "#f4f4f5",
  muted: "#a1a1aa",
  accent: "#6ee7b7",
  track: "#2a2a30",
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

function TokenCard({ card }: { card: Extract<CardData, { kind: "token" }> }) {
  const graduated = card.phase === "graduated";
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
      <div style={{ display: "flex", alignItems: "center" }}>
        {card.imageDataUri ? (
          // eslint-disable-next-line @next/next/no-img-element
          <img
            src={card.imageDataUri}
            width={200}
            height={200}
            alt=""
            style={{ borderRadius: 24, objectFit: "cover" }}
          />
        ) : (
          <div
            style={{
              display: "flex",
              width: 200,
              height: 200,
              borderRadius: 24,
              background: palette.panel,
              alignItems: "center",
              justifyContent: "center",
              fontSize: 72,
              color: palette.accent,
            }}
          >
            {(card.symbol || card.name).slice(0, 1).toUpperCase()}
          </div>
        )}
        <div style={{ display: "flex", flexDirection: "column", marginLeft: 48 }}>
          <div style={{ display: "flex", fontSize: 64, fontWeight: 700 }}>{card.name}</div>
          {card.symbol ? (
            <div style={{ display: "flex", fontSize: 44, color: palette.accent, marginTop: 8 }}>
              {`$${card.symbol}`}
            </div>
          ) : null}
        </div>
      </div>
      <div style={{ display: "flex", flexDirection: "column" }}>
        <div style={{ display: "flex", fontSize: 28, color: palette.muted }}>Market cap</div>
        <div style={{ display: "flex", alignItems: "baseline" }}>
          <div style={{ display: "flex", fontSize: 56, fontWeight: 700 }}>
            {`${card.marketCapEth} ETH`}
          </div>
          {card.marketCapUsd ? (
            <div style={{ display: "flex", fontSize: 36, color: palette.muted, marginLeft: 24 }}>
              {card.marketCapUsd}
            </div>
          ) : null}
        </div>
        <div style={{ display: "flex", alignItems: "center", marginTop: 24 }}>
          {graduated ? (
            <div
              style={{
                display: "flex",
                padding: "8px 24px",
                borderRadius: 999,
                background: palette.accent,
                color: palette.bg,
                fontSize: 30,
                fontWeight: 700,
              }}
            >
              Graduated
            </div>
          ) : (
            <div style={{ display: "flex", alignItems: "center" }}>
              <div
                style={{
                  display: "flex",
                  width: 600,
                  height: 20,
                  borderRadius: 10,
                  background: palette.track,
                }}
              >
                <div
                  style={{
                    display: "flex",
                    width: `${card.progressBps / 100}%`,
                    height: 20,
                    borderRadius: 10,
                    background: palette.accent,
                  }}
                />
              </div>
              <div style={{ display: "flex", fontSize: 28, color: palette.muted, marginLeft: 24 }}>
                {`${(card.progressBps / 100).toFixed(1)}% to graduation`}
              </div>
            </div>
          )}
          <div style={{ display: "flex", flexGrow: 1, justifyContent: "flex-end" }}>
            <Mark />
          </div>
        </div>
      </div>
    </div>
  );
}

export default async function Image({ params }: { params: Promise<{ address: string }> }) {
  const { address } = await params;
  const card = await loadCardData(address);
  // The default bundled font is used; the renderer makes no third-party request.
  return new ImageResponse(card.kind === "token" ? <TokenCard card={card} /> : <GenericCard />, {
    ...size,
  });
}
