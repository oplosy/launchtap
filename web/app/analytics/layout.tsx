import type { Metadata } from "next";
import type { ReactNode } from "react";

/* The analytics page is a client component, so its title is set here. */
export const metadata: Metadata = {
  title: "Analytics",
  description: "Protocol activity across launches, trades, volume, graduations and revenue.",
};

export default function AnalyticsLayout({ children }: { children: ReactNode }) {
  return children;
}
