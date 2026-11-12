import { useCallback, useEffect, useRef, useState } from "react";
import { ApiClient, type AuthHeaders, type TelegramStatus } from "@/api/client";
import { ApiProblem } from "@/api/problems";
import { Button } from "@/components/primitives";
import {
  isTelegramLink,
  telegramProblemMessage,
  thresholdToWei,
  weiToThreshold,
} from "./telegram-model";

type CardState =
  | { status: "loading" }
  | { status: "error" }
  | { status: "off" }
  | { status: "ready"; telegram: TelegramStatus };

type Form = { graduations: boolean; largeTrades: boolean; threshold: string };

const formFrom = (t: TelegramStatus): Form => ({
  graduations: t.graduations,
  largeTrades: t.large_trades,
  threshold: weiToThreshold(t.min_trade_wei),
});

/**
 * Rendered only inside the client InboxView.
 * Telegram alerts for the signed-in reader (docs/specs/2026-11-12-telegram-alerts-design.md):
 * connect a chat through a one-time deep link, choose what is sent, disconnect. Hidden when the
 * API has no bot configured (503).
 */
export function TelegramCard({
  baseUrl,
  auth,
}: {
  baseUrl: string;
  auth: () => Promise<AuthHeaders>;
}) {
  const [state, setState] = useState<CardState>({ status: "loading" });
  const [form, setForm] = useState<Form | null>(null);
  const [pending, setPending] = useState<{ url: string; expiresAt: string } | null>(null);
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState<{ text: string; error: boolean } | null>(null);
  const generation = useRef(0);

  const load = useCallback(() => {
    const run = ++generation.current;
    void auth()
      .then((headers) => new ApiClient({ baseUrl }).getTelegram(headers))
      .then((telegram) => {
        if (run !== generation.current) return;
        setState({ status: "ready", telegram });
        setForm(formFrom(telegram));
        if (telegram.linked) setPending(null);
      })
      .catch((error: unknown) => {
        if (run !== generation.current) return;
        setState({
          status: error instanceof ApiProblem && error.status === 503 ? "off" : "error",
        });
      });
  }, [auth, baseUrl]);

  useEffect(() => {
    load();
    return () => {
      generation.current += 1;
    };
  }, [load]);

  const run = useCallback(
    async (action: (client: ApiClient, headers: AuthHeaders) => Promise<void>) => {
      setBusy(true);
      setMessage(null);
      try {
        await action(new ApiClient({ baseUrl }), await auth());
      } catch (error) {
        setMessage({
          text: telegramProblemMessage(error instanceof ApiProblem ? error.apiCode : undefined),
          error: true,
        });
        if (error instanceof ApiProblem && error.apiCode === "telegram_not_linked") load();
      } finally {
        setBusy(false);
      }
    },
    [auth, baseUrl, load],
  );

  const connect = () =>
    run(async (client, headers) => {
      const link = await client.createTelegramLink(headers);
      if (!isTelegramLink(link.url)) throw new Error("unexpected link");
      setPending({ url: link.url, expiresAt: link.expires_at });
      window.open(link.url, "_blank", "noopener,noreferrer");
    });

  const save = (event: { preventDefault(): void }) => {
    event.preventDefault();
    if (!form) return;
    const wei = thresholdToWei(form.threshold);
    if (!wei) {
      setMessage({ text: telegramProblemMessage("invalid_threshold"), error: true });
      return;
    }
    void run(async (client, headers) => {
      const telegram = await client.updateTelegram(
        { graduations: form.graduations, large_trades: form.largeTrades, min_trade_wei: wei },
        headers,
      );
      setState({ status: "ready", telegram });
      setForm(formFrom(telegram));
      setMessage({ text: "Saved.", error: false });
    });
  };

  const disconnect = () =>
    run(async (client, headers) => {
      await client.unlinkTelegram(headers);
      setPending(null);
      load();
    });

  if (state.status === "off") return null;
  return (
    <section className="telegram-card" aria-labelledby="telegram-card-title">
      <h2 id="telegram-card-title">Telegram alerts</h2>
      {state.status === "loading" ? (
        <p role="status">Loading Telegram settings…</p>
      ) : state.status === "error" ? (
        <p role="alert">
          Telegram settings could not be loaded. <Button onClick={load}>Retry</Button>
        </p>
      ) : !state.telegram.linked ? (
        <div className="telegram-card-body">
          <p>
            Get these notifications as Telegram messages. Connecting opens Telegram; press Start
            there to finish.
          </p>
          {pending ? (
            <p role="status">
              Waiting for Start in Telegram.{" "}
              <a href={pending.url} target="_blank" rel="noopener noreferrer">
                Open Telegram again
              </a>{" "}
              (the link works until {new Date(pending.expiresAt).toLocaleTimeString()}).
            </p>
          ) : null}
          <div className="telegram-card-actions">
            <Button variant="primary" onClick={() => void connect()} disabled={busy}>
              {pending ? "New link" : "Connect Telegram"}
            </Button>
            {pending ? (
              <Button onClick={load} disabled={busy}>
                I pressed Start
              </Button>
            ) : null}
          </div>
        </div>
      ) : form ? (
        <form className="telegram-card-body" onSubmit={save}>
          <p>Connected. Alerts cover the tokens you watch.</p>
          <label className="telegram-card-check">
            <input
              type="checkbox"
              checked={form.graduations}
              onChange={(e) => setForm({ ...form, graduations: e.target.checked })}
            />
            Graduations
          </label>
          <label className="telegram-card-check">
            <input
              type="checkbox"
              checked={form.largeTrades}
              onChange={(e) => setForm({ ...form, largeTrades: e.target.checked })}
            />
            Trades of at least
            <input
              className="telegram-card-threshold mono"
              inputMode="decimal"
              aria-label="Smallest trade in ETH"
              value={form.threshold}
              onChange={(e) => setForm({ ...form, threshold: e.target.value })}
            />
            ETH
          </label>
          <div className="telegram-card-actions">
            <Button variant="primary" type="submit" disabled={busy}>
              Save
            </Button>
            <Button type="button" onClick={() => void disconnect()} disabled={busy}>
              Disconnect
            </Button>
          </div>
        </form>
      ) : null}
      {message ? (
        <p role={message.error ? "alert" : "status"} className="telegram-card-message">
          {message.text}
        </p>
      ) : null}
    </section>
  );
}
