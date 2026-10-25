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
      timeScale: { borderVisible: false, timeVisible: true },
    });
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
          });
    const volumeSeries = chart.addSeries(HistogramSeries, {
      priceFormat: { type: "volume" },
      priceScaleId: "volume",
      color: palette.volume,
    });
    chart.priceScale("volume").applyOptions({ scaleMargins: { top: 0.78, bottom: 0 } });
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
        <div ref={hostRef} className="market-chart" aria-label={`${mode} market chart`} />
      ) : (
        <p className="chart-empty">No candles in this range.</p>
      )}
      <p className="chart-attribution">
        Updates as activity is recorded ·{" "}
        <a href="https://www.tradingview.com" target="_blank" rel="noopener noreferrer">
          TradingView Lightweight Charts
        </a>
      </p>
    </div>
  );
}

/** Reads the chart colours from the active theme's CSS tokens (canvas cannot use variables). */
function chartPalette(host: HTMLElement) {
  const style = getComputedStyle(host);
  const token = (name: string, fallback: string) => style.getPropertyValue(name).trim() || fallback;
  return {
    text: token("--text-faint", "#84848d"),
    grid: token("--line", "rgba(255, 255, 255, 0.07)"),
    crosshair: token("--line-bright", "rgba(255, 255, 255, 0.2)"),
    label: token("--surface-hover", "#1e1e22"),
    volume: token("--line-strong", "rgba(255, 255, 255, 0.11)"),
    up: token("--accent-line", "#c8f54a"),
    down: token("--danger", "#ff6f61"),
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
    formatter: (price: number) => price.toFixed(precision),
  };
}
