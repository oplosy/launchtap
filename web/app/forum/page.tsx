import type { Metadata } from "next";
import { parseForumTab } from "@/forum/forum-model";
import { BiggestLaunches, ForumTabs, ForumView } from "@/forum/forum-view";

export const metadata: Metadata = {
  title: "Forum",
  description: "Every launch with a community, ranked by what is moving.",
};

export default async function ForumPage({
  searchParams,
}: {
  searchParams?: Promise<{ tab?: string | string[] }>;
}) {
  const query = searchParams ? await searchParams : undefined;
  const tab = parseForumTab(query?.tab);
  return (
    <div className="page-stack forum-page">
      <section className="page-hero">
        <div>
          <div className="section-kicker">Memestock</div>
          <h1>Every launch with a community, ranked by what is moving.</h1>
          <p className="hero-summary">
            Posts from holders and creators across every launch. To post, open a token&apos;s page.
          </p>
        </div>
      </section>
      <div className="forum-layout">
        <section className="workspace-panel forum-main" aria-label="Forum posts">
          <ForumTabs active={tab} />
          <ForumView tab={tab} />
        </section>
        <BiggestLaunches />
      </div>
    </div>
  );
}
