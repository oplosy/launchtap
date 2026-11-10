import type { components } from "./generated";

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
  | "unknown";

export class ApiProblem extends Error {
  readonly code: ProblemCode;
  constructor(
    readonly status: number,
    code: ProblemCode,
    readonly detail: string | null = null,
    /** The API's own problem code (the last segment of the problem `type`), or "". */
    readonly apiCode: string = "",
  ) {
    super(
      {
        validation: "Check the submitted values.",
        authentication_required: "Sign in to continue.",
        forbidden: "This action is not authorized for the selected wallet.",
        cursor_invalidated: "This list changed. Refreshing from the latest snapshot.",
        invalid_cursor: "This page marker is invalid. Refresh the list.",
        revision_conflict: "This record changed. Review the latest version before saving.",
        watchlist_full: "Your watchlist is full. Remove a token before adding another.",
        rate_limited: "Too many requests. Try again shortly.",
        service_unavailable: "The API is temporarily unavailable.",
        unknown: "The API request could not be completed.",
      }[code],
    );
    this.name = "ApiProblem";
    this.code = code;
  }
}

export function mapProblem(status: number, body: unknown): ApiProblem {
  const model = isErrorModel(body) ? body : undefined;
  const type = model?.type?.split(":").pop() ?? "";
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
  );
}

function isErrorModel(value: unknown): value is components["schemas"]["ErrorModel"] {
  return (
    typeof value === "object" &&
    value !== null &&
    typeof (value as { type?: unknown }).type === "string"
  );
}
