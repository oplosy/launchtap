import { redirect } from "next/navigation";
import { ExploreHub } from "@/discovery/explore-hub";
import { legacyExploreTarget } from "@/discovery/legacy-routes";

export default async function HomePage({
  searchParams,
}: {
  searchParams?: Promise<Record<string, string | string[] | undefined>>;
}) {
  // Old links put the token list views on the home page (`/?view=…`); they now live on /explore.
  const query = searchParams ? await searchParams : {};
  const params = new URLSearchParams();
  for (const [key, value] of Object.entries(query)) {
    if (typeof value === "string") params.set(key, value);
  }
  const target = legacyExploreTarget(params);
  if (target) redirect(target as never);
  return <ExploreHub />;
}
