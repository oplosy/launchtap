import { apiFallbackMessage, apiMessages, apiReferenceCodes } from "@/errors/catalog";
import type { components } from "./generated";

/** Coarse problem kinds the app branches on; `apiCode` keeps the API's exact code. */
export type ProblemCode =
  | "validation"
  | "authentication_required"
  | "forbidden"
  | "cursor_invalidated"
  | "invalid_cursor"
  | "revision_conflict"
  | "watchlist_full"
  | "rate_limited"
  | "service_unavailable"
  | "network"
  | "unknown";

/** The catalog sentence for an API code, with a short reference when only support can act on it. */
export function apiProblemMessage(apiCode: string, requestId: string | null): string {
  const message = apiMessages[apiCode] ?? apiFallbackMessage;
  const needsReference = !apiMessages[apiCode] || apiReferenceCodes.has(apiCode);
  return needsReference && requestId ? `${message} Reference: ${requestId.slice(0, 12)}.` : message;
}

export class ApiProblem extends Error {
  readonly code: ProblemCode;
  constructor(
    readonly status: number,
    code: ProblemCode,
    readonly detail: string | null = null,
    /** The API's own problem code (the last segment of the problem `type`), or "". */
    readonly apiCode: string = "",
    /** The API request ID (`X-Request-ID`), when the response carried one. */
    readonly requestId: string | null = null,
  ) {
    super(apiProblemMessage(apiCode || statusFallbackCode(status), requestId));
    this.name = "ApiProblem";
    this.code = code;
  }
}

/** The generic API code for a status, for a response that carried no problem body. */
function statusFallbackCode(status: number): string {
  if (status === 0) return "network_unreachable";
  if (status === 400) return "bad_request";
  if (status === 401) return "authentication_failed";
  if (status === 403) return "forbidden";
  if (status === 404) return "not_found";
  if (status === 412) return "revision_conflict";
  if (status === 413) return "payload_too_large";
  if (status === 415) return "unsupported_media_type";
  if (status === 422) return "invalid_request";
  if (status === 429) return "rate_limited";
  if (status === 503) return "service_unavailable";
  if (status === 504) return "timeout";
  if (status >= 500) return "internal_error";
  return "";
}

export function mapProblem(
  status: number,
  body: unknown,
  requestId: string | null = null,
): ApiProblem {
  const model = isErrorModel(body) ? body : undefined;
  const type = model && model.type !== "about:blank" ? (model.type?.split(":").pop() ?? "") : "";
  const code: ProblemCode =
    type === "cursor_invalidated"
      ? "cursor_invalidated"
      : type === "invalid_cursor"
        ? "invalid_cursor"
        : type === "watchlist_full"
          ? "watchlist_full"
          : type === "revision_conflict" || status === 412
            ? "revision_conflict"
            : status === 400
              ? "validation"
              : status === 401
                ? "authentication_required"
                : status === 403
                  ? "forbidden"
                  : status === 429
                    ? "rate_limited"
                    : status >= 500
                      ? "service_unavailable"
                      : "unknown";
  return new ApiProblem(
    status,
    code,
    typeof model?.detail === "string" ? model.detail : null,
    type,
    requestId,
  );
}

/** A request that never got a response (offline, DNS, CORS refusal, connection reset). */
export function networkProblem(): ApiProblem {
  return new ApiProblem(0, "network", null, "network_unreachable");
}

function isErrorModel(value: unknown): value is components["schemas"]["ErrorModel"] {
  return (
    typeof value === "object" &&
    value !== null &&
    typeof (value as { type?: unknown }).type === "string"
  );
}
