"use client";

import Link from "next/link";
import { useState } from "react";
import { ApiClient, type ReportReason } from "@/api/client";
import { ApiProblem } from "@/api/problems";
import { Badge } from "@/components/primitives";
import { shortAddress } from "@/token/address";
import { WatchlistAuthContext } from "@/watchlist/use-watchlist";
import {
  authorLabel,
  nextVote,
  problemMessage,
  scoreLabel,
  voteProblemMessage,
  type CommentItem,
  type VoteValue,
} from "./comment-model";

const REPORT_REASONS: ReportReason[] = ["spam", "scam", "abuse", "other"];

export function errorCode(error: unknown) {
  return error instanceof ApiProblem ? error.apiCode || undefined : undefined;
}

function formatTime(iso: string) {
  const date = new Date(iso);
  return Number.isNaN(date.getTime()) ? iso : date.toLocaleString();
}

export type AuthValue = React.ContextType<typeof WatchlistAuthContext>;
export type ClientRef = React.RefObject<ApiClient>;

export function CommentRow({
  item,
  auth,
  client,
}: {
  item: CommentItem;
  auth: AuthValue;
  client: ClientRef;
}) {
  const label = authorLabel(item.author);
  const [reported, setReported] = useState(false);
  const [message, setMessage] = useState<string | null>(null);
  const report = async (reason: ReportReason) => {
    const headers = await auth.getAuth();
    if (!headers) {
      auth.login();
      return;
    }
    try {
      await client.current.reportComment(item.id, reason, headers);
      setReported(true);
      setMessage(null);
    } catch (error) {
      setMessage(problemMessage(errorCode(error)));
    }
  };
  return (
    <li className="comment-row">
      <div className="comment-meta">
        {label.walletHref ? (
          <Link href={label.walletHref as never} title={item.author.wallet ?? undefined}>
            {label.primary}
          </Link>
        ) : (
          <span>{label.primary}</span>
        )}
        {item.author.username && item.author.wallet ? (
          <span className="comment-wallet">{shortAddress(item.author.wallet)}</span>
        ) : null}
        {item.author.role === "creator" ? <Badge tone="accent">Creator</Badge> : null}
        <time dateTime={item.created_at}>{formatTime(item.created_at)}</time>
        {auth.available ? (
          reported ? (
            <span className="comment-reported">Reported</span>
          ) : (
            <details className="comment-report">
              <summary>Report</summary>
              <div role="menu" aria-label="Report reason">
                {REPORT_REASONS.map((reason) => (
                  <button
                    key={reason}
                    type="button"
                    role="menuitem"
                    onClick={() => void report(reason)}
                  >
                    {reason.charAt(0).toUpperCase() + reason.slice(1)}
                  </button>
                ))}
              </div>
            </details>
          )
        ) : null}
      </div>
      <p className="comment-body">{item.body}</p>
      <VoteControl item={item} auth={auth} client={client} onMessage={setMessage} />
      {message ? (
        <p className="comment-error" role="alert">
          {message}
        </p>
      ) : null}
    </li>
  );
}

/**
 * Up/down arrows with the score. Holders (checked by the API) vote; the page remembers votes cast
 * here, but not earlier ones (docs/specs/2026-11-12-comment-votes-design.md).
 */
function VoteControl({
  item,
  auth,
  client,
  onMessage,
}: {
  item: CommentItem;
  auth: AuthValue;
  client: ClientRef;
  onMessage: (message: string | null) => void;
}) {
  const [score, setScore] = useState(item.score);
  const [vote, setVote] = useState<VoteValue>(0);
  const [busy, setBusy] = useState(false);
  const cast = async (clicked: 1 | -1) => {
    if (busy) return;
    const headers = await auth.getAuth();
    if (!headers) {
      auth.login();
      return;
    }
    setBusy(true);
    try {
      const result = await client.current.voteComment(item.id, nextVote(vote, clicked), headers);
      setScore(result.score);
      setVote(result.vote === 1 || result.vote === -1 ? result.vote : 0);
      onMessage(null);
    } catch (error) {
      onMessage(voteProblemMessage(errorCode(error)));
    } finally {
      setBusy(false);
    }
  };
  if (!auth.available) {
    return <span className="comment-score mono">{scoreLabel(score)}</span>;
  }
  return (
    <div className="comment-vote" role="group" aria-label={`Vote, ${scoreLabel(score)}`}>
      <button
        type="button"
        className="comment-vote-button"
        aria-label="Upvote"
        aria-pressed={vote === 1}
        disabled={busy}
        onClick={() => void cast(1)}
      >
        <span aria-hidden="true">▲</span>
      </button>
      <span className="comment-score mono">{score}</span>
      <button
        type="button"
        className="comment-vote-button"
        aria-label="Downvote"
        aria-pressed={vote === -1}
        disabled={busy}
        onClick={() => void cast(-1)}
      >
        <span aria-hidden="true">▼</span>
      </button>
    </div>
  );
}
