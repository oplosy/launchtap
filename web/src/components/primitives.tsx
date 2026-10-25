"use client";

import { Check, CircleNotch, WarningCircle, X } from "@phosphor-icons/react";
import { useEffect, useId, useRef, useState } from "react";

export type ButtonProps = React.ButtonHTMLAttributes<HTMLButtonElement> & {
  variant?: "primary" | "secondary" | "quiet" | "danger";
  size?: "sm" | "md" | "lg";
  loading?: boolean;
};

export function Button({
  className = "",
  variant = "secondary",
  size = "md",
  loading = false,
  children,
  disabled,
  ...props
}: ButtonProps) {
  return (
    <button
      className={`ui-button ui-button-${variant} ui-button-${size} ${className}`}
      disabled={disabled || loading}
      aria-busy={loading || undefined}
      {...props}
    >
      {loading ? <CircleNotch className="ui-spinner" size={16} aria-hidden="true" /> : null}
      <span>{children}</span>
    </button>
  );
}

export function Input({
  label,
  hint,
  error,
  className = "",
  id: suppliedId,
  ...props
}: React.InputHTMLAttributes<HTMLInputElement> & { label: string; hint?: string; error?: string }) {
  const generatedId = useId();
  const id = suppliedId ?? generatedId;
  const hintId = hint ? `${id}-hint` : undefined;
  const errorId = error ? `${id}-error` : undefined;
  return (
    <div className="ui-field">
      <label htmlFor={id}>{label}</label>
      <input
        id={id}
        className={`ui-input ${error ? "ui-input-error" : ""} ${className}`}
        aria-invalid={error ? "true" : undefined}
        aria-describedby={[hintId, errorId].filter(Boolean).join(" ") || undefined}
        {...props}
      />
      {hint ? (
        <p id={hintId} className="ui-field-hint">
          {hint}
        </p>
      ) : null}
      {error ? (
        <p id={errorId} className="ui-field-error" role="alert">
          {error}
        </p>
      ) : null}
    </div>
  );
}

export function Dialog({
  open,
  title,
  description,
  onClose,
  children,
}: {
  open: boolean;
  title: string;
  description?: string;
  onClose: () => void;
  children: React.ReactNode;
}) {
  const ref = useRef<HTMLDialogElement>(null);
  const titleId = useId();
  useEffect(() => {
    const dialog = ref.current;
    if (!dialog) return;
    if (open && !dialog.open) dialog.showModal();
    if (!open && dialog.open) dialog.close();
  }, [open]);
  return (
    <dialog
      ref={ref}
      className="ui-dialog"
      aria-labelledby={titleId}
      onCancel={(event) => {
        event.preventDefault();
        onClose();
      }}
      onClose={onClose}
    >
      <div className="ui-dialog-inner">
        <div className="ui-dialog-header">
          <div>
            <h2 id={titleId}>{title}</h2>
            {description ? <p>{description}</p> : null}
          </div>
          <button className="ui-icon-button" aria-label="Close dialog" onClick={onClose}>
            <X size={18} />
          </button>
        </div>
        <div className="ui-dialog-body">{children}</div>
      </div>
    </dialog>
  );
}

export function Sheet({
  open,
  title,
  onClose,
  children,
}: {
  open: boolean;
  title: string;
  onClose: () => void;
  children: React.ReactNode;
}) {
  return (
    <Dialog open={open} title={title} onClose={onClose}>
      <div className="ui-sheet-content">{children}</div>
    </Dialog>
  );
}

export type TabItem = {
  value: string;
  label: string;
  disabled?: boolean;
  panel: React.ReactNode;
};

export function getNextTabIndex(
  tabs: ReadonlyArray<Pick<TabItem, "disabled">>,
  currentIndex: number,
  key: "ArrowLeft" | "ArrowRight" | "Home" | "End",
) {
  const enabled = tabs.flatMap((tab, index) => (tab.disabled ? [] : [index]));
  if (enabled.length === 0) return -1;
  if (key === "Home") return enabled[0] ?? -1;
  if (key === "End") return enabled[enabled.length - 1] ?? -1;
  const currentPosition = Math.max(0, enabled.indexOf(currentIndex));
  const direction = key === "ArrowRight" ? 1 : -1;
  return enabled[(currentPosition + direction + enabled.length) % enabled.length] ?? -1;
}

export function Tabs({
  tabs,
  value,
  onChange,
  ariaLabel = "View selection",
}: {
  tabs: TabItem[];
  value: string;
  onChange: (value: string) => void;
  ariaLabel?: string;
}) {
  const tablistId = useId();
  const tabRefs = useRef<Array<HTMLButtonElement | null>>([]);
  const selectTab = (index: number) => {
    const next = tabs[index];
    if (!next || next.disabled) return;
    onChange(next.value);
    tabRefs.current[index]?.focus();
  };
  return (
    <div className="ui-tabs-shell">
      <div className="ui-tabs" role="tablist" aria-label={ariaLabel} id={tablistId}>
        {tabs.map((tab, index) => {
          const tabId = `${tablistId}-tab-${index}`;
          const panelId = `${tablistId}-panel-${index}`;
          return (
            <button
              key={tab.value}
              ref={(element) => {
                tabRefs.current[index] = element;
              }}
              id={tabId}
              type="button"
              role="tab"
              aria-selected={tab.value === value}
              aria-controls={panelId}
              tabIndex={tab.value === value ? 0 : -1}
              disabled={tab.disabled}
              className={tab.value === value ? "is-active" : ""}
              onClick={() => selectTab(index)}
              onKeyDown={(event) => {
                const key = event.key;
                if (key !== "ArrowLeft" && key !== "ArrowRight" && key !== "Home" && key !== "End")
                  return;
                event.preventDefault();
                const nextIndex = getNextTabIndex(tabs, index, key);
                if (nextIndex >= 0) selectTab(nextIndex);
              }}
            >
              {tab.label}
            </button>
          );
        })}
      </div>
      {tabs.map((tab, index) => {
        const tabId = `${tablistId}-tab-${index}`;
        const panelId = `${tablistId}-panel-${index}`;
        return (
          <div
            key={panelId}
            id={panelId}
            role="tabpanel"
            aria-labelledby={tabId}
            hidden={tab.value !== value}
            tabIndex={tab.value === value ? 0 : -1}
            className="ui-tabpanel"
          >
            {tab.panel}
          </div>
        );
      })}
    </div>
  );
}

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

export function SafeExternalLink({
  href,
  children,
  className = "",
}: {
  href: string;
  children: React.ReactNode;
  className?: string;
}) {
  const safeHref = (() => {
    try {
      const url = new URL(href);
      return url.protocol === "https:" ? url.toString() : null;
    } catch {
      return null;
    }
  })();
  if (!safeHref)
    return (
      <span className={`ui-disabled-link ${className}`} aria-label="Link unavailable">
        {children}
      </span>
    );
  return (
    <a href={safeHref} target="_blank" rel="noopener noreferrer" className={className}>
      {children}
    </a>
  );
}

export function SafeImage({
  src,
  alt,
  fallbackLabel = "Image unavailable",
  className = "",
}: {
  src?: string | null;
  alt: string;
  fallbackLabel?: string;
  className?: string;
}) {
  const [failed, setFailed] = useState(false);
  const validSrc = (() => {
    if (!src) return null;
    try {
      const url = new URL(src);
      const local =
        url.hostname === "localhost" || url.hostname === "127.0.0.1" || url.hostname === "[::1]";
      return url.protocol === "https:" || (url.protocol === "http:" && local)
        ? url.toString()
        : null;
    } catch {
      return null;
    }
  })();
  if (!validSrc || failed)
    return (
      <span
        className={`ui-image-fallback ${className}`}
        role="img"
        aria-label={`${alt}. ${fallbackLabel}`}
      >
        <span aria-hidden="true" className="ui-image-fallback-letter">
          {alt.trim().charAt(0).toUpperCase() || "•"}
        </span>
      </span>
    );
  return (
    // SafeImage validates remote input before this intentionally unoptimized external image.
    /* eslint-disable-next-line @next/next/no-img-element */
    <img
      src={validSrc}
      alt={alt}
      className={className}
      loading="lazy"
      referrerPolicy="no-referrer"
      onError={() => setFailed(true)}
    />
  );
}
