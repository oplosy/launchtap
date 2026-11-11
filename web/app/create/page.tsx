import Link from "next/link";
import { EngineToggle } from "@/components/engine-toggle";
import { LaunchPanel } from "@/transactions-panel";

export default function CreatePage() {
  return (
    <div className="page-stack create-page">
      <div className="create-page-toolbar">
        <Link className="ui-button ui-button-secondary ui-button-sm create-back-button" href="/">
          <span aria-hidden="true">‹</span> Back
        </Link>
        <EngineToggle />
      </div>
      <LaunchPanel />
    </div>
  );
}
