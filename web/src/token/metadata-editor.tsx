"use client";
/* Form state resets when the authoritative token snapshot changes. */
/* eslint-disable react-hooks/set-state-in-effect */
/* eslint-disable @next/next/no-img-element */

import { useEffect, useRef, useState } from "react";
import { useIdentityToken, usePrivy } from "@privy-io/react-auth";
import { ApiClient, resolveApiAssetUrl } from "@/api/client";
import { ApiProblem } from "@/api/problems";
import { publicConfiguration } from "@/config/public";
import { Button, ErrorState, Input, SafeImage } from "@/components/primitives";
import { useWalletReadiness } from "@/wallet/readiness";

type Props = {
  token: {
    address: string;
    description: string;
    x_url: string;
    telegram_url: string;
    image_url: string;
  };
  onConflict: () => Promise<unknown> | void;
};

const maxImageBytes = 5 * 1024 * 1024;
const imageTypes = new Set(["image/png", "image/jpeg", "image/webp"]);

function safeHttps(value: string) {
  if (!value.trim()) return true;
  if (new TextEncoder().encode(value).length > 2048) return false;
  try {
    const url = new URL(value);
    return url.protocol === "https:" && Boolean(url.hostname) && !url.username && !url.password;
  } catch {
    return false;
  }
}

async function hasValidImageSignature(file: File) {
  const bytes = new Uint8Array(await file.slice(0, 12).arrayBuffer());
  const png =
    bytes.length >= 8 && [137, 80, 78, 71, 13, 10, 26, 10].every((v, i) => bytes[i] === v);
  const jpeg = bytes.length >= 3 && bytes[0] === 255 && bytes[1] === 216 && bytes[2] === 255;
  const webp =
    bytes.length >= 12 &&
    new TextDecoder().decode(bytes.slice(0, 4)) === "RIFF" &&
    new TextDecoder().decode(bytes.slice(8, 12)) === "WEBP";
  return png || jpeg || webp;
}

export type MetadataConflictRecovery = {
  description: string;
  xUrl: string;
  telegramUrl: string;
  metadataRevision: number;
  metadataEtag: string | null;
  imageRevision: number;
};

export async function recoverMetadataConflict(
  client: ApiClient,
  address: string,
  onConflict: () => Promise<unknown> | void,
): Promise<MetadataConflictRecovery> {
  const refreshedToken = await Promise.resolve(onConflict()).catch(() => undefined);
  const [metadataResult, imageResult] = await Promise.allSettled([
    client.getTokenMetadata(address),
    client.getTokenImageRevision(address),
  ]);
  let description = "";
  let xUrl = "";
  let telegramUrl = "";
  if (refreshedToken && typeof refreshedToken === "object" && "description" in refreshedToken) {
    const latest = refreshedToken as {
      description?: string;
      x_url?: string;
      telegram_url?: string;
    };
    description = latest.description ?? "";
    xUrl = latest.x_url ?? "";
    telegramUrl = latest.telegram_url ?? "";
  }
  let metadataRevision = 0;
  let metadataEtag: string | null = null;
  if (metadataResult.status === "fulfilled") {
    description = metadataResult.value.body.description ?? "";
    xUrl = metadataResult.value.body.x_url ?? "";
    telegramUrl = metadataResult.value.body.telegram_url ?? "";
    metadataRevision = metadataResult.value.body.revision;
    metadataEtag = metadataResult.value.etag;
  }
  return {
    description,
    xUrl,
    telegramUrl,
    metadataRevision,
    metadataEtag,
    imageRevision: imageResult.status === "fulfilled" ? imageResult.value.revision : 0,
  };
}

/**
 * The editor for the wallet that launched the token only. Visitors never see it, and its reads
 * (a 404 for a token without metadata) are not made for them. Showing it is not authorization:
 * the API still checks every write against the signed-in account's linked creator wallet.
 */
export function CreatorMetadataEditor({ creator, ...props }: Props & { creator: string }) {
  const readiness = useWalletReadiness();
  if (!readiness.address || readiness.address.toLowerCase() !== creator.toLowerCase()) return null;
  return <MetadataEditor {...props} />;
}

export function MetadataEditor({ token, onConflict }: Props) {
  const configuration = publicConfiguration();
  const { authenticated, getAccessToken, login } = usePrivy();
  const { identityToken } = useIdentityToken();
  const readiness = useWalletReadiness();
  const [description, setDescription] = useState(token.description);
  const [xUrl, setXUrl] = useState(token.x_url);
  const [telegramUrl, setTelegramUrl] = useState(token.telegram_url);
  const [file, setFile] = useState<File | null>(null);
  const [preview, setPreview] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const [metadataRevision, setMetadataRevision] = useState(0);
  const [metadataEtag, setMetadataEtag] = useState<string | null>(null);
  const [imageRevision, setImageRevision] = useState(0);
  const [reviewRequired, setReviewRequired] = useState(false);
  const fileRef = useRef<HTMLInputElement>(null);
  const currentImageUrl = resolveApiAssetUrl(configuration.apiBaseUrl, token.image_url);

  useEffect(() => {
    setDescription(token.description);
    setXUrl(token.x_url);
    setTelegramUrl(token.telegram_url);
    setMetadataRevision(0);
    setMetadataEtag(null);
    setImageRevision(0);
  }, [token.address, token.description, token.telegram_url, token.x_url]);
  useEffect(() => {
    if (configuration.status !== "ready" || !configuration.apiBaseUrl) return;
    const controller = new AbortController();
    const client = new ApiClient({ baseUrl: configuration.apiBaseUrl });
    void client
      .getTokenMetadata(token.address, controller.signal)
      .then((result) => {
        setMetadataRevision(result.body.revision);
        setMetadataEtag(result.etag);
      })
      .catch(() => {
        // Tokens without an off-chain metadata row start at revision zero.
      });
    void client
      .getTokenImageRevision(token.address, controller.signal)
      .then((result) => {
        setImageRevision(result.revision);
      })
      .catch(() => {
        // Tokens without an image start at revision zero.
      });
    return () => controller.abort();
  }, [configuration.apiBaseUrl, configuration.status, token.address]);
  useEffect(() => {
    return () => {
      if (preview) URL.revokeObjectURL(preview);
    };
  }, [preview]);

  const chooseFile = async (candidate: File | undefined) => {
    setError(null);
    setNotice(null);
    if (!candidate) return;
    if (!imageTypes.has(candidate.type) || candidate.size < 1 || candidate.size > maxImageBytes) {
      setFile(null);
      setPreview(null);
      setError("Choose a PNG, JPEG, or WebP image up to 5 MiB.");
      return;
    }
    if (!(await hasValidImageSignature(candidate))) {
      setFile(null);
      setPreview(null);
      setError("The image bytes do not match the declared image type.");
      return;
    }
    if (preview) URL.revokeObjectURL(preview);
    setFile(candidate);
    setPreview(URL.createObjectURL(candidate));
  };

  const submit = async () => {
    setError(null);
    setNotice(null);
    if (reviewRequired) {
      setError("Review the refreshed metadata before resubmitting.");
      return;
    }
    if (
      new TextEncoder().encode(description).length > 2000 ||
      !safeHttps(xUrl) ||
      !safeHttps(telegramUrl)
    ) {
      setError(
        "Description must be at most 2,000 bytes and links must be HTTPS URLs up to 2,048 bytes.",
      );
      return;
    }
    if (configuration.status !== "ready" || !configuration.apiBaseUrl) {
      setError("The reviewed API deployment is unavailable.");
      return;
    }
    if (!authenticated || !identityToken || !readiness.address) {
      setError("Sign in and connect the wallet you want the server to verify.");
      return;
    }
    setSaving(true);
    try {
      const accessToken = await getAccessToken();
      if (!accessToken) throw new Error("Sign in again to refresh the access token.");
      const client = new ApiClient({ baseUrl: configuration.apiBaseUrl });
      const auth = { accessToken, identityToken };
      const metadataResult = await client.updateTokenMetadata(
        token.address,
        { description, x_url: xUrl, telegram_url: telegramUrl },
        metadataRevision,
        metadataEtag,
        auth,
      );
      setMetadataRevision(metadataResult.body.revision);
      setMetadataEtag(metadataResult.etag);
      if (file) {
        const imageResult = await client.updateTokenImage(token.address, file, imageRevision, auth);
        setImageRevision(imageResult.body.revision);
      }
      setNotice("Saved. The server accepted the verified creator update.");
    } catch (cause) {
      if (cause instanceof ApiProblem && cause.code === "revision_conflict") {
        const client = new ApiClient({ baseUrl: configuration.apiBaseUrl });
        const refreshed = await recoverMetadataConflict(client, token.address, onConflict);
        setDescription(refreshed.description);
        setXUrl(refreshed.xUrl);
        setTelegramUrl(refreshed.telegramUrl);
        setMetadataRevision(refreshed.metadataRevision);
        setMetadataEtag(refreshed.metadataEtag);
        setImageRevision(refreshed.imageRevision);
        setFile(null);
        if (preview) {
          URL.revokeObjectURL(preview);
          setPreview(null);
        }
        setReviewRequired(true);
        setNotice(
          "This token changed in another session. The latest token, metadata, and image revision were loaded; review them before resubmitting.",
        );
      } else setError(cause instanceof Error ? cause.message : "The metadata update was rejected.");
    } finally {
      setSaving(false);
    }
  };

  return (
    <section className="workspace-panel metadata-editor" aria-labelledby="metadata-editor-title">
      <div className="panel-head">
        <div>
          <p className="panel-kicker">Creator controls</p>
          <h2 id="metadata-editor-title">Edit token metadata</h2>
        </div>
        <span className="mono">Server authorization required</span>
      </div>
      <p className="metadata-editor-note">
        Your browser can preview and validate inputs, but only the API can authorize the linked
        creator wallet. Every update uses the latest revision with <code>If-Match</code>.
      </p>
      {!authenticated ? (
        <div className="editor-auth-state">
          <p>Sign in with Privy to request a creator edit.</p>
          <Button onClick={() => login()}>Sign in</Button>
        </div>
      ) : !readiness.address ? (
        <div className="editor-auth-state">
          <p>Connect a wallet. A connected address is not creator authorization.</p>
        </div>
      ) : (
        <div className="metadata-editor-grid">
          <div className="editor-form">
            <label className="ui-field">
              <span>Description</span>
              <textarea
                className="ui-input ui-textarea"
                value={description}
                onChange={(event) => setDescription(event.target.value)}
                aria-describedby="description-hint"
              />
            </label>
            <p id="description-hint" className="ui-field-hint">
              {new TextEncoder().encode(description).length.toLocaleString("en-US")}/2,000 bytes
            </p>
            <Input
              label="X URL"
              type="url"
              value={xUrl}
              onChange={(event) => setXUrl(event.target.value)}
              placeholder="https://x.com/..."
            />
            <Input
              label="Telegram URL"
              type="url"
              value={telegramUrl}
              onChange={(event) => setTelegramUrl(event.target.value)}
              placeholder="https://t.me/..."
            />
            <label className="ui-field">
              <span>Token image</span>
              <input
                ref={fileRef}
                type="file"
                accept="image/png,image/jpeg,image/webp"
                onChange={(event) => void chooseFile(event.target.files?.[0])}
              />
              <span className="ui-field-hint">PNG, JPEG, or WebP · max 5 MiB</span>
            </label>
            {error ? <ErrorState title="Update not sent" description={error} /> : null}
            {notice ? (
              <p className="discovery-notice" role="status">
                {notice}
              </p>
            ) : null}
            {reviewRequired ? (
              <Button variant="secondary" onClick={() => setReviewRequired(false)}>
                I reviewed the latest values
              </Button>
            ) : (
              <Button variant="primary" loading={saving} onClick={() => void submit()}>
                Save metadata
              </Button>
            )}
          </div>
          <div className="metadata-preview" aria-label="Metadata preview">
            <p className="panel-kicker">Preview</p>
            <div className="metadata-preview-image">
              {preview ? (
                <img src={preview} alt="Selected token image preview" />
              ) : (
                <SafeImage
                  src={currentImageUrl}
                  alt="Current token image"
                  fallbackLabel="Select an image to preview"
                />
              )}
            </div>
            <p>{description || "No description supplied."}</p>
            <span className="mono">{xUrl || telegramUrl || "No external links"}</span>
          </div>
        </div>
      )}
    </section>
  );
}
