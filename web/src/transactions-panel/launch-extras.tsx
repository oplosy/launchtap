"use client";

import Link from "next/link";
import { useContext, useEffect, useRef, useState } from "react";
import type { Address } from "viem";
import { ApiClient } from "@/api/client";
import { publicConfiguration } from "@/config/public";
import { Button } from "@/components/primitives";
import { WatchlistAuthContext } from "@/watchlist/use-watchlist";
import {
  acceptableImageFile,
  descriptionTooLong,
  hasValidImageSignature,
  maxDescriptionBytes,
} from "@/token/image-file";
import {
  AttachError,
  attachLaunchMetadata,
  hasLaunchExtras,
  type AttachStep,
  type LaunchExtras,
} from "./launch-metadata";

type AttachState =
  | { status: "idle" }
  | { status: "running"; token: Address; step: AttachStep }
  | { status: "failed"; token: Address; step: AttachStep; message: string }
  | { status: "done"; token: Address };

/**
 * The image, description and links a creator attaches at launch, and the post-receipt writes that
 * save them (docs/specs/2026-11-13-launch-image-upload-preflight.md).
 */
export function useLaunchExtras(links: { xUrl: string; telegramUrl: string }) {
  const auth = useContext(WatchlistAuthContext);
  const [file, setFile] = useState<File | null>(null);
  const [preview, setPreview] = useState<string | null>(null);
  const [fileError, setFileError] = useState<string | null>(null);
  const [description, setDescription] = useState("");
  const [attach, setAttach] = useState<AttachState>({ status: "idle" });
  // The values a launch was signed with; a retry saves these, not later edits to the form.
  const launched = useRef<LaunchExtras | null>(null);
  useEffect(() => {
    return () => {
      if (preview) URL.revokeObjectURL(preview);
    };
  }, [preview]);

  const extras: LaunchExtras = { description, file, ...links };
  const wanted = hasLaunchExtras(extras);
  const descriptionError = descriptionTooLong(description)
    ? `Keep the description within ${maxDescriptionBytes.toLocaleString("en-US")} bytes.`
    : undefined;

  /** Validates before accepting: a rejected file clears the previous choice. */
  const chooseFile = async (candidate: File | undefined) => {
    setFileError(null);
    setPreview(null);
    setFile(null);
    if (!candidate) return;
    if (!acceptableImageFile(candidate)) {
      setFileError("Choose a PNG, JPEG, or WebP image up to 5 MiB.");
      return;
    }
    if (!(await hasValidImageSignature(candidate))) {
      setFileError("The file's bytes are not a PNG, JPEG, or WebP image.");
      return;
    }
    setFile(candidate);
    setPreview(URL.createObjectURL(candidate));
  };

  /** Runs (or resumes) the writes for a launched token. Never throws; the state carries failures. */
  const run = async (token: Address, from: AttachStep = "index") => {
    const configuration = publicConfiguration();
    if (configuration.status !== "ready" || !configuration.apiBaseUrl) {
      setAttach({
        status: "failed",
        token,
        step: from,
        message: "The API is unavailable, so the details were not saved.",
      });
      return;
    }
    const client = new ApiClient({ baseUrl: configuration.apiBaseUrl });
    if (from === "index" || !launched.current) launched.current = extras;
    const saved = launched.current;
    try {
      await attachLaunchMetadata({
        client,
        token,
        extras: saved,
        from,
        getAuth: auth.getAuth,
        onStep: (step) => setAttach({ status: "running", token, step }),
      });
      setAttach({ status: "done", token });
    } catch (cause) {
      const step = cause instanceof AttachError ? cause.step : from;
      const message = cause instanceof Error ? cause.message : "Saving the details failed.";
      setAttach({ status: "failed", token, step, message });
    }
  };

  return {
    auth,
    extras,
    wanted,
    /** Something to upload and no session to upload it with. */
    needsSignIn: wanted && !auth.authenticated,
    file,
    preview,
    fileError,
    chooseFile,
    description,
    setDescription,
    descriptionError,
    attach,
    run,
  };
}

export type LaunchExtrasState = ReturnType<typeof useLaunchExtras>;

export function LaunchImageField({
  extras,
  onChange,
}: {
  extras: LaunchExtrasState;
  onChange: () => void;
}) {
  return (
    <div className="ui-field launch-image-field">
      <label htmlFor="launch-image-file">Token image</label>
      <input
        id="launch-image-file"
        className="launch-image-input"
        type="file"
        accept="image/png,image/jpeg,image/webp"
        aria-describedby="launch-image-hint"
        aria-invalid={extras.fileError ? "true" : undefined}
        onChange={(event) => {
          onChange();
          void extras.chooseFile(event.target.files?.[0]);
        }}
      />
      <p id="launch-image-hint" className="ui-field-hint">
        Optional. PNG, JPEG, or WebP, up to 5 MiB. Uploaded after the launch confirms.
      </p>
      {extras.fileError ? (
        <p className="ui-field-error" role="alert">
          {extras.fileError}
        </p>
      ) : null}
      {extras.preview ? (
        // A local object URL of the chosen file; SafeImage only accepts remote URLs.
        /* eslint-disable-next-line @next/next/no-img-element */
        <img src={extras.preview} alt="Token image preview" className="launch-image-preview" />
      ) : null}
    </div>
  );
}

export function LaunchDescriptionField({
  extras,
  onChange,
}: {
  extras: LaunchExtrasState;
  onChange: () => void;
}) {
  const bytes = new TextEncoder().encode(extras.description).length;
  return (
    <div className="ui-field">
      <label htmlFor="launch-description">Description</label>
      <textarea
        id="launch-description"
        className={`ui-input ui-textarea ${extras.descriptionError ? "ui-input-error" : ""}`}
        value={extras.description}
        placeholder="Optional. What is this token about?"
        aria-describedby="launch-description-hint"
        aria-invalid={extras.descriptionError ? "true" : undefined}
        onChange={(event) => {
          onChange();
          extras.setDescription(event.target.value);
        }}
      />
      <p id="launch-description-hint" className="ui-field-hint">
        {bytes.toLocaleString("en-US")}/{maxDescriptionBytes.toLocaleString("en-US")} bytes
      </p>
      {extras.descriptionError ? (
        <p className="ui-field-error" role="alert">
          {extras.descriptionError}
        </p>
      ) : null}
    </div>
  );
}

/** Shown in place of "Review launch" while details wait for a session to be saved with. */
export function LaunchSignIn({ extras }: { extras: LaunchExtrasState }) {
  if (!extras.auth.available)
    return (
      <p className="transaction-error" role="alert">
        Sign-in is unavailable here, so the image, description and links cannot be saved. Remove
        them to launch.
      </p>
    );
  return (
    <>
      <p className="ui-field-hint">
        The image, description and links are saved by the server for the creator wallet, so they
        need a signed-in account that owns this wallet.
      </p>
      <Button variant="primary" size="lg" onClick={() => extras.auth.login()}>
        Sign in to attach details
      </Button>
    </>
  );
}

const STEP_LABEL: Record<AttachStep, string> = {
  index: "Waiting for the new token to be indexed…",
  metadata: "Saving the description and links…",
  image: "Uploading the image…",
};

export function LaunchAttachStatus({ extras }: { extras: LaunchExtrasState }) {
  const { attach } = extras;
  if (attach.status === "idle") return null;
  const tokenLink = <Link href={`/token/${attach.token}` as never}>Open the token page</Link>;
  if (attach.status === "running")
    return (
      <p className="discovery-notice" role="status">
        {STEP_LABEL[attach.step]}
      </p>
    );
  if (attach.status === "done")
    return (
      <p className="discovery-notice" role="status">
        Launched, with your image and details saved. {tokenLink}
      </p>
    );
  return (
    <div className="launch-attach-failed" role="alert">
      <p className="transaction-error">
        The token launched, but its details were not saved. {attach.message}
      </p>
      <div className="transaction-actions">
        {extras.auth.authenticated ? (
          <Button variant="primary" onClick={() => void extras.run(attach.token, attach.step)}>
            Retry
          </Button>
        ) : (
          <Button variant="primary" onClick={() => extras.auth.login()}>
            Sign in to retry
          </Button>
        )}
        {tokenLink}
      </div>
    </div>
  );
}
