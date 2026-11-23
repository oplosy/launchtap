import type { Metadata } from "next";
import Link from "next/link";
import { UnavailableState } from "@/components/primitives";
import { publicConfiguration } from "@/config/public";
import { RewardsView } from "@/rewards/rewards-view";

export const metadata: Metadata = { title: "Rewards" };

/** The Rewards tab: how a creator earns, then what the signed-in account can claim. */
export default function RewardsPage() {
  const configuration = publicConfiguration();
  return (
    <>
      <section className="rewards-how" aria-labelledby="rewards-how-title">
        <h2 id="rewards-how-title" className="sr-only">
          How creators earn
        </h2>
        <ol className="rewards-steps">
          <li>
            <span className="rewards-step-index">01</span>
            <h3>Launch</h3>
            <p>Create a token. The wallet that launches it is its creator.</p>
          </li>
          <li>
            <span className="rewards-step-index">02</span>
            <h3>Earn on every trade</h3>
            <p>
              Half of the 1% curve fee, 0.5% of every buy and sell, goes to the creator. On engine
              v2 tokens the creator also shares the Uniswap pool fee after graduation.
            </p>
          </li>
          <li>
            <span className="rewards-step-index">03</span>
            <h3>Claim anytime</h3>
            <p>
              Fees collect on chain until you claim them, and claims keep working while trading is
              paused and after graduation.
            </p>
          </li>
        </ol>
        <p className="rewards-more">
          <Link href="/docs#economics">Fees and economics in the docs</Link>
        </p>
      </section>
      {/* The fixture build may sign in with a test session instead of Privy (src/e2e). */}
      {configuration.status === "ready" &&
      (configuration.privyAppId || process.env.NEXT_PUBLIC_E2E_FIXTURE === "1") ? (
        <RewardsView />
      ) : (
        <UnavailableState
          title="Rewards unavailable"
          description="Account access is not configured for this environment. Public launch data remains available."
        />
      )}
    </>
  );
}
