"use client";
/* Data loading is an external synchronization boundary for this route. */
/* eslint-disable react-hooks/set-state-in-effect */
/* eslint-disable react-hooks/exhaustive-deps */

import { useEffect, useState } from "react";
import Link from "next/link";
import { ApiClient, type ProtocolDailyResponse } from "@/api/client";
import { protocolRevenueView, type ProtocolRevenueView } from "@/analytics/protocol-revenue";
import { DEFAULT_E2E_WEB_URL, publicConfiguration } from "@/config/public";
import { PageHeader } from "@/components/layout";
import { formatCanonicalBaseUnits } from "@/amounts";
import { Badge, Button, ErrorState, Skeleton, UnavailableState } from "@/components/primitives";

function eth(value: string) {
  const formatted = formatCanonicalBaseUnits(value, 4);
  return formatted === null ? "Unavailable" : `${formatted} ETH`;
}
function snapshotLabel(snapshot: ProtocolDailyResponse["snapshot"]) {
  return snapshot.finality === "finalized" || snapshot.finality === "safe"
    ? "Verified data"
    : "Updating data";
}

export default function AnalyticsPage() {
  const configuration = publicConfiguration();
  const [summary, setSummary] = useState<Awaited<ReturnType<ApiClient["getProtocol"]>> | null>(
    null,
  );
  const [daily, setDaily] = useState<ProtocolDailyResponse | null>(null);
  // Revenue is a separate read: its failure leaves the rest of Analytics standing.
  const [revenue, setRevenue] = useState<ProtocolRevenueView | "unavailable" | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<Error | null>(null);
  const load = async () => {
    // E2E-only: `?fixture=populated` reads the routed fixture API, as the token page does.
    const fixtureBase =
      process.env.NEXT_PUBLIC_E2E_FIXTURE === "1" &&
      new URLSearchParams(window.location.search).get("fixture") === "populated"
        ? process.env.NEXT_PUBLIC_TASK6_ANVIL_WEB_URL || DEFAULT_E2E_WEB_URL
        : null;
    const apiBaseUrl =
      fixtureBase ?? (configuration.status === "ready" ? configuration.apiBaseUrl : null);
    if (!apiBaseUrl) {
      setLoading(false);
      return;
    }
    setLoading(true);
    setError(null);
    try {
      const client = new ApiClient({ baseUrl: apiBaseUrl });
      const revenueRequest = client.getProtocolRevenue().then(
        (body) => protocolRevenueView(body) ?? "unavailable",
        () => "unavailable" as const,
      );
      const [nextSummary, nextDaily, nextRevenue] = await Promise.all([
        client.getProtocol(),
        client.getProtocolDaily({ limit: 366 }),
        revenueRequest,
      ]);
      if (
        nextSummary.snapshot.as_of_block !== nextDaily.snapshot.as_of_block ||
        nextSummary.snapshot.as_of_block_hash !== nextDaily.snapshot.as_of_block_hash
      )
        throw new Error("The API returned mixed canonical snapshots. Refresh to retry.");
      setSummary(nextSummary);
      setDaily(nextDaily);
      setRevenue(nextRevenue);
    } catch (cause) {
      setError(cause instanceof Error ? cause : new Error("Analytics unavailable"));
    } finally {
      setLoading(false);
    }
  };
  useEffect(() => {
    void load();
  }, [configuration.apiBaseUrl, configuration.status]);
  return (
    <div className="page-stack analytics-page">
      <PageHeader
        title="Read the launch route."
        summary="Protocol activity across launches, trades, volume, and graduations. ETH-native values are shown without USD enrichment."
        actions={
          <Link className="ui-button ui-button-secondary ui-button-md" href="/leaderboard">
            View the leaderboard
          </Link>
        }
      />
      {error ? (
        <ErrorState
          title="Live analytics are unavailable"
          description="The indexed protocol snapshot could not be loaded. No estimated or cached market values are being substituted."
          action={
            <div className="state-actions">
              <Button onClick={() => void load()}>Retry</Button>
              <Link className="ui-button ui-button-quiet ui-button-md" href="/docs#contracts">
                How data finality works
              </Link>
            </div>
          }
        />
      ) : null}
      {loading ? (
        <div className="analytics-grid">
          {[1, 2, 3, 4].map((item) => (
            <div className="metric-panel" key={item}>
              <Skeleton />
              <Skeleton />
            </div>
          ))}
        </div>
      ) : null}
      {!loading && !error && summary && daily ? (
        <>
          <section className="analytics-snapshot" aria-label="Current analytics">
            <span className="panel-kicker">Current window</span>
            <Badge
              tone={
                summary.snapshot.finality === "finalized" || summary.snapshot.finality === "safe"
                  ? "success"
                  : "warning"
              }
            >
              {snapshotLabel(summary.snapshot)}
            </Badge>
            <span className="mono">
              Updated {new Date(summary.updated_at).toLocaleString("en-US")}
            </span>
          </section>
          <section className="analytics-grid" aria-label="Protocol summary">
            <Metric label="24h volume" value={eth(summary.volume_24h_eth)} />
            <Metric label="All-time volume" value={eth(summary.volume_all_time_eth)} />
            <Metric label="24h launches" value={summary.launches_24h.toLocaleString("en-US")} />
            <Metric
              label="All-time launches"
              value={summary.launches_all_time.toLocaleString("en-US")}
            />
            <Metric label="24h trades" value={summary.trades_24h.toLocaleString("en-US")} />
            <Metric
              label="Graduations"
              value={`${summary.graduations_24h.toLocaleString("en-US")} / ${summary.graduations_all_time.toLocaleString("en-US")}`}
            />
          </section>
          {revenue ? <RevenueCard revenue={revenue} /> : null}
          <section className="analytics-history" aria-labelledby="daily-history-title">
            <div className="panel-head">
              <div>
                <p className="panel-kicker">Activity history</p>
                <h2 id="daily-history-title">Daily activity</h2>
              </div>
              <span className="mono">{daily.items?.length ?? 0} days · ETH only</span>
            </div>
            {daily.items?.length ? <VolumeChart items={daily.items} /> : null}
            {daily.items?.length ? (
              <div className="analytics-table" role="table">
                <div className="analytics-row analytics-heading" role="row">
                  <span>Day</span>
                  <span>Volume</span>
                  <span>Launches</span>
                  <span>Trades</span>
                  <span>Graduations</span>
                </div>
                {daily.items.map((item) => (
                  <div className="analytics-row" role="row" key={item.day}>
                    <span className="mono">{item.day}</span>
                    <span className="mono">{eth(item.volume_eth)}</span>
                    <span>{item.launches.toLocaleString("en-US")}</span>
                    <span>{item.trades.toLocaleString("en-US")}</span>
                    <span>{item.graduations.toLocaleString("en-US")}</span>
                  </div>
                ))}
              </div>
            ) : (
              <UnavailableState
                title="No daily history"
                description="The indexer has not published a canonical daily range yet."
              />
            )}
          </section>
          <p className="ui-field-hint">
            Daily values update as new chain activity becomes available.
          </p>
        </>
      ) : null}
      {!loading && !error && !summary ? (
        <UnavailableState
          title="Analytics unavailable"
          description="No reviewed API deployment is connected. No sample market values are shown."
        />
      ) : null}
    </div>
  );
}

function RevenueCard({ revenue }: { revenue: ProtocolRevenueView | "unavailable" }) {
  return (
    <section className="analytics-history analytics-revenue" aria-labelledby="revenue-title">
      <div className="panel-head">
        <div>
          <p className="panel-kicker">Protocol revenue</p>
          <h2 id="revenue-title">
            {revenue === "unavailable" ? "Protocol revenue" : `${revenue.total} earned`}
          </h2>
        </div>
        {revenue === "unavailable" ? null : (
          <span className="mono">All-time · block {revenue.block.toLocaleString("en-US")}</span>
        )}
      </div>
      {revenue === "unavailable" ? (
        <UnavailableState
          title="Protocol revenue is unavailable"
          description="The revenue read could not be loaded. The other analytics above are unaffected."
        />
      ) : revenue.rows.length ? (
        <div className="analytics-table" role="table" aria-label="Protocol revenue by source">
          <div className="analytics-row analytics-revenue-row analytics-heading" role="row">
            <span role="columnheader">Source</span>
            <span role="columnheader">Engine 1</span>
            <span role="columnheader">Engine 2</span>
            <span role="columnheader">Total</span>
          </div>
          {revenue.rows.map((row) => (
            <div className="analytics-row analytics-revenue-row" role="row" key={row.label}>
              <span role="cell">{row.label}</span>
              <span className="mono" role="cell">
                {row.engine1}
              </span>
              <span className="mono" role="cell">
                {row.engine2}
              </span>
              <span className="mono" role="cell">
                {row.total}
              </span>
            </div>
          ))}
        </div>
      ) : (
        <UnavailableState
          title="No protocol revenue yet"
          description="Revenue appears once launches and trades are indexed."
        />
      )}
      <p className="ui-field-hint">
        ETH credited to the protocol when it happens (launch fees, the protocol share of trade and
        LP fees, snipe tax, graduation remainders), whether or not the treasury has claimed it yet.
        Token-denominated amounts are not included.
      </p>
    </section>
  );
}

type DailyItem = NonNullable<ProtocolDailyResponse["items"]>[number];

/** Bar heights are BigInt ratios against the busiest day; the table below stays the data source. */
function VolumeChart({ items }: { items: DailyItem[] }) {
  const ordered = [...items].sort((a, b) => a.day.localeCompare(b.day));
  const volumes = ordered.map((item) => {
    try {
      return BigInt(item.volume_eth);
    } catch {
      return 0n;
    }
  });
  const max = volumes.reduce((top, value) => (value > top ? value : top), 0n);
  return (
    <div className="volume-chart" aria-hidden="true">
      {ordered.map((item, index) => {
        const height = max === 0n ? 0 : Number(((volumes[index] ?? 0n) * 1000n) / max) / 10;
        return (
          <div
            className="volume-bar"
            key={item.day}
            data-tip={`${item.day} · ${eth(item.volume_eth)}`}
            style={{ "--h": `${height}%`, "--i": index } as React.CSSProperties}
          >
            <span />
          </div>
        );
      })}
    </div>
  );
}

function Metric({ label, value }: { label: string; value: string }) {
  return (
    <section className="metric-panel">
      <dl>
        <dt>{label}</dt>
        <dd className="metric-value">
          <span className="sr-only">{value}</span>
          {/* Characters roll in one by one; the string is never parsed back into a number. */}
          {[...value].map((char, index) => (
            <span key={index} aria-hidden="true" style={{ "--i": index } as React.CSSProperties}>
              {char}
            </span>
          ))}
        </dd>
      </dl>
    </section>
  );
}
