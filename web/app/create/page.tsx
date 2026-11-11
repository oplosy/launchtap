import Link from "next/link";
import { LaunchPanel } from "@/transactions-panel";

export default function CreatePage() {
  return (
    <div className="page-stack create-page">
      <div className="create-page-toolbar">
        <Link className="create-back-button" href="/">
          <span aria-hidden="true">‹</span> Back
        </Link>
        <span className="create-protocol-label">V1</span>
      </div>
      <LaunchPanel />
    </div>
  );
}
