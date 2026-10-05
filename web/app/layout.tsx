import type { Metadata } from "next";
import { connection } from "next/server";
import localFont from "next/font/local";
import "./globals.css";
import { AppShell } from "@/components/app-shell";
import { RouteTransition } from "@/components/route-transition";
import { Providers, WalletRouteGate } from "./providers";

const display = localFont({
  src: "../node_modules/@fontsource/ibm-plex-sans/files/ibm-plex-sans-latin-400-normal.woff2",
  variable: "--font-display",
  display: "swap",
});
const body = localFont({
  src: "../node_modules/@fontsource/ibm-plex-sans/files/ibm-plex-sans-latin-400-normal.woff2",
  variable: "--font-body",
  display: "swap",
});
const mono = localFont({
  src: "../node_modules/@fontsource/ibm-plex-mono/files/ibm-plex-mono-latin-500-normal.woff2",
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

export default async function RootLayout({ children }: Readonly<{ children: React.ReactNode }>) {
  // Script nonces are issued per request by proxy.ts, so every document renders dynamically.
  await connection();
  return (
    <html lang="en" data-theme="dark">
      <body className={`${display.variable} ${body.variable} ${mono.variable}`}>
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
