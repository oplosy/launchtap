import type { Metadata } from "next";
import { CreateWorkspace } from "@/components/create-workspace";

export const metadata: Metadata = {
  title: "Create a token",
  description: "Launch a fixed-supply token on a bonding curve that graduates to Uniswap.",
};

export default function CreatePage() {
  return (
    <div className="page-stack create-page">
      <CreateWorkspace />
    </div>
  );
}
