import type { PropsWithChildren } from "react";
import { RewardsTabs } from "@/rewards/rewards-tabs";

/** One page for creator rewards and the profit ranking: a shared heading, then a tab per section. */
export default function RewardsLayout({ children }: PropsWithChildren) {
  return (
    <div className="page-stack rewards-page">
      <section className="page-hero">
        <div>
          <h1>Rewards.</h1>
          <p className="hero-summary">
            Launch a token and earn from every trade on it, for as long as people trade it.
          </p>
        </div>
      </section>
      <RewardsTabs />
      {children}
    </div>
  );
}
