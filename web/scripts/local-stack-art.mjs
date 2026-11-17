// Editorial artwork for the local stack's demo tokens: engraving-style line art on textured paper
// with flat colour collage blocks. Each seeded symbol gets its own motif, palette and layout, and the
// details are deterministic per symbol. Pure SVG strings; local-stack-images.mjs rasterises them.
// Stdlib only.
import { createHash } from "node:crypto";

const f = (n) => String(Math.round(n * 10) / 10);
const TAU = Math.PI * 2;
const ROMAN = ["i", "ii", "iii", "iv", "v", "vi", "vii", "viii", "ix", "x", "xi", "xii"];
const SERIF = `font-family="Georgia, 'Times New Roman', serif" font-style="italic"`;

function rng(seed) {
  let s = createHash("sha256").update(seed).digest().readUInt32LE(0);
  return () => {
    s = (s + 0x6d2b79f5) >>> 0;
    let t = s;
    t = Math.imul(t ^ (t >>> 15), t | 1);
    t ^= t + Math.imul(t ^ (t >>> 7), t | 61);
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

const between = (r, a, b) => a + (b - a) * r();
const pt = (cx, cy, radius, angle) => [
  cx + radius * Math.cos(angle),
  cy + radius * Math.sin(angle),
];
const xy = ([x, y]) => `${f(x)} ${f(y)}`;
const poly = (points, close = false) => "M" + points.map(xy).join("L") + (close ? "Z" : "");

/** Closed Catmull-Rom curve through the points, as cubic Béziers. */
function smoothClosed(points) {
  const n = points.length;
  let d = `M${xy(points[0])}`;
  for (let i = 0; i < n; i++) {
    const p0 = points[(i - 1 + n) % n];
    const p1 = points[i];
    const p2 = points[(i + 1) % n];
    const p3 = points[(i + 2) % n];
    const c1 = [p1[0] + (p2[0] - p0[0]) / 6, p1[1] + (p2[1] - p0[1]) / 6];
    const c2 = [p2[0] - (p3[0] - p1[0]) / 6, p2[1] - (p3[1] - p1[1]) / 6];
    d += `C${xy(c1)} ${xy(c2)} ${xy(p2)}`;
  }
  return d + "Z";
}

function rgb(hex) {
  const v = parseInt(hex.slice(1), 16);
  return [(v >> 16) & 255, (v >> 8) & 255, v & 255];
}

function mix(a, b, t) {
  const [x, y] = [rgb(a), rgb(b)];
  return (
    "#" +
    x
      .map((c, i) =>
        Math.round(c + (y[i] - c) * t)
          .toString(16)
          .padStart(2, "0"),
      )
      .join("")
  );
}

// paper: ground; ink: line work; a: strong accent; b: secondary/strip; c: light shape fill.
export const PALETTES = {
  terracotta: { paper: "#e8dfd0", ink: "#2b2620", a: "#c8623e", b: "#d9c6a6", c: "#efe7d8" },
  sage: { paper: "#e9e4d4", ink: "#211f1b", a: "#4f9a5a", b: "#1f1d1a", c: "#efe9da" },
  sky: { paper: "#dde6e1", ink: "#2a2a28", a: "#6b9bd2", b: "#b8d3ca", c: "#eef2ee" },
  mustard: { paper: "#f7f0dc", ink: "#2b2620", a: "#c9a24a", b: "#f0d488", c: "#fbf6e6" },
  oxblood: { paper: "#efe4dc", ink: "#2b2220", a: "#8e3a36", b: "#dba9a2", c: "#f6ece6" },
  night: { paper: "#1d2333", ink: "#e9e1d1", a: "#d6a441", b: "#2d3650", c: "#283048", dark: true },
  olive: { paper: "#e3dcc5", ink: "#2b2a20", a: "#7a7838", b: "#c69a4c", c: "#efe9d6" },
  plum: { paper: "#ebe5ec", ink: "#2a2430", a: "#6f4f86", b: "#c9b6d6", c: "#f4eff5" },
  teal: { paper: "#e4ecea", ink: "#1f2a2a", a: "#2f7774", b: "#e38569", c: "#f1f5f3" },
  ember: { paper: "#27231f", ink: "#ece3d4", a: "#e2793a", b: "#3a342e", c: "#332d28", dark: true },
  rose: { paper: "#f3e6e2", ink: "#2b2220", a: "#d97f86", b: "#9fbfb0", c: "#faf1ee" },
  cobalt: { paper: "#e7e9ee", ink: "#1c2233", a: "#2f4fa8", b: "#e7c35a", c: "#f3f4f7" },
};

function orbit(p, r, v) {
  const cx = [420, 400, 380, 440][v % 4];
  const cy = between(r, 400, 450);
  const R = between(r, 145, 175);
  const flip = v % 2 === 1 ? `translate(${f(2 * cx)} 0) scale(-1 1)` : "";
  let s = `<g fill="none" stroke="${p.ink}">`;
  s += `<circle cx="${f(cx)}" cy="${f(cy)}" r="${f(R * 1.6)}" stroke-width="1.2"/>`;
  s += `<circle cx="${f(cx)}" cy="${f(cy)}" r="${f(R * 2.1)}" stroke-width="0.9" stroke-dasharray="2 7"/>`;
  s += `<circle cx="${f(cx)}" cy="${f(cy)}" r="${f(R * 2.6)}" stroke-width="1.4"/>`;
  for (let i = 0; i < 8; i++) {
    const a = (i * TAU) / 8;
    s += `<path d="${poly([pt(cx, cy, R * 1.6 - 8, a), pt(cx, cy, R * 1.6 + 8, a)])}" stroke-width="1"/>`;
  }
  s += `</g><g clip-path="url(#planet)">`;
  s += `<rect x="${f(cx - R)}" y="${f(cy - R)}" width="${f(2 * R)}" height="${f(2 * R)}" fill="${p.c}"/>`;
  s += `<rect x="${f(cx - R)}" y="${f(cy - R)}" width="${f(2 * R)}" height="${f(2 * R)}" fill="url(#hatch)" opacity="0.5"/>`;
  s += `<g transform="${flip}">`;
  for (const [k, o] of [
    [0.44, 1],
    [0.75, 0.7],
  ])
    s += `<path d="M${f(cx)} ${f(cy - R)}a${f(R)} ${f(R)} 0 0 1 0 ${f(2 * R)}a${f(R * k)} ${f(R)} 0 0 0 0 ${f(-2 * R)}Z" fill="url(#hatch2)" opacity="${o}"/>`;
  s += `</g><g fill="none" stroke="${p.ink}" stroke-width="1">`;
  s += `<ellipse cx="${f(cx)}" cy="${f(cy)}" rx="${f(R)}" ry="${f(R * 0.23)}"/>`;
  s += `<ellipse cx="${f(cx)}" cy="${f(cy - R * 0.4)}" rx="${f(R * 0.92)}" ry="${f(R * 0.19)}"/>`;
  s += `<ellipse cx="${f(cx)}" cy="${f(cy + R * 0.4)}" rx="${f(R * 0.92)}" ry="${f(R * 0.19)}"/>`;
  s += `<ellipse cx="${f(cx)}" cy="${f(cy)}" rx="${f(R * 0.41)}" ry="${f(R)}"/>`;
  s += `</g></g><circle cx="${f(cx)}" cy="${f(cy)}" r="${f(R)}" fill="none" stroke="${p.ink}" stroke-width="2"/>`;
  const [mx, my] = pt(cx, cy, R * 2.1, between(r, -2.4, -0.7));
  s += `<circle cx="${f(mx)}" cy="${f(my)}" r="40" fill="${p.c}"/><circle cx="${f(mx)}" cy="${f(my)}" r="40" fill="url(#stipple)"/>`;
  s += `<circle cx="${f(mx)}" cy="${f(my)}" r="40" fill="none" stroke="${p.ink}" stroke-width="1.6"/>`;
  const defs = `<clipPath id="planet"><circle cx="${f(cx)}" cy="${f(cy)}" r="${f(R)}"/></clipPath>`;
  return { defs, body: s };
}

const WINDS = [
  "N",
  "N.N.E",
  "N.E",
  "E.N.E",
  "E",
  "E.S.E",
  "S.E",
  "S.S.E",
  "S",
  "S.S.W",
  "S.W",
  "W.S.W",
  "W",
  "W.N.W",
  "N.W",
  "N.N.W",
];

function compass(p, r, v) {
  const cx = 400 + between(r, -40, 40);
  const cy = [650, 600, 680][v % 3];
  const R = 440;
  let s = `<g transform="rotate(${f(between(r, -10, 10))} ${f(cx)} ${f(cy)})" stroke="${p.ink}">`;
  for (const [k, w] of [
    [R, 2.2],
    [R - 44, 1.4],
    [R - 150, 1.4],
    [R - 156, 0.7],
  ])
    s += `<circle cx="${f(cx)}" cy="${f(cy)}" r="${k}" fill="none" stroke-width="${w}"/>`;
  for (let d = 0; d < 360; d += 2.5) {
    const a = (d * Math.PI) / 180 - Math.PI / 2;
    const len = d % 22.5 === 0 ? 44 : d % 7.5 === 0 ? 26 : 14;
    s += `<path d="${poly([pt(cx, cy, R - 44, a), pt(cx, cy, R - 44 + len, a)])}" stroke-width="1"/>`;
  }
  s += `<g fill="${p.ink}" stroke="none" ${SERIF} font-size="26" text-anchor="middle">`;
  WINDS.forEach((name, i) => {
    s += `<text x="${f(cx)}" y="${f(cy - R + 100)}" transform="rotate(${i * 22.5} ${f(cx)} ${f(cy)})">${name}</text>`;
  });
  s += `</g>`;
  const order = [...WINDS.keys()].sort(
    (x, y) => (x % 4 === 0) - (y % 4 === 0) || (x % 2 === 0) - (y % 2 === 0),
  );
  for (const i of order) {
    const a = (i * Math.PI) / 8 - Math.PI / 2;
    const L = (R - 160) * (i % 4 === 0 ? 1 : i % 2 === 0 ? 0.66 : 0.42);
    const w = Math.max(14, L * 0.12);
    const tip = pt(cx, cy, L, a);
    const left = pt(cx, cy, w, a - Math.PI / 2);
    const right = pt(cx, cy, w, a + Math.PI / 2);
    s += `<path d="${poly([[cx, cy], tip, left], true)}" fill="${p.ink}" stroke-width="0.8"/>`;
    s += `<path d="${poly([[cx, cy], tip, right], true)}" fill="${p.c}" stroke-width="0.8"/>`;
    s += `<path d="${poly([[cx, cy], tip, right], true)}" fill="url(#hatch)" stroke="none"/>`;
  }
  s += `<circle cx="${f(cx)}" cy="${f(cy)}" r="22" fill="${p.c}" stroke-width="1.6"/>`;
  s += `<circle cx="${f(cx)}" cy="${f(cy)}" r="8" fill="${p.ink}"/></g>`;
  return { defs: "", body: s };
}

function nautilus(p, r, v) {
  const field = p.dark ? p.paper : p.ink;
  const line = p.dark ? p.ink : p.c;
  const cx = [520, 470][v % 2];
  const cy = between(r, 380, 430);
  const b = 0.3063;
  const a = 4.5;
  const phi = between(r, 0, TAU);
  const P = (t) => pt(cx, cy, a * Math.exp(b * t), t + phi);
  const tMax = Math.log(360 / a) / b;
  const points = [];
  for (let t = 0; t <= tMax; t += 0.04) points.push(P(t));
  let s = `<rect x="150" width="650" height="800" fill="${field}"/>`;
  s += `<path d="${poly(points, true)}" fill="${line}" fill-opacity="0.1"/>`;
  s += `<circle cx="${f(cx + 40 * Math.cos(phi))}" cy="${f(cy + 40 * Math.sin(phi))}" r="240" fill="url(#glow)"/>`;
  s += `<g fill="none" stroke="${line}">`;
  for (let t = tMax - 0.2; t > TAU + 0.6; t -= 0.09) {
    const [x1, y1] = P(t);
    const len = a * Math.exp(b * t) * 0.1;
    const [x2, y2] = [
      x1 + ((cx - x1) / Math.hypot(cx - x1, cy - y1)) * len,
      y1 + ((cy - y1) / Math.hypot(cx - x1, cy - y1)) * len,
    ];
    s += `<path d="M${f(x1)} ${f(y1)}L${f(x2)} ${f(y2)}" stroke-width="0.5" opacity="0.45"/>`;
  }
  for (let t = tMax - 0.2; t > TAU + 0.6; t -= 0.38) {
    const A = P(t);
    const B = P(t - TAU);
    const ctrl = [
      (A[0] + B[0]) / 2 - (B[1] - A[1]) * 0.18,
      (A[1] + B[1]) / 2 + (B[0] - A[0]) * 0.18,
    ];
    s += `<path d="M${xy(A)}Q${xy(ctrl)} ${xy(B)}" stroke-width="1.4" opacity="0.85"/>`;
  }
  s += `<path d="${poly(points)}" stroke-width="2.4"/>`;
  s += `<path d="M${xy(P(tMax))}L${xy(P(tMax - TAU))}" stroke-width="2.4"/></g>`;
  const defs =
    `<radialGradient id="glow"><stop offset="0" stop-color="#fff" stop-opacity="0.9"/>` +
    `<stop offset="0.35" stop-color="${line}" stop-opacity="0.45"/><stop offset="1" stop-color="${line}" stop-opacity="0"/></radialGradient>`;
  return { defs, body: s };
}

function columns(p, r, v) {
  const n = v % 2 === 0 ? 2 : 3;
  const spacing = n === 2 ? 190 : 140;
  const colW = n === 2 ? 74 : 60;
  const x0 = n === 2 ? 80 : 60;
  const top = 220;
  const bottom = 650;
  const span = (n - 1) * spacing + colW;
  let s = `<g stroke="${p.ink}">`;
  s += `<rect x="${x0 - 40}" y="${top - 74}" width="${span + 80}" height="12" fill="${p.c}" stroke-width="1.2"/>`;
  s += `<rect x="${x0 - 40}" y="${top - 62}" width="${span + 80}" height="26" fill="${p.c}" stroke-width="1.4"/>`;
  for (let i = 0; i < n; i++) {
    const x = x0 + i * spacing;
    s += `<rect x="${x - 20}" y="${top - 36}" width="${colW + 40}" height="16" fill="${p.c}" stroke-width="1.4"/>`;
    s += `<path d="${poly(
      [
        [x - 4, top],
        [x + colW + 4, top],
        [x + colW + 18, top - 20],
        [x - 18, top - 20],
      ],
      true,
    )}" fill="${p.c}" stroke-width="1.4"/>`;
    s += `<rect x="${x}" y="${top}" width="${colW}" height="${bottom - top}" fill="${p.c}" stroke-width="1.4"/>`;
    s += `<rect x="${f(x + colW * 0.7)}" y="${top}" width="${f(colW * 0.3)}" height="${bottom - top}" fill="url(#hatch)" stroke="none" opacity="0.7"/>`;
    for (let fx = x + 6; fx < x + colW - 3; fx += 7)
      s += `<path d="M${fx} ${top + 16}V${bottom}" stroke-width="0.8" opacity="0.8"/>`;
    s += `<path d="M${x} ${top + 8}h${colW}M${x} ${top + 14}h${colW}" stroke-width="0.8"/>`;
  }
  s += `<rect x="${x0 - 60}" y="${bottom}" width="${span + 120}" height="18" fill="${p.c}" stroke-width="1.4"/>`;
  s += `<rect x="${x0 - 80}" y="${bottom + 18}" width="${span + 160}" height="22" fill="${p.c}" stroke-width="1.4"/>`;
  s += `<path d="M${x0 - 62} 110V740" stroke-width="0.7" stroke-dasharray="3 4"/>`;
  for (let y = 140; y < 740; y += 40) s += `<path d="M${x0 - 68} ${y}h12" stroke-width="0.8"/>`;
  s += `</g><g fill="none" stroke="${p.ink}" stroke-width="2.2">`;
  for (let i = 0; i < 3; i++) {
    const lx = between(r, 560, 760);
    const ly = between(r, 120, 700);
    const pts = [];
    for (let k = 0; k < 6; k++)
      pts.push(pt(lx, ly, between(r, 70, 190), (k * TAU) / 6 + between(r, -0.3, 0.3)));
    s += `<path d="${smoothClosed(pts)}"/>`;
  }
  s += `</g>`;
  return { defs: "", body: s };
}

function waves(p, r, v) {
  const sx = [560, 300, 430][v % 3];
  const sy = between(r, 250, 320);
  const sr = between(r, 140, 180);
  let s = `<circle cx="${f(sx)}" cy="${f(sy)}" r="${f(sr)}" fill="${p.a}"/><g clip-path="url(#sun)" stroke="${p.paper}">`;
  for (let y = sy; y < sy + sr; y += 11)
    s += `<path d="M0 ${f(y)}H800" stroke-width="${f(1 + ((y - sy) / sr) * 5)}"/>`;
  s += `</g>`;
  let k = 0;
  for (let y = 420; y < 840; y += 22, k++) {
    const amp = between(r, 8, 18);
    const wl = between(r, 70, 130);
    const ph = k * 0.9 + r() * 0.5;
    const pts = [];
    for (let x = -10; x <= 810; x += 6)
      pts.push([
        x,
        y + amp * Math.sin((x / wl) * TAU + ph) + 0.35 * amp * Math.sin((x / wl) * 5 + ph * 2),
      ]);
    const shape = `${poly(pts)}L810 820L-10 820Z`;
    s += `<path d="${shape}" fill="${p.c}" stroke="${p.ink}" stroke-width="1.6"/>`;
    if (k % 3 === 1) s += `<path d="${shape}" fill="url(#hatch)" stroke="none" opacity="0.8"/>`;
    s += `<path d="${poly(pts.map(([x, py]) => [x, py + 6]))}" fill="none" stroke="${p.ink}" stroke-width="0.7"/>`;
  }
  const defs = `<clipPath id="sun"><circle cx="${f(sx)}" cy="${f(sy)}" r="${f(sr)}"/></clipPath>`;
  return { defs, body: s };
}

function contour(p, r, v) {
  const peaks = [
    { x: between(r, 300, 420), y: between(r, 380, 470), R: 400, n: 15 },
    { x: between(r, 600, 700), y: between(r, 140, 240), R: 170, n: 7 },
  ];
  if (v % 2 === 1) for (const peak of peaks) peak.x = 800 - peak.x;
  let s = `<g stroke="${p.ink}">`;
  for (const peak of peaks) {
    const [s1, s2, s3] = [r() * TAU, r() * TAU, r() * TAU];
    for (let k = peak.n; k >= 1; k--) {
      const rad = (peak.R * k) / peak.n;
      const pts = [];
      for (let i = 0; i < 36; i++) {
        const th = (i * TAU) / 36;
        const rr =
          rad *
          (1 +
            0.16 * Math.sin(3 * th + s1) +
            0.08 * Math.sin(5 * th + s2) +
            0.05 * Math.sin(2 * th + s3 + k * 0.15));
        pts.push(pt(peak.x, peak.y, rr, th));
      }
      const fill =
        k <= 3 ? `fill="${p.a}" fill-opacity="${f(0.55 + (3 - k) * 0.15)}"` : `fill="none"`;
      s += `<path d="${smoothClosed(pts)}" ${fill} stroke-width="${k % 4 === 0 ? 1.8 : 0.9}"/>`;
    }
    s += `<path d="${poly(
      [
        [peak.x, peak.y - 9],
        [peak.x + 8, peak.y + 6],
        [peak.x - 8, peak.y + 6],
      ],
      true,
    )}" fill="${p.ink}"/>`;
  }
  const route = [[-10, 760]];
  for (let i = 1; i <= 8; i++) {
    const t = i / 8;
    route.push([
      -10 + (peaks[0].x + 10) * t + between(r, -40, 40) * (1 - t),
      760 + (peaks[0].y - 760) * t,
    ]);
  }
  s += `<path d="${poly(route)}" fill="none" stroke-width="1.6" stroke-dasharray="6 5"/></g>`;
  s += `<text x="${f(peaks[0].x + 14)}" y="${f(peaks[0].y - 12)}" fill="${p.ink}" ${SERIF} font-size="15">${Math.floor(between(r, 900, 3200))}</text>`;
  return { defs: "", body: s };
}

function frond(P0, C, P2, maxL, fills, p, outline) {
  const B = (t) => [
    (1 - t) ** 2 * P0[0] + 2 * (1 - t) * t * C[0] + t * t * P2[0],
    (1 - t) ** 2 * P0[1] + 2 * (1 - t) * t * C[1] + t * t * P2[1],
  ];
  const D = (t) => [
    2 * (1 - t) * (C[0] - P0[0]) + 2 * t * (P2[0] - C[0]),
    2 * (1 - t) * (C[1] - P0[1]) + 2 * t * (P2[1] - C[1]),
  ];
  const stroke = outline ? `stroke="${p.ink}" stroke-width="1"` : `stroke="none"`;
  let s = "";
  let i = 0;
  for (let t = 0.1; t < 0.97; t += 0.042) {
    for (const side of [-1, 1]) {
      const tt = t + (side > 0 ? 0.02 : 0);
      const S = B(tt);
      const [dx, dy] = D(tt);
      const ang = Math.atan2(dy, dx) + side * 0.95;
      const L = maxL * Math.pow(1 - tt, 0.75) + 14;
      const w = L * 0.24;
      const dir = [Math.cos(ang), Math.sin(ang)];
      const perp = [-dir[1], dir[0]];
      const T = [S[0] + L * dir[0], S[1] + L * dir[1]];
      const c1 = [S[0] + 0.5 * L * dir[0] + w * perp[0], S[1] + 0.5 * L * dir[1] + w * perp[1]];
      const c2 = [S[0] + 0.5 * L * dir[0] - w * perp[0], S[1] + 0.5 * L * dir[1] - w * perp[1]];
      s += `<path d="M${xy(S)}Q${xy(c1)} ${xy(T)}Q${xy(c2)} ${xy(S)}Z" fill="${fills[i++ % fills.length]}" ${stroke}/>`;
      if (outline)
        s += `<path d="M${xy(S)}L${xy([S[0] + 0.9 * L * dir[0], S[1] + 0.9 * L * dir[1]])}" stroke="${p.ink}" stroke-width="0.7"/>`;
    }
  }
  const stem = `M${xy(P0)}Q${xy(C)} ${xy(P2)}`;
  return (
    s + `<path d="${stem}" fill="none" stroke="${outline ? p.ink : fills[0]}" stroke-width="3"/>`
  );
}

function fern(p, r, v) {
  const flip = v % 2 === 1 ? `transform="translate(800 0) scale(-1 1)"` : "";
  let s = `<g ${flip}>`;
  s += frond(
    [620, 830],
    [between(r, 640, 740), 420],
    [between(r, 330, 430), 70],
    120,
    [p.a],
    p,
    false,
  );
  s += frond(
    [200, 820],
    [between(r, 200, 270), between(r, 280, 340)],
    [between(r, 520, 600), 80],
    150,
    [p.c, "url(#hatch)"],
    p,
    true,
  );
  s += `</g>`;
  return { defs: "", body: s };
}

function rays(p, r, v) {
  const hy = [560, 520, 600][v % 3];
  const cx = between(r, 340, 460);
  const n = 28;
  let s = `<g clip-path="url(#sky)">`;
  for (let i = 0; i < n; i++) {
    const a0 = Math.PI + (i * Math.PI) / n;
    const fill = i % 2 === 0 ? p.a : "url(#hatch)";
    s += `<path d="${poly([[cx, hy], pt(cx, hy, 1000, a0), pt(cx, hy, 1000, a0 + Math.PI / n)], true)}" fill="${fill}"/>`;
  }
  s += `</g><rect y="${hy}" width="800" height="${800 - hy}" fill="${p.b}"/><g stroke="${p.ink}" fill="none">`;
  for (let k = 1; k <= 14; k++)
    s += `<path d="M0 ${f(hy + (800 - hy) * (k / 14) ** 2)}H800" stroke-width="0.9"/>`;
  for (let j = -10; j <= 10; j++)
    s += `<path d="M${f(cx)} ${hy}L${f(cx + j * 120)} 800" stroke-width="0.7"/>`;
  s += `<path d="M${f(cx - 150)} ${hy}A150 150 0 0 1 ${f(cx + 150)} ${hy}Z" fill="${p.c}" stroke-width="2"/>`;
  for (const k of [120, 90, 60])
    s += `<path d="M${f(cx - k)} ${hy}A${k} ${k} 0 0 1 ${f(cx + k)} ${hy}" stroke-width="0.9"/>`;
  s += `<path d="M0 ${hy}H800" stroke-width="2"/></g>`;
  return { defs: `<clipPath id="sky"><rect width="800" height="${hy}"/></clipPath>`, body: s };
}

const NUMERALS = ["XII", "I", "II", "III", "IV", "V", "VI", "VII", "VIII", "IX", "X", "XI"];

function clock(p, r, v) {
  const cx = [440, 470, 400][v % 3];
  const cy = [430, 470, 400][v % 3];
  const R = 330;
  let s = `<circle cx="${cx + 28}" cy="${cy + 28}" r="${R + 6}" fill="${p.a}"/><g stroke="${p.ink}">`;
  s += `<circle cx="${cx}" cy="${cy}" r="${R}" fill="${p.c}" stroke-width="2.4"/>`;
  const sector = (r1, r2, a1, a2) =>
    `M${xy(pt(cx, cy, r1, a1))}A${r1} ${r1} 0 0 1 ${xy(pt(cx, cy, r1, a2))}L${xy(pt(cx, cy, r2, a2))}A${r2} ${r2} 0 0 0 ${xy(pt(cx, cy, r2, a1))}Z`;
  s += `<path d="${sector(R - 48, R - 150, 0.35, 1.9)}" fill="url(#hatch)" stroke="none" opacity="0.6"/>`;
  for (const [k, w] of [
    [R - 48, 1.2],
    [R - 54, 0.7],
    [R - 150, 1.2],
  ])
    s += `<circle cx="${cx}" cy="${cy}" r="${k}" fill="none" stroke-width="${w}"/>`;
  for (let m = 0; m < 120; m++) {
    const a = (m * Math.PI) / 60;
    const len = m % 10 === 0 ? 48 : m % 2 === 0 ? 22 : 12;
    s += `<path d="${poly([pt(cx, cy, R, a), pt(cx, cy, R - len, a)])}" stroke-width="1"/>`;
  }
  s += `<circle cx="${cx}" cy="${cy - 70}" r="190" fill="none" stroke-width="1.4"/>`;
  s += `<circle cx="${cx}" cy="${cy + 40}" r="120" fill="none" stroke-width="0.8" stroke-dasharray="3 5"/></g>`;
  s += `<g fill="${p.ink}" ${SERIF} font-size="34" text-anchor="middle" dominant-baseline="middle">`;
  NUMERALS.forEach((label, h) => {
    const [x, y] = pt(cx, cy, R - 92, (h * Math.PI) / 6 - Math.PI / 2);
    s += `<text x="${f(x)}" y="${f(y)}" transform="rotate(${h * 30} ${f(x)} ${f(y)})">${label}</text>`;
  });
  s += `</g>`;
  const hand = (len, w, angle) => {
    const tip = pt(cx, cy, len, angle);
    const mid = len * 0.62;
    return poly(
      [
        pt(cx, cy, 18, angle + Math.PI),
        pt(cx, cy, Math.hypot(mid, w), angle - Math.atan2(w, mid)),
        tip,
        pt(cx, cy, Math.hypot(mid, w), angle + Math.atan2(w, mid)),
      ],
      true,
    );
  };
  s += `<path d="${hand(170, 16, r() * TAU)}" fill="${p.ink}"/><path d="${hand(262, 9, r() * TAU)}" fill="${p.ink}"/>`;
  s += `<circle cx="${cx}" cy="${cy}" r="14" fill="${p.c}" stroke="${p.ink}" stroke-width="1.6"/><circle cx="${cx}" cy="${cy}" r="5" fill="${p.ink}"/>`;
  return { defs: "", body: s };
}

function mosaic(p, r, v) {
  const stops = [mix(p.ink, p.b, 0.45), mix(p.ink, p.b, 0.45), mix(p.b, p.c, 0.3), p.c, p.c].map(
    rgb,
  );
  const table = (i) => stops.map((c) => f(c[i] / 255)).join(" ");
  const defs =
    `<filter id="clouds" x="0" y="0" width="100%" height="100%" color-interpolation-filters="sRGB">` +
    `<feTurbulence type="fractalNoise" baseFrequency="${(0.0045 + r() * 0.003).toFixed(4)}" numOctaves="5" seed="${Math.floor(r() * 999)}"/>` +
    `<feColorMatrix type="matrix" values="0.33 0.33 0.33 0 0 0.33 0.33 0.33 0 0 0.33 0.33 0.33 0 0 0 0 0 0 1"/>` +
    `<feComponentTransfer><feFuncR type="table" tableValues="${table(0)}"/><feFuncG type="table" tableValues="${table(1)}"/>` +
    `<feFuncB type="table" tableValues="${table(2)}"/></feComponentTransfer></filter>`;
  let s = `<rect width="800" height="800" filter="url(#clouds)"/>`;
  const cell = 800 / 6;
  const colors = [p.a, p.b, mix(p.c, p.b, 0.4)];
  for (let i = 0; i < 7 + (v % 2); i++) {
    const x = Math.floor(r() * 6);
    const y = Math.floor(r() * 6);
    const w = 1 + Math.floor(r() * 2);
    const h = 1 + Math.floor(r() * 3);
    s += `<rect x="${f(x * cell)}" y="${f(y * cell)}" width="${f(w * cell)}" height="${f(h * cell)}" fill="${colors[i % 3]}"/>`;
  }
  return { defs, body: s };
}

function bloom(p, r, v) {
  const cx = [400, 430, 380][v % 3];
  const cy = [420, 400, 440][v % 3];
  const rot = r() * 360;
  const petal = (L, w) =>
    `M0 0C${f(w)} ${f(-L * 0.3)} ${f(w * 0.8)} ${f(-L * 0.85)} 0 ${f(-L)}C${f(-w * 0.8)} ${f(-L * 0.85)} ${f(-w)} ${f(-L * 0.3)} 0 0Z`;
  let s = `<g stroke="${p.ink}"><circle cx="${cx}" cy="${cy}" r="360" fill="none" stroke-width="0.9" stroke-dasharray="2 6"/>`;
  s += `<circle cx="${cx}" cy="${cy}" r="380" fill="none" stroke-width="1.2"/>`;
  const n = 10 + Math.floor(r() * 4);
  const layers = [
    [320, 120, 0, p.c, true],
    [210, 80, 0.5, p.a, false],
    [110, 46, 0, p.b, false],
  ];
  for (const [L, w, offset, fill, hatched] of layers) {
    const count = L === 110 ? 8 : n;
    for (let i = 0; i < count; i++) {
      const t = `translate(${cx} ${cy}) rotate(${f(rot + ((i + offset) * 360) / count)})`;
      s += `<path d="${petal(L, w)}" transform="${t}" fill="${fill}" stroke-width="1.3"/>`;
      if (hatched && i % 2 === 0)
        s += `<path d="${petal(L, w)}" transform="${t}" fill="url(#hatch)" stroke="none"/>`;
      s += `<path d="M0 -20V${f(-L * 0.9)}" transform="${t}" stroke-width="0.8"/>`;
    }
  }
  s += `<circle cx="${cx}" cy="${cy}" r="46" fill="${p.c}"/><circle cx="${cx}" cy="${cy}" r="46" fill="url(#stipple)" stroke-width="1.6"/></g>`;
  return { defs: "", body: s };
}

function radar(p, r, v) {
  const cx = [360, 400, 330][v % 3];
  const cy = [440, 420, 470][v % 3];
  const R = 330;
  let s = `<g stroke="${p.ink}"><circle cx="${cx}" cy="${cy}" r="${R}" fill="${p.c}" fill-opacity="0.55" stroke-width="2"/>`;
  for (let k = 1; k < 4; k++)
    s += `<circle cx="${cx}" cy="${cy}" r="${f((R * k) / 4)}" fill="none" stroke-width="1"/>`;
  s += `<path d="M${cx - R} ${cy}H${cx + R}M${cx} ${cy - R}V${cy + R}" stroke-width="1"/>`;
  for (let d = 0; d < 360; d += 5) {
    const a = (d * Math.PI) / 180;
    s += `<path d="${poly([pt(cx, cy, R, a), pt(cx, cy, R + (d % 30 === 0 ? 18 : 8), a)])}" stroke-width="1"/>`;
  }
  const th = r() * TAU;
  s += `<path d="M${cx} ${cy}L${xy(pt(cx, cy, R, th - 0.9))}A${R} ${R} 0 0 1 ${xy(pt(cx, cy, R, th))}Z" fill="${p.a}" fill-opacity="0.5" stroke="none"/>`;
  for (let j = 1; j <= 10; j++)
    s += `<path d="${poly([[cx, cy], pt(cx, cy, R, th - j * 0.08)])}" stroke="${p.a}" stroke-width="1.4" opacity="${f(0.55 - j * 0.04)}"/>`;
  s += `<path d="${poly([[cx, cy], pt(cx, cy, R, th)])}" stroke="${p.a}" stroke-width="3"/>`;
  for (let i = 0; i < 7; i++) {
    const [bx, by] = pt(cx, cy, between(r, 0.2, 0.9) * R, r() * TAU);
    s += `<circle cx="${f(bx)}" cy="${f(by)}" r="5" fill="${p.ink}"/><circle cx="${f(bx)}" cy="${f(by)}" r="12" fill="none" stroke-width="0.8"/>`;
  }
  s += `</g><g fill="${p.ink}" ${SERIF} font-size="14" text-anchor="middle" dominant-baseline="middle">`;
  for (let d = 0; d < 360; d += 30) {
    const [x, y] = pt(cx, cy, R + 36, (d * Math.PI) / 180 - Math.PI / 2);
    s += `<text x="${f(x)}" y="${f(y)}">${String(d).padStart(3, "0")}</text>`;
  }
  s += `</g>`;
  return { defs: "", body: s };
}

function comet(p, r, v) {
  const hx = [600, 220, 560][v % 3];
  const hy = [210, 220, 300][v % 3];
  const ang = Math.atan2(620 - hy, (hx > 400 ? 150 : 650) - hx);
  let s = `<g fill="${p.ink}" stroke="${p.ink}">`;
  for (let i = 0; i < 36; i++) {
    const [x, y] = [between(r, 20, 780), between(r, 20, 780)];
    if (i % 4 === 0) {
      const k = between(r, 5, 10);
      s += `<path d="M${f(x)} ${f(y - k)}L${f(x + 1.5)} ${f(y - 1.5)}L${f(x + k)} ${f(y)}L${f(x + 1.5)} ${f(y + 1.5)}L${f(x)} ${f(y + k)}L${f(x - 1.5)} ${f(y + 1.5)}L${f(x - k)} ${f(y)}L${f(x - 1.5)} ${f(y - 1.5)}Z" stroke="none"/>`;
    } else s += `<circle cx="${f(x)}" cy="${f(y)}" r="${f(between(r, 0.8, 2))}" stroke="none"/>`;
  }
  s += `<path d="${poly([[hx, hy], pt(hx, hy, 720, ang - 0.1), pt(hx, hy, 720, ang + 0.1)], true)}" fill="${p.a}" fill-opacity="0.55" stroke="none"/>`;
  s += `<g fill="none">`;
  for (let i = 0; i < 46; i++) {
    const spread = (i / 45 - 0.5) * 0.24;
    const L = between(r, 380, 720);
    const ctrl = pt(hx, hy, L * 0.5, ang + spread * 0.6);
    s += `<path d="M${hx} ${hy}Q${xy(ctrl)} ${xy(pt(hx, hy, L, ang + spread))}" stroke-width="${f(between(r, 0.5, 1.3))}" opacity="${f(between(r, 0.35, 0.9))}"/>`;
  }
  s += `</g><circle cx="${hx}" cy="${hy}" r="64" fill="url(#halo)" stroke="none"/>`;
  s += `<circle cx="${hx}" cy="${hy}" r="30" fill="${p.c}" stroke-width="1.6"/><circle cx="${hx}" cy="${hy}" r="30" fill="url(#stipple)" stroke="none"/></g>`;
  const defs = `<radialGradient id="halo"><stop offset="0.4" stop-color="${p.c}" stop-opacity="0.9"/><stop offset="1" stop-color="${p.c}" stop-opacity="0"/></radialGradient>`;
  return { defs, body: s };
}

const MOTIFS = {
  orbit,
  compass,
  nautilus,
  columns,
  waves,
  contour,
  fern,
  rays,
  clock,
  mosaic,
  bloom,
  radar,
  comet,
};

// symbol -> [motif, palette, layout, variant]; every seeded token in local-stack.ps1 has its own row.
export const TOKEN_ART = {
  ORBIT: ["orbit", "terracotta", "stripR", 0],
  PAPER: ["columns", "sky", "stripR", 0],
  NIGHT: ["mosaic", "night", "none", 0],
  FIRE: ["rays", "ember", "none", 0],
  HRBR: ["waves", "teal", "block", 0],
  MOON: ["orbit", "night", "stripL", 1],
  FOX: ["contour", "oxblood", "split", 0],
  GLASS: ["nautilus", "sage", "stripL", 0],
  VLVT: ["comet", "plum", "block", 0],
  SOLAR: ["rays", "mustard", "stripR", 1],
  LOTUS: ["bloom", "rose", "none", 0],
  WHALE: ["waves", "cobalt", "stripR", 1],
  BLOOM: ["bloom", "olive", "split", 1],
  AMBER: ["comet", "ember", "stripL", 1],
  CORAL: ["radar", "rose", "stripR", 0],
  HERON: ["fern", "sky", "split", 0],
  OWL: ["clock", "olive", "block", 0],
  MOTH: ["orbit", "plum", "block", 2],
  COBLT: ["contour", "cobalt", "block", 1],
  KITE: ["compass", "mustard", "split", 0],
  ECHO: ["fern", "sage", "block", 1],
  MINT: ["mosaic", "sky", "none", 1],
  RUST: ["clock", "ember", "stripR", 1],
  BELL: ["columns", "oxblood", "block", 1],
  FERN: ["fern", "plum", "stripR", 2],
  TIDE: ["waves", "mustard", "split", 2],
  COMMA: ["nautilus", "terracotta", "block", 1],
  OPAL: ["mosaic", "rose", "none", 2],
  RADAR: ["radar", "sage", "split", 1],
  PEPR: ["orbit", "teal", "split", 3],
  NRTH: ["compass", "night", "stripL", 1],
  CEDAR: ["fern", "olive", "none", 3],
  VOWL: ["clock", "plum", "split", 2],
  FROST: ["contour", "sky", "stripL", 2],
  CRUMB: ["columns", "mustard", "stripL", 2],
  GLDN: ["rays", "cobalt", "block", 2],
  PLUM: ["bloom", "night", "stripR", 2],
  TRAM: ["comet", "cobalt", "split", 2],
  MRBL: ["columns", "rose", "stripR", 3],
  ANCHR: ["compass", "teal", "block", 2],
};

function frame(p, layout, motif, caption) {
  const grid = p.dark ? p.ink : "#7a5f3e";
  let base = `<rect width="800" height="800" fill="${p.paper}"/>`;
  if (layout === "stripR")
    base += `<rect x="500" width="300" height="800" fill="${p.b}"/><rect x="500" width="300" height="800" fill="url(#grid)"/>`;
  if (layout === "stripL") base += `<rect width="112" height="800" fill="${p.a}"/>`;
  if (layout === "split") base += `<rect x="400" width="400" height="800" fill="${p.b}"/>`;
  if (layout === "block") base += `<rect y="600" width="240" height="200" fill="${p.a}"/>`;
  return (
    `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 800 800" width="800" height="800"><defs>` +
    `<filter id="grain" x="0" y="0" width="100%" height="100%"><feTurbulence type="fractalNoise" baseFrequency="0.85" numOctaves="3" seed="7" result="n"/>` +
    `<feColorMatrix type="saturate" values="0" in="n" result="g"/><feComponentTransfer in="g" result="a"><feFuncA type="table" tableValues="0 0.16"/></feComponentTransfer>` +
    `<feComposite in="a" in2="SourceGraphic" operator="in"/></filter>` +
    `<filter id="ink"><feTurbulence type="fractalNoise" baseFrequency="0.9" numOctaves="2" seed="3"/><feDisplacementMap in="SourceGraphic" scale="1.6"/></filter>` +
    `<pattern id="hatch" width="7" height="7" patternUnits="userSpaceOnUse" patternTransform="rotate(-38)"><path d="M0 0V7" stroke="${p.ink}" stroke-width="1.1"/></pattern>` +
    `<pattern id="hatch2" width="5" height="5" patternUnits="userSpaceOnUse" patternTransform="rotate(52)"><path d="M0 0V5" stroke="${p.ink}" stroke-width="1.5"/></pattern>` +
    `<pattern id="stipple" width="9" height="9" patternUnits="userSpaceOnUse"><circle cx="2" cy="2" r="0.9" fill="${p.ink}"/><circle cx="6.5" cy="6" r="0.7" fill="${p.ink}"/></pattern>` +
    `<pattern id="gridf" width="16" height="16" patternUnits="userSpaceOnUse"><path d="M16 0H0V16" fill="none" stroke="${grid}" stroke-width="0.6" opacity="0.5"/></pattern>` +
    `<pattern id="grid" width="80" height="80" patternUnits="userSpaceOnUse"><rect width="80" height="80" fill="url(#gridf)"/><path d="M80 0H0V80" fill="none" stroke="${grid}" stroke-width="1.1" opacity="0.65"/></pattern>` +
    motif.defs +
    `</defs>${base}<g filter="url(#ink)">${motif.body}</g>` +
    `<text x="24" y="44" fill="${p.ink}" opacity="0.75" ${SERIF} font-size="15">fig. ${caption}</text>` +
    `<rect width="800" height="800" fill="#000" filter="url(#grain)"/></svg>`
  );
}

/** The artwork SVG for a seeded symbol, or null when the symbol has no assigned artwork. */
export function tokenArtSvg(symbol) {
  const entry = TOKEN_ART[symbol];
  if (!entry) return null;
  const [motif, palette, layout, variant] = entry;
  const r = rng(`launchpad-art:${symbol}`);
  const caption = ROMAN[Math.floor(r() * ROMAN.length)];
  return frame(PALETTES[palette], layout, MOTIFS[motif](PALETTES[palette], r, variant), caption);
}
