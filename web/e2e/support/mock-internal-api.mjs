// Fixture API for server-side fetches (share cards, token metadata). Playwright's page.route
// only sees browser traffic, so the Next server needs a real HTTP origin to read from.
import http from "node:http";

const port = Number(process.env.TASK7_MOCK_API_PORT ?? "3418");
const known = "0x0000000000000000000000000000000000000001";
const wad = (value) => `${BigInt(value) * 10n ** 18n}`;
const png = Buffer.from(
  "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==",
  "base64",
);

const token = {
  address: known,
  name: "Fixture Route",
  symbol: "FIX",
  market_cap_eth: wad("500"),
  graduation_progress_bps: 7143,
  phase: "curve",
  image_url: `/v1/tokens/${known}/image`,
};

http
  .createServer((request, response) => {
    const path = new URL(request.url ?? "/", "http://127.0.0.1").pathname;
    const json = (status, body) => {
      response.writeHead(status, { "content-type": "application/json" });
      response.end(JSON.stringify(body));
    };
    if (path === "/healthz") return json(200, { ok: true });
    if (path === `/v1/tokens/${known}`) return json(200, token);
    if (path === `/v1/tokens/${known}/image`) {
      response.writeHead(200, { "content-type": "image/png" });
      return response.end(png);
    }
    if (path === "/v1/reference/eth-usd") return json(200, { price: "2000.00" });
    return json(404, { title: "Not found" });
  })
  .listen(port, "127.0.0.1");
