import { parseEventLogs, type Abi, type Address, type TransactionReceipt } from "viem";
import type { ApiClient, AuthHeaders } from "@/api/client";
import { ApiProblem } from "@/api/problems";

/**
 * Off-chain details a creator attaches at launch. They are written after the launch receipt
 * through the verified creator endpoints (`PUT /v1/tokens/{token}/metadata` and `/image`); see
 * docs/specs/2026-11-13-launch-image-upload-preflight.md.
 */
export type LaunchExtras = {
  description: string;
  xUrl: string;
  telegramUrl: string;
  file: File | null;
};

export function hasLaunchExtras(extras: LaunchExtras) {
  return Boolean(
    extras.file || extras.description.trim() || extras.xUrl.trim() || extras.telegramUrl.trim(),
  );
}

/**
 * The launched token from the factory's `TokenLaunched` log. Only a log emitted by the reviewed
 * factory for the signing creator counts; anything else in the receipt is ignored.
 */
export function launchedToken(
  receipt: Pick<TransactionReceipt, "logs">,
  abi: Abi,
  factory: Address,
  creator: Address,
): Address | null {
  const events = parseEventLogs({
    abi,
    eventName: "TokenLaunched",
    logs: receipt.logs,
    strict: false,
  }) as unknown as { address: Address; args: { token?: Address; creator?: Address } }[];
  const match = events.find(
    (event) =>
      event.address.toLowerCase() === factory.toLowerCase() &&
      event.args.creator?.toLowerCase() === creator.toLowerCase(),
  );
  return match?.args.token ?? null;
}

/** The step a run starts from; a failed run reports the step to resume at. */
export type AttachStep = "index" | "metadata" | "image";

export class AttachError extends Error {
  constructor(
    readonly step: AttachStep,
    readonly cause: unknown,
  ) {
    super(attachMessage(step, cause));
  }
}

function attachMessage(step: AttachStep, cause: unknown) {
  if (cause instanceof ApiProblem && cause.apiCode === "creator_required")
    return "The signed-in account does not own the launching wallet, so the server refused the details.";
  if (cause instanceof ApiProblem && cause.code === "authentication_required")
    return "Sign in again, then retry.";
  if (cause instanceof ApiProblem && cause.code === "rate_limited")
    return "Too many writes in a minute. Wait a moment, then retry.";
  if (step === "index") return "The new token is not indexed yet. Retry in a moment.";
  if (step === "image") return "The image upload failed. Retry, or set it from the token page.";
  return "Saving the details failed. Retry, or set them from the token page.";
}

type AttachClient = Pick<
  ApiClient,
  "getToken" | "getTokenMetadata" | "updateTokenMetadata" | "updateTokenImage"
>;

export type AttachOptions = {
  client: AttachClient;
  token: Address;
  extras: LaunchExtras;
  getAuth: () => Promise<AuthHeaders | null>;
  from?: AttachStep;
  /** Called as each step starts, so the form can show progress. */
  onStep?: (step: AttachStep) => void;
  pollMs?: number;
  maxWaitMs?: number;
  sleep?: (ms: number) => Promise<void>;
};

const wait = (ms: number) => new Promise<void>((resolve) => setTimeout(resolve, ms));

/**
 * Waits until the API knows the token (the creator check needs the indexed launch), then writes
 * the metadata and the image at revision 0. Throws `AttachError` naming the step to resume from.
 */
export async function attachLaunchMetadata({
  client,
  token,
  extras,
  getAuth,
  from = "index",
  onStep,
  pollMs = 1500,
  maxWaitMs = 60_000,
  sleep = wait,
}: AttachOptions): Promise<void> {
  const order: AttachStep[] = ["index", "metadata", "image"];
  for (const step of order.slice(order.indexOf(from))) {
    if (step === "image" && !extras.file) continue;
    onStep?.(step);
    try {
      if (step === "index") await waitForToken(client, token, pollMs, maxWaitMs, sleep);
      else {
        const auth = await getAuth();
        if (!auth) throw new ApiProblem(401, "authentication_required");
        if (step === "metadata") await writeMetadata(client, token, extras, auth);
        else await client.updateTokenImage(token, extras.file!, 0, auth);
      }
    } catch (cause) {
      throw new AttachError(step, cause);
    }
  }
}

async function waitForToken(
  client: AttachClient,
  token: Address,
  pollMs: number,
  maxWaitMs: number,
  sleep: (ms: number) => Promise<void>,
) {
  for (let waited = 0; ; waited += pollMs) {
    try {
      await client.getToken(token);
      return;
    } catch (cause) {
      const notYet = cause instanceof ApiProblem && cause.status === 404;
      if (!notYet || waited + pollMs > maxWaitMs) throw cause;
    }
    await sleep(pollMs);
  }
}

/**
 * Revision 0 creates the row. A retry after a lost response finds revision 1 already written; that
 * counts as saved only when the stored values are exactly the ones this launch sent.
 */
async function writeMetadata(
  client: AttachClient,
  token: Address,
  extras: LaunchExtras,
  auth: AuthHeaders,
) {
  const body = {
    description: extras.description.trim(),
    x_url: extras.xUrl.trim(),
    telegram_url: extras.telegramUrl.trim(),
  };
  try {
    await client.updateTokenMetadata(token, body, 0, null, auth);
  } catch (cause) {
    if (!(cause instanceof ApiProblem && cause.code === "revision_conflict")) throw cause;
    const stored = (await client.getTokenMetadata(token)).body;
    const same =
      (stored.description ?? "") === body.description &&
      (stored.x_url ?? "") === body.x_url &&
      (stored.telegram_url ?? "") === body.telegram_url;
    if (!same) throw cause;
  }
}
