"use client";

import { useEffect, useRef } from "react";
import {
  AreaSeries,
  CandlestickSeries,
  ColorType,
  HistogramSeries,
  createChart,
  type IChartApi,
} from "lightweight-charts";
import { compactZeros } from "@/amounts";
import { useResolvedTheme } from "@/components/theme";
import { setChartSnapshot, type CandleChartPoint, type ChartSnapshotSeries } from "./chart-data";

export function MarketChart({
  points,
  mode,
}: {
  points: CandleChartPoint[];
  mode: "area" | "candles";
}) {
  const theme = useResolvedTheme();
  const hostRef = useRef<HTMLDivElement>(null);
  const chartRef = useRef<IChartApi | null>(null);
  const seriesRef = useRef<{
    setData: (data: unknown[]) => void;
  } | null>(null);
  const volumeRef = useRef<{ setData: (data: unknown[]) => void } | null>(null);

  useEffect(() => {
    const host = hostRef.current;
    if (!host) return;
    const palette = chartPalette(host);
    const chart = createChart(host, {
      autoSize: false,
      width: host.clientWidth,
      height: 320,
      layout: {
        background: { type: ColorType.Solid, color: "transparent" },
        textColor: palette.text,
        fontFamily: palette.font,
        fontSize: 11,
        attributionLogo: false,
      },
      grid: { vertLines: { visible: false }, horzLines: { color: palette.grid } },
      crosshair: {
        vertLine: { color: palette.crosshair, labelBackgroundColor: palette.label },
        horzLine: { color: palette.crosshair, labelBackgroundColor: palette.label },
      },
      rightPriceScale: { borderVisible: false },
      // Fixed edges let a handful of points span the width instead of clustering mid-chart.
      timeScale: { borderVisible: false, timeVisible: true, fixLeftEdge: true, fixRightEdge: true },
    });
    const sparse = points.length < SPARSE_POINTS;
    chartRef.current = chart;
    const priceSeries =
      mode === "candles"
        ? chart.addSeries(CandlestickSeries, {
            upColor: palette.up,
            downColor: palette.down,
            borderVisible: false,
            priceFormat: priceFormatFor(points),
            wickUpColor: palette.up,
            wickDownColor: palette.down,
          })
        : chart.addSeries(AreaSeries, {
            lineColor: palette.up,
            lineWidth: 2,
            topColor: withAlpha(palette.up, 0.22),
            bottomColor: withAlpha(palette.up, 0),
            priceLineVisible: false,
            priceFormat: priceFormatFor(points),
            // A line needs two points; markers keep one or two trades visible.
            pointMarkersVisible: sparse,
          });
    const volumeSeries = chart.addSeries(HistogramSeries, {
      priceFormat: { type: "volume" },
      priceScaleId: "volume",
      color: palette.volume,
      // Its last-value label would sit on the price axis, over the price labels.
      lastValueVisible: false,
      priceLineVisible: false,
    });
    chart.priceScale("volume").applyOptions({ scaleMargins: { top: 0.78, bottom: 0 } });
    // Keep the price line above the volume band.
    chart.priceScale("right").applyOptions({ scaleMargins: { top: 0.1, bottom: 0.25 } });
    setChartSnapshot(
      priceSeries as unknown as ChartSnapshotSeries,
      volumeSeries as unknown as ChartSnapshotSeries,
      points,
      mode,
    );
    seriesRef.current = priceSeries as unknown as {
      setData: (data: unknown[]) => void;
    };
    volumeRef.current = volumeSeries as unknown as { setData: (data: unknown[]) => void };
    chart.timeScale().fitContent();
    const observer = new ResizeObserver(() => {
      if (host.clientWidth > 0) chart.applyOptions({ width: host.clientWidth });
    });
    observer.observe(host);
    return () => {
      observer.disconnect();
      seriesRef.current = null;
      volumeRef.current = null;
      chartRef.current = null;
      chart.remove();
    };
    // Recreate only when the mode, point count or theme changes; snapshots replace series data below.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [mode, points.length, theme]);

  useEffect(() => {
    const series = seriesRef.current;
    const volume = volumeRef.current;
    if (!series || !volume) return;
    setChartSnapshot(series, volume, points, mode);
  }, [mode, points]);

  return (
    <div className="market-chart-wrap">
      {points.length ? (
        <div
          ref={hostRef}
          className="market-chart"
          role="img"
          aria-label={`${mode} market chart`}
        />
      ) : (
        <p className="chart-empty">No candles in this range.</p>
      )}
      {points.length > 0 && points.length < SPARSE_POINTS ? (
        <p className="chart-sparse">
          Few trades in this range so far; the chart fills in as they land.
        </p>
      ) : null}
      <p className="chart-attribution">
        Updates as activity is recorded ·{" "}
        <a href="https://www.tradingview.com" target="_blank" rel="noopener noreferrer">
          TradingView Lightweight Charts
        </a>
      </p>
    </div>
  );
}

/** Below this many points the chart marks each one and says the range is still thin. */
export const SPARSE_POINTS = 5;

/** Reads the chart colours from the active theme's CSS tokens (canvas cannot use variables). */
function chartPalette(host: HTMLElement) {
  const style = getComputedStyle(host);
  const token = (name: string, fallback: string) => style.getPropertyValue(name).trim() || fallback;
  return {
    text: token("--text-3", "#8a8a8a"),
    grid: token("--hairline", "rgba(255, 255, 255, 0.08)"),
    crosshair: token("--hairline-bright", "rgba(255, 255, 255, 0.24)"),
    label: token("--fill-hover", "#262626"),
    volume: token("--hairline-strong", "rgba(255, 255, 255, 0.12)"),
    up: token("--up", "#34c77b"),
    down: token("--down", "#ff6461"),
    font: style.fontFamily,
  };
}

/** `#rrggbb` → `rgba(...)`; any other colour form is returned unchanged. */
export function withAlpha(color: string, alpha: number) {
  const hex = /^#([0-9a-f]{6})$/i.exec(color)?.[1];
  if (!hex) return color;
  const value = Number.parseInt(hex, 16);
  return `rgba(${(value >> 16) & 255}, ${(value >> 8) & 255}, ${value & 255}, ${alpha})`;
}

/** Token prices are tiny fractions of ETH; size the scale's precision to the data or it reads 0.00. */
export function priceFormatFor(points: readonly CandleChartPoint[]) {
  const top = points.reduce((max, point) => Math.max(max, point.high), 0);
  const precision = top > 0 ? Math.min(15, Math.max(2, Math.ceil(-Math.log10(top)) + 3)) : 2;
  // A custom formatter: the built-in one garbles labels once minMove drops below ~1e-9.
  return {
    type: "custom" as const,
    minMove: 10 ** -precision,
    formatter: (price: number) => compactZeros(price.toFixed(precision)),
  };
}
