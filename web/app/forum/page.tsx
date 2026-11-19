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
      <h1 className="sr-only">Forum</h1>
      <div className="forum-layout">
        <section className="forum-main" aria-label="Forum posts">
          <ForumTabs active={tab} />
          <ForumView tab={tab} />
        </section>
        <BiggestLaunches />
      </div>
    </div>
  );
}
