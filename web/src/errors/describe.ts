import { ApiProblem } from "@/api/problems";
import { decodeTransactionError } from "@/transaction-errors";
import { apiFallbackMessage, apiMessages, contextMessages, type ErrorContext } from "./catalog";

export type DescribedError = { code: string; message: string };

/** An error whose message was written for users and is shown as is. */
export class UserFacingError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "UserFacingError";
  }
}

/** Contexts that fall back to another context's words before the general ones. */
const parentContext: Partial<Record<ErrorContext, ErrorContext>> = {
  vote: "comment",
  report: "comment",
};

function contextual(context: ErrorContext | undefined, code: string) {
  for (let current = context; current; current = parentContext[current]) {
    const words = contextMessages[current];
    if (words[code]) return words[code];
  }
  return undefined;
}

function contextFallback(context: ErrorContext | undefined) {
  for (let current = context; current; current = parentContext[current]) {
    const fallback = contextMessages[current].fallback;
    if (fallback) return fallback;
  }
  return undefined;
}

/**
 * The one way to turn a caught error into the sentence a user sees
 * (docs/specs/2026-11-16-user-error-messages-preflight.md):
 * - API problems by their code, in the words of `context` when it has its own;
 * - app-written `UserFacingError`s as written;
 * - with a `context` (an API-backed feature), anything else as that feature's fallback;
 * - without one (a transaction surface), as a wallet, RPC or contract failure. `submitted` marks
 *   a failure after a transaction hash exists.
 */
export function describeError(
  cause: unknown,
  options: { context?: ErrorContext; submitted?: boolean } = {},
): DescribedError {
  const { context } = options;
  if (cause instanceof ApiProblem) {
    const code = cause.apiCode || "unknown";
    const words = contextual(context, code);
    if (words) return { code, message: words };
    const fallback = contextFallback(context);
    if (!apiMessages[cause.apiCode] && fallback) {
      // An unknown code: the context's own sentence, still with a reference for support.
      const reference = cause.requestId ? ` Reference: ${cause.requestId.slice(0, 12)}.` : "";
      return { code, message: fallback + reference };
    }
    return { code, message: cause.message };
  }
  if (cause instanceof UserFacingError) return { code: "app", message: cause.message };
  if (context) return { code: "unknown", message: contextFallback(context) ?? apiFallbackMessage };
  return decodeTransactionError(cause, { submitted: options.submitted });
}
