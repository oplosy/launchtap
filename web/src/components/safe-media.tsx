"use client";

import { useState } from "react";

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
