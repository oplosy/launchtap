# Scale-out path and CDN

How each service of the Compose deployment (`deploy/compose.yaml`) scales past one host-sized
instance, what limits it, and how to put a CDN in front without breaking rate limiting. Every
statement names the code or configuration that makes it true; re-check those when they change.

## Summary

| Service | Scales how | Limit |
|---------|-----------|-------|
| `web` | Horizontally, stateless | none in the app; add replicas behind the proxy like `api` |
| `api` | Horizontally: `docker compose up -d --scale api=N` | per-replica rate limits, Postgres connections |
| `indexer` | Exactly one writer per chain | Postgres advisory lock; scale the RPC provider and database instead |
| `telegram` | Exactly one replica | Postgres advisory lock |
| `postgres` | Vertically, then read replicas | single primary; all writes go through the indexer |
| `proxy` (Caddy) | One per host; several hosts behind a load balancer or CDN | TLS certificates are per host |

## API

The API holds no request state that must be shared:

- **Live updates.** Each replica opens its own Postgres `LISTEN` connection
  (`storepostgres.ListenAPIRefreshReady`, wired in `cmd/api/main.go` `listenRefreshHints`) and
  fans refresh hints out to its own SSE clients. Every replica therefore sees every hint; a client
  may reconnect to any replica.
- **Proxy distribution.** Caddy resolves the `api` service name every 10 seconds (`dynamic a`
  in `deploy/Caddyfile`) and balances with `least_conn`, which suits long-lived SSE streams. A
  removed replica leaves the pool within one refresh interval. Verified 2026-11-14 with two
  replicas behind the repository Caddyfile: 20 requests split 12/8, and 10/10 succeeded after one
  replica was removed.
- **Rate limits are per replica.** `RateLimiter` and `StreamLimiter`
  (`internal/apiserver/ratelimit.go`) keep their buckets in process memory. With `N` replicas a
  client can reach up to `N ×` `API_RATE_LIMIT_PER_MINUTE` / `API_RATE_LIMIT_BURST` and
  `N ×` `API_SSE_MAX_PER_CLIENT`. Divide those values by `N` when scaling out, or move the limit to
  the edge (CDN or load balancer) once there is one. A shared store (Redis) is deliberately not
  added before it is needed.
- **Database connections.** Each replica opens up to `DATABASE_MAX_CONNS` connections (store
  default when unset). Keep `N × DATABASE_MAX_CONNS` plus the indexer's pool and its ownership
  connection below the Postgres `max_connections`, with headroom for migrations and backups.

## Indexer

`storepostgres.AcquireOwnership` takes a chain-scoped `pg_try_advisory_lock`. A second indexer for
the same chain fails at start with `indexer ownership already held` and exits; it never writes.
This is intentional: the ledger and reorg handling assume a single writer per chain.

- **Failover** is a restart: Compose `restart: unless-stopped` brings the process back, and it
  takes the lock as soon as the previous session's connection is gone. Do not run a crash-looping
  second replica as a "standby".
- **Throughput** comes from log-fetch concurrency (`Discoverer.WithLogConcurrency`), the RPC
  provider's limits and Postgres write speed, not from more indexer processes.

## Telegram worker

Runs from the opt-in `telegram` profile and holds its own advisory lock; exactly one replica
(`deploy/README.md`).

## Postgres

One primary takes every write. The next steps, in order: a larger instance; a managed Postgres
with automated backups (the backup/restore procedure in `deploy/check.ps1` stays the tested
baseline); read replicas for the API only once measured read load needs them. The indexer and
migrations must always use the primary.

## Multiple hosts

Run the same Compose project on each API/web host with `DATABASE_URL` pointing at the shared
primary, and the `indexer` and `telegram` services on exactly one host. Put a load balancer or
CDN in front of the hosts' `proxy` services.

## CDN in front of the proxy

Not configured in the repository: choosing and contracting a CDN is an infrastructure-owner
decision. When one is added:

1. **Cache only what is safe to cache.** Next.js serves `/_next/static/*` with
   `Cache-Control: public, max-age=31536000, immutable`; let the CDN honor origin headers
   rather than forcing a site-wide TTL. The API sets its own `Cache-Control` per route
   (`private, no-store` for wallet-scoped reads such as the watchlist).
2. **Never cache or buffer `/v1/events`.** It is a server-sent event stream; disable caching and
   response buffering for that path.
3. **Keep the client IP for rate limiting.** Behind a CDN, Caddy's TCP peer is a CDN edge, so
   every user would share that edge's rate-limit bucket. Two changes, both with the CDN's
   published egress ranges:
   - Caddy global options, so Caddy keeps the CDN's incoming `X-Forwarded-For` instead of
     replacing it with the edge address:

     ```caddyfile
     {
     	servers {
     		trusted_proxies static <CDN egress CIDRs>
     	}
     }
     ```

   - `API_TRUSTED_PROXY_CIDRS` in `deploy/compose.yaml`: append the same ranges to the private
     ranges. Caddy appends the edge address to `X-Forwarded-For`, and the API
     (`clientAddress` in `internal/apiserver/ratelimit.go`) walks the chain right to left, skipping
     trusted hops. Without the CDN ranges it stops at the edge address and still rate-limits the
     edge, not the client.

   Keep the range list current from the provider's published source: a stale list either loses
   client IPs or lets an untrusted peer spoof them.
4. **TLS.** Use full (strict) TLS between the CDN and Caddy so the origin certificate is still
   validated.
