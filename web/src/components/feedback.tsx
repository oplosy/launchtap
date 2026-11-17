"use client";

import { Check, CircleNotch, WarningCircle, X } from "@phosphor-icons/react";

import { SafeExternalLink } from "./safe-media";

export function Badge({
  tone = "neutral",
  children,
}: {
  tone?: "neutral" | "success" | "warning" | "danger" | "accent";
  children: React.ReactNode;
}) {
  return (
    <span className={`ui-badge ui-badge-${tone}`}>
      <span>{children}</span>
    </span>
  );
}

export function Skeleton({ className = "" }: { className?: string }) {
  return <span className={`ui-skeleton ${className}`} aria-hidden="true" />;
}

export function EmptyState({
  title,
  description,
  action,
}: {
  title: string;
  description: string;
  action?: React.ReactNode;
}) {
  return (
    <section className="ui-state ui-empty" aria-live="polite">
      <span className="ui-state-mark" aria-hidden="true">
        0
      </span>
      <h2>{title}</h2>
      <p>{description}</p>
      {action ? <div className="ui-state-action">{action}</div> : null}
    </section>
  );
}

export function ErrorState({
  title = "Something went wrong",
  description,
  action,
}: {
  title?: string;
  description: string;
  action?: React.ReactNode;
}) {
  return (
    <section className="ui-state ui-error-state" role="alert">
      <WarningCircle size={24} aria-hidden="true" />
      <div>
        <h2>{title}</h2>
        <p>{description}</p>
        {action ? <div className="ui-state-action">{action}</div> : null}
      </div>
    </section>
  );
}

export function UnavailableState({
  title = "Unavailable",
  description,
}: {
  title?: string;
  description: string;
}) {
  return (
    <section className="ui-state ui-unavailable" aria-live="polite">
      <span className="ui-state-mark" aria-hidden="true">
        /
      </span>
      <div>
        <h2>{title}</h2>
        <p>{description}</p>
      </div>
    </section>
  );
}

export function Toast({
  title,
  description,
  tone = "neutral",
  onDismiss,
}: {
  title: string;
  description?: string;
  tone?: "neutral" | "success" | "warning" | "danger";
  onDismiss: () => void;
}) {
  return (
    <div className={`ui-toast ui-toast-${tone}`} role="status">
      <div className="ui-toast-icon">
        {tone === "success" ? (
          <Check size={16} />
        ) : tone === "danger" ? (
          <WarningCircle size={16} />
        ) : (
          <CircleNotch size={16} />
        )}
      </div>
      <div>
        <strong>{title}</strong>
        {description ? <p>{description}</p> : null}
      </div>
      <button className="ui-icon-button" aria-label="Dismiss notification" onClick={onDismiss}>
        <X size={16} />
      </button>
    </div>
  );
}

export function TransactionStatus({
  status,
  hash,
  explorerUrl,
}: {
  status:
    | "idle"
    | "signing"
    | "submitted"
    | "mined"
    | "indexed"
    | "safe"
    | "finalized"
    | "stale"
    | "failed";
  hash?: string;
  explorerUrl?: string;
}) {
  const labels: Record<typeof status, string> = {
    idle: "Ready",
    signing: "Awaiting signature",
    submitted: "Submitted",
    mined: "Mined",
    indexed: "Indexed",
    safe: "Safe",
    finalized: "Finalized",
    stale: "Stale",
    failed: "Failed",
  };
  const tone =
    status === "failed"
      ? "danger"
      : status === "stale"
        ? "warning"
        : ["mined", "indexed", "safe", "finalized"].includes(status)
          ? "success"
          : "neutral";
  return (
    <div className="transaction-status">
      <Badge tone={tone}>{labels[status]}</Badge>
      {hash ? <span className="mono transaction-hash">{hash}</span> : null}
      {hash && explorerUrl ? (
        <SafeExternalLink href={explorerUrl} className="ui-inline-link">
          View transaction
        </SafeExternalLink>
      ) : null}
    </div>
  );
}
