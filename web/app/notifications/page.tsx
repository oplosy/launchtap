import type { Metadata } from "next";
import { UnavailableState } from "@/components/primitives";
import { publicConfiguration } from "@/config/public";
import { InboxView } from "@/notifications/inbox-view";

export const metadata: Metadata = { title: "Notifications" };

export default function NotificationsPage() {
  const configuration = publicConfiguration();
  return (
    <div className="page-stack">
      <section className="page-hero">
        <div>
          <div className="section-kicker">Inbox</div>
          <h1>Your notifications.</h1>
          <p className="hero-summary">
            Graduations and trades of 0.5 ETH or more on the tokens you watch, from the last 30
            days. The same inbox appears on every device you sign in with.
          </p>
        </div>
      </section>
      <section className="workspace-panel">
        {configuration.status === "ready" && configuration.privyAppId ? (
          <InboxView />
        ) : (
          <UnavailableState
            title="Inbox unavailable"
            description="Account access is not configured for this environment."
          />
        )}
      </section>
    </div>
  );
}
