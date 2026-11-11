// Demo token artwork for the local stack (web/scripts/local-stack.ps1): prints SQL that stores a
// generated 512x512 PNG for each token address given on the command line. The art is abstract
// (a two-hue gradient with soft discs) and deterministic per address. Stdlib only.
import { createHash } from "node:crypto";
import { crc32, deflateSync } from "node:zlib";

const SIZE = 512;
const CHAIN_ID = 31337;

function chunk(type, data) {
  const head = Buffer.alloc(8);
  head.writeUInt32BE(data.length, 0);
  head.write(type, 4, "ascii");
  const crc = Buffer.alloc(4);
  crc.writeUInt32BE(crc32(Buffer.concat([head.subarray(4), data])) >>> 0, 0);
  return Buffer.concat([head, data, crc]);
}

function png(width, height, rgb) {
  const header = Buffer.alloc(13);
  header.writeUInt32BE(width, 0);
  header.writeUInt32BE(height, 4);
  header[8] = 8; // bit depth
  header[9] = 2; // truecolour RGB
  const raw = Buffer.alloc((width * 3 + 1) * height);
  for (let y = 0; y < height; y++) {
    raw[y * (width * 3 + 1)] = 0; // filter: none
    rgb.copy(raw, y * (width * 3 + 1) + 1, y * width * 3, (y + 1) * width * 3);
  }
  return Buffer.concat([
    Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]),
    chunk("IHDR", header),
    chunk("IDAT", deflateSync(raw, { level: 9 })),
    chunk("IEND", Buffer.alloc(0)),
  ]);
}

function hsl(h, s, l) {
  const k = (n) => (n + h / 30) % 12;
  const a = s * Math.min(l, 1 - l);
  const f = (n) => l - a * Math.max(-1, Math.min(k(n) - 3, 9 - k(n), 1));
  return [f(0), f(8), f(4)].map((v) => Math.round(v * 255));
}

function art(address) {
  const seed = createHash("sha256").update(address.toLowerCase()).digest();
  const hueA = (seed[0] / 255) * 360;
  const hueB = (hueA + 60 + (seed[1] / 255) * 120) % 360;
  const angle = (seed[2] / 255) * Math.PI * 2;
  const from = hsl(hueA, 0.55, 0.32);
  const to = hsl(hueB, 0.6, 0.55);
  const discs = Array.from({ length: 3 }, (_, i) => ({
    x: (seed[3 + i * 3] / 255) * SIZE,
    y: (seed[4 + i * 3] / 255) * SIZE,
    r: 60 + (seed[5 + i * 3] / 255) * 160,
    color: hsl((hueB + 40 * (i + 1)) % 360, 0.7, 0.62),
  }));
  const rgb = Buffer.alloc(SIZE * SIZE * 3);
  const dx = Math.cos(angle);
  const dy = Math.sin(angle);
  for (let y = 0; y < SIZE; y++) {
    for (let x = 0; x < SIZE; x++) {
      const t = Math.min(1, Math.max(0, ((x - SIZE / 2) * dx + (y - SIZE / 2) * dy) / SIZE + 0.5));
      let px = from.map((c, i) => c + (to[i] - c) * t);
      for (const disc of discs) {
        const d = Math.hypot(x - disc.x, y - disc.y) / disc.r;
        if (d < 1) {
          const w = (1 - d * d) * 0.55;
          px = px.map((c, i) => c + (disc.color[i] - c) * w);
        }
      }
      const offset = (y * SIZE + x) * 3;
      rgb[offset] = px[0];
      rgb[offset + 1] = px[1];
      rgb[offset + 2] = px[2];
    }
  }
  return png(SIZE, SIZE, rgb);
}

const addresses = process.argv.slice(2).filter((value) => /^0x[0-9a-fA-F]{40}$/.test(value));
if (addresses.length === 0) {
  console.error("usage: node local-stack-images.mjs 0x<address> [...]");
  process.exit(1);
}
console.log("BEGIN;");
for (const address of addresses) {
  const image = art(address);
  const hex = address.slice(2).toLowerCase();
  const sha = createHash("sha256").update(image).digest("hex");
  // The image is bound to the launch event, as the API's read query joins on it.
  console.log(
    `DELETE FROM token_images WHERE chain_id = ${CHAIN_ID} AND token_address = decode('${hex}', 'hex');`,
  );
  console.log(
    `INSERT INTO token_images (chain_id, token_address, launch_tx_hash, launch_log_index, content_type, ` +
      `content, byte_size, sha256, updated_at) SELECT chain_id, token_address, tx_hash, log_index, 'image/png', ` +
      `decode('${image.toString("base64")}', 'base64'), ${image.length}, decode('${sha}', 'hex'), now() ` +
      `FROM token_launches WHERE chain_id = ${CHAIN_ID} AND token_address = decode('${hex}', 'hex');`,
  );
}
console.log("COMMIT;");
