"use client";

import Link from "next/link";
import { useContext, useEffect, useRef, useState } from "react";
import type { Address } from "viem";
import { ApiClient } from "@/api/client";
import { publicConfiguration } from "@/config/public";
import { Button } from "@/components/primitives";
import { describeError } from "@/errors/describe";
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
  | { status: "done"; token: Address; image: boolean };

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
  // The values each launch was signed with, by token; a retry saves these, not later edits to
  // the form or the details of a later launch.
  const launched = useRef(new Map<Address, LaunchExtras>());
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
    const saved = launched.current.get(token) ?? extras;
    launched.current.set(token, saved);
    try {
      await attachLaunchMetadata({
        client,
        token,
        extras: saved,
        from,
        getAuth: auth.getAuth,
        onStep: (step) => setAttach({ status: "running", token, step }),
      });
      setAttach({ status: "done", token, image: Boolean(saved.file) });
    } catch (cause) {
      const step = cause instanceof AttachError ? cause.step : from;
      const message =
        cause instanceof AttachError
          ? cause.message
          : describeError(cause, { context: "launch-details" }).message;
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

/**
 * The token image picker, drawn as the preview card's image square: the native picker sits
 * invisibly over it, so a click, a keyboard press or a dropped file all reach the real input.
 */
export function LaunchImageSlot({
  extras,
  onChange,
}: {
  extras: LaunchExtrasState;
  onChange: () => void;
}) {
  return (
    <div
      className="launch-preview-image launch-image-slot"
      data-filled={extras.preview ? "" : undefined}
      data-invalid={extras.fileError ? "" : undefined}
    >
      <input
        className="launch-image-input"
        type="file"
        accept="image/png,image/jpeg,image/webp"
        aria-label="Token image"
        aria-describedby="launch-image-hint"
        aria-invalid={extras.fileError ? "true" : undefined}
        onChange={(event) => {
          onChange();
          void extras.chooseFile(event.target.files?.[0]);
        }}
      />
      {extras.preview ? (
        <>
          {/* A local object URL of the chosen file; SafeImage only accepts remote URLs. */}
          {/* eslint-disable-next-line @next/next/no-img-element */}
          <img src={extras.preview} alt="Token image preview" />
          <span className="launch-image-slot-replace" aria-hidden="true">
            Replace
          </span>
        </>
      ) : (
        <span className="launch-image-slot-mark" aria-hidden="true">
          <span className="launch-image-slot-plus">+</span>
          <span>Add image</span>
        </span>
      )}
    </div>
  );
}

/** The image slot's hint (for screen readers) and its error, under the preview's name. */
export function LaunchImageNote({ extras }: { extras: LaunchExtrasState }) {
  return (
    <>
      <p id="launch-image-hint" className="sr-only">
        Optional. PNG, JPEG, or WebP, up to 5 MiB. Uploaded after the launch confirms.
      </p>
      {extras.fileError ? (
        <p className="ui-field-error launch-image-error" role="alert">
          {extras.fileError}
        </p>
      ) : null}
    </>
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
        className={`ui-input ui-textarea launch-description ${extras.descriptionError ? "ui-input-error" : ""}`}
        rows={1}
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
        Launched, with your {attach.image ? "image and details" : "details"} saved. {tokenLink}
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
