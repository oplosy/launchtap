import type { Metadata } from "next";
import { connection } from "next/server";
import localFont from "next/font/local";
import "./globals.css";
import { AppShell } from "@/components/app-shell";
import { RouteTransition } from "@/components/route-transition";
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

export default async function RootLayout({ children }: Readonly<{ children: React.ReactNode }>) {
  // Script nonces are issued per request by proxy.ts, so every document renders dynamically.
  await connection();
  return (
    <html lang="en" data-theme="dark">
      <body className={`${sans.variable} ${mono.variable}`}>
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
