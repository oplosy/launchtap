// Fixture API for server-side fetches (share cards, token metadata). Playwright's page.route
// only sees browser traffic, so the Next server needs a real HTTP origin to read from.
// Shapes follow backend/openapi/v1.json (TokenDetailDTO, EthUsdBody); phase is "curve" or "graduated".
//
// Address conventions: ...0001 is the stable "Fixture Route" token, ...0002 has an emoji name,
// ...0003 has an all-emoji name and symbol, ...0004 has a long unbroken name, ...00ff does not
// exist. Any other address is a generic token named after its last four digits.
// GET /__calls returns per-path request counts; POST /__reset clears them.
import http from "node:http";

const port = Number(process.env.TASK7_MOCK_API_PORT ?? "3418");
const wad = (value) => `${BigInt(value) * 10n ** 18n}`;
const png = Buffer.from(
  "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==",
  "base64",
);
const calls = new Map();

function names(address) {
  const tail = address.slice(-4);
  if (tail === "0001") return ["Fixture Route", "FIX"];
  if (tail === "0002") return ["Rocket \u{1F680} 日本語", "RKT"];
  if (tail === "0003") return ["\u{1F680}\u{1F680}", "\u{1F525}"];
  if (tail === "0004") return ["W".repeat(32), "LONG"];
  return [`Token ${tail}`, `T${tail}`];
}

function token(address) {
  const [name, symbol] = names(address);
  return {
    address,
    ath_at: "2026-09-10T13:00:00Z",
    ath_price_eth: wad("3"),
    creator: address,
    curve: address,
    curve_tokens: wad("1000"),
    description: "Fixture token",
    engine_version: 1,
    eth_reserve: wad("50"),
    fdv_eth: wad("1000"),
    graduation_eth: wad("70"),
    graduation_progress_bps: 7143,
    holder_count: 1,
    image_url: `/v1/tokens/${address}/image`,
    initial_virtual_eth: wad("1"),
    initial_virtual_token: wad("1000"),
    liquidity_eth: wad("50"),
    lp_tokens: wad("10"),
    market_cap_eth: wad("500"),
    name,
    pair: address,
    phase: "curve",
    price_change_24h_bps: 125,
    protocol_share_bps: 100,
    protocol_treasury: address,
    real_curve_eth: wad("50"),
    reserve_block: 123,
    reserve_hash: "0xabc",
    reserve_source: "indexed",
    snapshot: { chain_id: 4663, as_of_block: 123, as_of_block_hash: "0xabc", finality: "safe" },
    spot_price_eth: wad("2"),
    symbol,
    telegram_url: "",
    token_reserve: wad("900"),
    total_supply: wad("1000"),
    trade_fee_bps: 100,
    volume_24h_eth: wad("20"),
    weth: address,
    x_url: "",
  };
}

http
  .createServer((request, response) => {
    const path = new URL(request.url ?? "/", "http://127.0.0.1").pathname;
    const json = (status, body) => {
      response.writeHead(status, { "content-type": "application/json" });
      response.end(JSON.stringify(body));
    };
    if (path === "/healthz") return json(200, { ok: true });
    if (path === "/__calls") return json(200, Object.fromEntries(calls));
    if (path === "/__reset") {
      calls.clear();
      return json(200, { ok: true });
    }
    calls.set(path, (calls.get(path) ?? 0) + 1);
    if (path === "/v1/reference/eth-usd") return json(200, { price: "2000.00" });
    const match = /^\/v1\/tokens\/(0x[0-9a-f]{40})(\/image)?$/.exec(path);
    if (!match || match[1].endsWith("00ff")) return json(404, { title: "Not found" });
    if (match[2]) {
      response.writeHead(200, { "content-type": "image/png" });
      return response.end(png);
    }
    return json(200, token(match[1]));
  })
  .listen(port, "127.0.0.1");
