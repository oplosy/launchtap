import { mapProblem, networkProblem } from "./problems";
import type { AuthHeaders } from "./response-types";

/** Sends API requests: base URL, auth headers, sensitive-query guard and problem mapping. */
export class ApiTransport {
  protected readonly fetchImpl: typeof globalThis.fetch;
  constructor(protected readonly options: { baseUrl: string; fetch?: typeof globalThis.fetch }) {
    this.fetchImpl = options.fetch ?? ((input, init) => globalThis.fetch(input, init));
  }

  async request<T>(path: string, init: RequestInit = {}, auth?: AuthHeaders): Promise<T> {
    const response = await this.send(path, init, auth);
    return (await response.json()) as T;
  }

  protected async requestNoContent(path: string, init: RequestInit, auth: AuthHeaders) {
    await this.send(path, init, auth);
  }

  protected async requestWithResponse<T>(path: string, init: RequestInit, auth?: AuthHeaders) {
    const response = await this.send(path, init, auth);
    return { body: (await response.json()) as T, etag: response.headers.get("ETag") };
  }

  /** Sends one request and returns the successful response; a non-2xx status throws its problem. */
  protected async send(
    path: string,
    init: RequestInit,
    auth?: AuthHeaders,
    accept = "application/json",
  ): Promise<Response> {
    const url = new URL(path, this.options.baseUrl);
    for (const key of url.searchParams.keys()) {
      if (/authorization|token|signature|secret|private|password/i.test(key))
        throw new Error("Sensitive values are not permitted in request URLs");
    }
    const headers = new Headers(init.headers);
    headers.set("Accept", accept);
    if (auth?.accessToken) headers.set("Authorization", `Bearer ${auth.accessToken}`);
    if (auth?.identityToken) headers.set("privy-id-token", auth.identityToken);
    let response: Response;
    try {
      response = await this.fetchImpl(url, { ...init, headers });
    } catch (cause) {
      // An abort is the caller's decision, not a failure to report.
      if (cause instanceof DOMException && cause.name === "AbortError") throw cause;
      if (init.signal?.aborted) throw cause;
      throw networkProblem();
    }
    if (!response.ok) {
      let body: unknown;
      try {
        body = await response.json();
      } catch {
        body = undefined;
      }
      throw mapProblem(response.status, body, response.headers?.get("X-Request-ID") ?? null);
    }
    return response;
  }
}
