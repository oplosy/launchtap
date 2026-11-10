"use client";

import { useCallback, useEffect, useState } from "react";
import { ApiClient, type AuthHeaders, type PublicProfileResponse } from "@/api/client";
import { ApiProblem } from "@/api/problems";
import { problemMessage } from "@/comments/comment-model";
import { Badge, Button, Input } from "@/components/primitives";
import { shortAddress } from "@/token/address";
import {
  MAX_PUBLIC_WALLETS,
  nextChangeLabel,
  normalizeUsername,
  usernameHint,
} from "./public-profile-model";

function errorMessage(error: unknown) {
  return problemMessage(error instanceof ApiProblem ? error.apiCode || undefined : undefined);
}

/**
 * Username and published-wallet settings. A username is shown on comments, the wallet page and the
 * leaderboard only for the wallets the user publishes here.
 */
export function PublicProfileForm({
  apiBaseUrl,
  linkedWallets,
  getAuth,
}: {
  apiBaseUrl: string;
  linkedWallets: readonly string[];
  getAuth: () => Promise<AuthHeaders | null>;
}) {
  const [profile, setProfile] = useState<PublicProfileResponse | null>(null);
  const [username, setUsername] = useState("");
  const [published, setPublished] = useState<string[]>([]);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [saveError, setSaveError] = useState<string | null>(null);
  const [saved, setSaved] = useState(false);
  const [saving, setSaving] = useState(false);
  const [confirmRemove, setConfirmRemove] = useState(false);

  const apply = useCallback((next: PublicProfileResponse) => {
    setProfile(next);
    setUsername(next.username ?? "");
    setPublished((next.public_wallets ?? []).map((w) => w.toLowerCase()));
  }, []);

  useEffect(() => {
    const controller = new AbortController();
    void (async () => {
      try {
        const auth = await getAuth();
        if (!auth) return;
        const next = await new ApiClient({ baseUrl: apiBaseUrl }).getPublicProfile(
          auth,
          controller.signal,
        );
        if (!controller.signal.aborted) apply(next);
      } catch (error) {
        if (!controller.signal.aborted) setLoadError(errorMessage(error));
      }
    })();
    return () => controller.abort();
  }, [apiBaseUrl, apply, getAuth]);

  const toggle = (wallet: string) => {
    setSaved(false);
    setPublished((current) =>
      current.includes(wallet)
        ? current.filter((w) => w !== wallet)
        : current.length >= MAX_PUBLIC_WALLETS
          ? current
          : [...current, wallet],
    );
  };

  const run = async (action: (client: ApiClient, auth: AuthHeaders) => Promise<void>) => {
    setSaving(true);
    setSaveError(null);
    setSaved(false);
    try {
      const auth = await getAuth();
      if (!auth)
        throw new ApiProblem(401, "authentication_required", null, "authentication_failed");
      await action(new ApiClient({ baseUrl: apiBaseUrl }), auth);
      setSaved(true);
    } catch (error) {
      setSaveError(errorMessage(error));
    } finally {
      setSaving(false);
    }
  };

  const save = () =>
    run(async (client, auth) => {
      const name = normalizeUsername(username);
      const body: { username?: string; public_wallets: string[] } = { public_wallets: published };
      if (name && name !== profile?.username) body.username = name;
      apply(await client.putPublicProfile(body, auth));
    });

  const remove = () =>
    run(async (client, auth) => {
      await client.deletePublicProfile(auth);
      setConfirmRemove(false);
      apply(await client.getPublicProfile(auth));
    });

  const hint = usernameHint(username);
  const nextChange = nextChangeLabel(profile?.next_username_change_at ?? null, new Date());
  const full = published.length >= MAX_PUBLIC_WALLETS;

  return (
    <section className="workspace-panel public-profile" aria-labelledby="public-profile-title">
      <div className="panel-head">
        <div>
          <p className="panel-kicker">Public identity</p>
          <h2 id="public-profile-title">Username</h2>
        </div>
        {profile?.username ? <Badge tone="accent">@{profile.username}</Badge> : null}
      </div>
      {loadError ? (
        <p className="comment-error" role="alert">
          {loadError}
        </p>
      ) : null}
      {profile?.banned ? (
        <p className="discovery-notice" role="status">
          This account is banned. You can remove your username, but not change it.
        </p>
      ) : null}
      <form
        className="public-profile-form"
        onSubmit={(event) => {
          event.preventDefault();
          void save();
        }}
      >
        <Input
          label="Username"
          value={username}
          maxLength={20}
          autoComplete="off"
          hint={
            nextChange ?? "3-20 characters: a-z, 0-9 and _. You can change it once every 7 days."
          }
          error={hint ?? undefined}
          onChange={(event) => {
            setSaved(false);
            setUsername(event.target.value);
          }}
          disabled={!profile || profile.banned}
        />
        <fieldset className="public-profile-wallets" disabled={!profile || profile.banned}>
          <legend>
            Show the username on these wallets ({published.length}/{MAX_PUBLIC_WALLETS})
          </legend>
          {linkedWallets.length === 0 ? (
            <p className="ui-field-hint">No linked wallet to publish.</p>
          ) : (
            linkedWallets.map((wallet) => {
              const checked = published.includes(wallet.toLowerCase());
              return (
                <label key={wallet} className="public-profile-wallet">
                  <input
                    type="checkbox"
                    checked={checked}
                    disabled={!checked && full}
                    onChange={() => toggle(wallet.toLowerCase())}
                  />
                  <span className="mono">{shortAddress(wallet)}</span>
                </label>
              );
            })
          )}
          {full ? (
            <p className="ui-field-hint">You can publish at most {MAX_PUBLIC_WALLETS} wallets.</p>
          ) : null}
        </fieldset>
        <p className="ui-field-hint">
          Anyone who saw one of your comments before you set a username saw its posting wallet and
          can link the two.
        </p>
        <div className="public-profile-actions">
          <Button
            type="submit"
            variant="primary"
            loading={saving}
            disabled={!profile || profile.banned || Boolean(hint)}
          >
            Save
          </Button>
          {profile?.username ? (
            confirmRemove ? (
              <>
                <Button variant="danger" loading={saving} onClick={() => void remove()}>
                  Confirm removal
                </Button>
                <Button variant="quiet" onClick={() => setConfirmRemove(false)}>
                  Keep it
                </Button>
              </>
            ) : (
              <Button variant="quiet" onClick={() => setConfirmRemove(true)}>
                Remove username
              </Button>
            )
          ) : null}
        </div>
        {saved ? (
          <p className="ui-field-hint" role="status">
            Saved.
          </p>
        ) : null}
        {saveError ? (
          <p className="comment-error" role="alert">
            {saveError}
          </p>
        ) : null}
      </form>
    </section>
  );
}
