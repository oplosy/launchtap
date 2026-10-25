import type { Metadata, Viewport } from "next";
import { connection } from "next/server";
import { cookies } from "next/headers";
import localFont from "next/font/local";
import "./globals.css";
import { AppShell } from "@/components/app-shell";
import { RouteTransition } from "@/components/route-transition";
import { THEME_COOKIE } from "@/components/theme-cookie";
import { Providers, WalletRouteGate } from "./providers";

const sans = localFont({
  src: "../node_modules/@fontsource-variable/geist/files/geist-latin-wght-normal.woff2",
  weight: "100 900",
  variable: "--font-sans",
  display: "swap",
});
const mono = localFont({
  src: "../node_modules/@fontsource-variable/geist-mono/files/geist-mono-latin-wght-normal.woff2",
  weight: "100 900",
  variable: "--font-mono",
  display: "swap",
});

export const metadata: Metadata = {
  title: { default: "Launchpad | Onchain launch desk", template: "%s | Launchpad" },
  description:
    "A non-custodial fixed-supply token launchpad with a visible path from launch to liquidity.",
  applicationName: "Launchpad",
  keywords: ["token launchpad", "bonding curve", "non-custodial"],
};

export const viewport: Viewport = {
  themeColor: [
    { media: "(prefers-color-scheme: dark)", color: "#0a0a0b" },
    { media: "(prefers-color-scheme: light)", color: "#f8f8f7" },
  ],
};

export default async function RootLayout({ children }: Readonly<{ children: React.ReactNode }>) {
  // Script nonces are issued per request by proxy.ts, so every document renders dynamically.
  await connection();
  const saved = (await cookies()).get(THEME_COOKIE)?.value;
  const theme = saved === "light" || saved === "dark" ? saved : undefined;
  return (
    <html
      lang="en"
      data-theme={theme}
      data-scroll-behavior="smooth"
      className={`${sans.variable} ${mono.variable}`}
    >
      <body>
        <Providers>
          <AppShell>
            <RouteTransition>
              <WalletRouteGate>{children}</WalletRouteGate>
            </RouteTransition>
          </AppShell>
        </Providers>
      </body>
    </html>
  );
}
