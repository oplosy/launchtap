"use client";

import { useCallback, useContext, useEffect, useRef, useState } from "react";
import { ApiClient, type CommentEligibilityResponse } from "@/api/client";
import { ApiProblem } from "@/api/problems";
import { Badge, Button } from "@/components/primitives";
import { shortAddress } from "@/token/address";
import { WatchlistAuthContext } from "@/watchlist/use-watchlist";
import {
  BODY_RULE_MESSAGES,
  checkBody,
  codePointCount,
  MAX_BODY_CODE_POINTS,
} from "./comment-rules";
import { appendPage, mergeFirstPage, problemMessage, type CommentItem } from "./comment-model";

import { CommentRow, errorCode, type AuthValue, type ClientRef } from "./comment-row";
/** Window over which live `comment` hints collapse into one first-page refetch. */
export const COMMENT_REFETCH_COALESCE_MS = 500;
const PAGE_SIZE = 20;

/**
 * Token comments: public list with "Load more", a composer for holders and the creator, and a
 * report menu. `liveSignal` changes on every `comment` hint (and reconnect) for this token; each
 * burst of changes triggers one first-page refetch.
 */
export function CommentPanel({
  tokenAddress,
  apiBaseUrl,
  liveSignal,
}: {
  tokenAddress: string;
  apiBaseUrl: string;
  liveSignal: number;
}) {
  const auth = useContext(WatchlistAuthContext);
  const [items, setItems] = useState<CommentItem[]>([]);
  const [count, setCount] = useState(0);
  const [nextCursor, setNextCursor] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [loadingMore, setLoadingMore] = useState(false);
  const [listError, setListError] = useState<string | null>(null);
  const client = useRef(new ApiClient({ baseUrl: apiBaseUrl }));
  useEffect(() => {
    client.current = new ApiClient({ baseUrl: apiBaseUrl });
  }, [apiBaseUrl]);

  const loadFirstPage = useCallback(
    async (fresh: boolean, signal?: AbortSignal) => {
      const page = await client.current.listTokenComments(
        tokenAddress,
        { limit: PAGE_SIZE },
        { signal, cache: fresh ? "no-store" : undefined },
      );
      const first = page.items ?? [];
      setItems((current) => (fresh ? mergeFirstPage(current, first) : first));
      setCount(page.comment_count);
      // A fresh first page keeps the older pages already shown, so their cursor stays valid.
      if (!fresh) setNextCursor(page.next_cursor ?? null);
      setListError(null);
    },
    [tokenAddress],
  );

  useEffect(() => {
    // The panel is keyed by token address, so the initial state is already empty and loading.
    const controller = new AbortController();
    loadFirstPage(false, controller.signal)
      .catch((error: unknown) => {
        if (!controller.signal.aborted) setListError(problemMessage(errorCode(error)));
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false);
      });
    return () => controller.abort();
  }, [loadFirstPage]);

  // Coalesce live hints: the first hint arms a timer; hints inside the window ride along.
  const liveTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const firstSignal = useRef(liveSignal);
  useEffect(() => {
    if (liveSignal === firstSignal.current) return;
    if (liveTimer.current) return;
    liveTimer.current = setTimeout(() => {
      liveTimer.current = null;
      void loadFirstPage(true).catch(() => undefined);
    }, COMMENT_REFETCH_COALESCE_MS);
  }, [liveSignal, loadFirstPage]);
  useEffect(
    () => () => {
      if (liveTimer.current) clearTimeout(liveTimer.current);
    },
    [],
  );

  const loadMore = async () => {
    if (!nextCursor) return;
    setLoadingMore(true);
    try {
      const page = await client.current.listTokenComments(tokenAddress, {
        cursor: nextCursor,
        limit: PAGE_SIZE,
      });
      setItems((current) => appendPage(current, page.items ?? []));
      setNextCursor(page.next_cursor ?? null);
    } catch (error) {
      setListError(problemMessage(errorCode(error)));
    } finally {
      setLoadingMore(false);
    }
  };

  const onPosted = (comment: CommentItem) => {
    setItems((current) => appendPage([comment], current));
    setCount((n) => n + 1);
  };

  return (
    <section className="workspace-panel comment-panel" aria-labelledby="comments-title">
      <div className="panel-head">
        <h2 id="comments-title">Comments</h2>
        <Badge>{count}</Badge>
      </div>
      <CommentComposer
        auth={auth}
        client={client}
        tokenAddress={tokenAddress}
        onPosted={onPosted}
      />
      {listError ? (
        <p className="comment-error" role="alert">
          {listError}
        </p>
      ) : null}
      {loading ? (
        <p className="comment-empty">Loading comments…</p>
      ) : items.length === 0 && !listError ? (
        <p className="comment-empty">No comments yet.</p>
      ) : (
        <ol className="comment-list" aria-label="Comments">
          {items.map((item) => (
            <CommentRow key={item.id} item={item} auth={auth} client={client} />
          ))}
        </ol>
      )}
      {nextCursor ? (
        <div className="history-pagination">
          <span>{items.length} shown</span>
          <Button loading={loadingMore} onClick={() => void loadMore()}>
            Load more
          </Button>
        </div>
      ) : null}
    </section>
  );
}

type Eligibility =
  | { state: "idle" }
  | { state: "loading" }
  | { state: "error"; message: string }
  | { state: "ready"; data: CommentEligibilityResponse };

function CommentComposer({
  auth,
  client,
  tokenAddress,
  onPosted,
}: {
  auth: AuthValue;
  client: ClientRef;
  tokenAddress: string;
  onPosted: (comment: CommentItem) => void;
}) {
  const [eligibility, setEligibility] = useState<Eligibility>({ state: "idle" });
  const [text, setText] = useState("");
  const [wallet, setWallet] = useState("");
  const [posting, setPosting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const signedIn = auth.available && auth.ready && auth.authenticated;

  useEffect(() => {
    if (!signedIn) {
      // Sign-out must drop the previous account's eligibility before the next sign-in.
      // eslint-disable-next-line react-hooks/set-state-in-effect
      setEligibility({ state: "idle" });
      return;
    }
    const controller = new AbortController();
    setEligibility({ state: "loading" });
    void (async () => {
      try {
        const headers = await auth.getAuth();
        if (!headers)
          throw new ApiProblem(401, "authentication_required", null, "authentication_failed");
        const data = await client.current.getCommentEligibility(
          tokenAddress,
          headers,
          controller.signal,
        );
        if (controller.signal.aborted) return;
        setEligibility({ state: "ready", data });
        setWallet(data.eligible_wallets?.[0]?.address ?? "");
      } catch (err) {
        if (!controller.signal.aborted)
          setEligibility({ state: "error", message: problemMessage(errorCode(err)) });
      }
    })();
    return () => controller.abort();
  }, [auth, client, signedIn, tokenAddress]);

  if (!auth.available) {
    return (
      <p className="comment-note">Sign-in is not available here, so comments are read-only.</p>
    );
  }
  if (!signedIn) {
    return (
      <div className="comment-note">
        <Button variant="primary" onClick={() => auth.login()} disabled={!auth.ready}>
          Sign in to comment
        </Button>
      </div>
    );
  }
  if (eligibility.state === "idle" || eligibility.state === "loading") {
    return <p className="comment-note">Checking whether you can comment…</p>;
  }
  if (eligibility.state === "error") {
    return (
      <p className="comment-error" role="alert">
        {eligibility.message}
      </p>
    );
  }
  if (eligibility.data.banned) {
    return <p className="comment-note">{problemMessage("banned")}</p>;
  }
  const wallets = eligibility.data.eligible_wallets ?? [];
  if (wallets.length === 0) {
    return <p className="comment-note">Hold this token to comment.</p>;
  }

  const check = checkBody(text);
  const used = codePointCount(text.trim());
  const ruleMessage = text && !check.ok ? BODY_RULE_MESSAGES[check.rule] : null;
  const submit = async (event: { preventDefault: () => void }) => {
    event.preventDefault();
    if (!check.ok || !wallet) return;
    setPosting(true);
    setError(null);
    try {
      const headers = await auth.getAuth();
      if (!headers) {
        auth.login();
        return;
      }
      const posted = await client.current.postTokenComment(
        tokenAddress,
        { body: check.body, wallet },
        headers,
      );
      setText("");
      onPosted(posted);
    } catch (err) {
      setError(problemMessage(errorCode(err)));
    } finally {
      setPosting(false);
    }
  };

  return (
    <form className="comment-composer" onSubmit={(event) => void submit(event)}>
      {wallets.length > 1 ? (
        <label className="comment-wallet-select">
          Post as
          <select value={wallet} onChange={(event) => setWallet(event.target.value)}>
            {wallets.map((w) => (
              <option key={w.address} value={w.address}>
                {shortAddress(w.address)} ({w.role})
              </option>
            ))}
          </select>
        </label>
      ) : null}
      <label className="comment-textarea">
        <span className="sr-only">Comment</span>
        <textarea
          value={text}
          rows={3}
          placeholder="Say something about this token"
          onChange={(event) => setText(event.target.value)}
          aria-invalid={ruleMessage ? "true" : undefined}
        />
      </label>
      <div className="comment-composer-foot">
        <span
          className={used > MAX_BODY_CODE_POINTS ? "comment-counter is-over" : "comment-counter"}
        >
          {used}/{MAX_BODY_CODE_POINTS}
        </span>
        {ruleMessage ? <span className="comment-rule">{ruleMessage}</span> : null}
        <Button type="submit" variant="primary" loading={posting} disabled={!check.ok}>
          Post
        </Button>
      </div>
      {error ? (
        <p className="comment-error" role="alert">
          {error}
        </p>
      ) : null}
    </form>
  );
}
