import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { CandleChartPoint } from "./chart-data";

type ChartDatum = { time: number; [key: string]: unknown };
type MockSeries = {
  data: ChartDatum[];
  setData: ReturnType<typeof vi.fn>;
  update: ReturnType<typeof vi.fn>;
};
type MockChart = {
  price: MockSeries;
  volume: MockSeries;
  applyOptions: ReturnType<typeof vi.fn>;
  remove: ReturnType<typeof vi.fn>;
};
type Effect = {
  create: () => void | (() => void);
  deps: readonly unknown[] | undefined;
  cleanup?: () => void;
};

const harness = vi.hoisted(() => {
  const createSeries = () => {
    const series: MockSeries = { data: [], setData: vi.fn(), update: vi.fn() };
    series.setData.mockImplementation((data: ChartDatum[]) => {
      series.data = [...data];
    });
    series.update.mockImplementation((datum: ChartDatum, historicalUpdate = false) => {
      const existingIndex = series.data.findIndex(({ time }) => time === datum.time);
      if (existingIndex >= 0 && (existingIndex === series.data.length - 1 || historicalUpdate)) {
        series.data[existingIndex] = datum;
      } else if (existingIndex < 0) {
        series.data.push(datum);
      }
    });
    return series;
  };

  return {
    refs: [] as Array<{ current: unknown }>,
    effects: [] as Effect[],
    pending: [] as Effect[],
    refIndex: 0,
    effectIndex: 0,
    charts: [] as MockChart[],
    createSeries,
  };
});

vi.mock("react", async (importOriginal) => {
  const actual = await importOriginal<typeof import("react")>();
  return {
    ...actual,
    useRef: (initial: unknown) => {
      const index = harness.refIndex++;
      return (harness.refs[index] ??= { current: initial });
    },
    useEffect: (create: Effect["create"], deps?: readonly unknown[]) => {
      harness.pending[harness.effectIndex++] = { create, deps };
    },
  };
});

vi.mock("lightweight-charts", () => ({
  AreaSeries: {},
  CandlestickSeries: {},
  ColorType: { Solid: "solid" },
  HistogramSeries: {},
  createChart: () => {
    const price = harness.createSeries();
    const volume = harness.createSeries();
    let seriesIndex = 0;
    const chart: MockChart = {
      price,
      volume,
      applyOptions: vi.fn(),
      remove: vi.fn(),
    };
    harness.charts.push(chart);
    return {
      addSeries: () => (seriesIndex++ === 0 ? price : volume),
      priceScale: () => ({ applyOptions: vi.fn() }),
      timeScale: () => ({ fitContent: vi.fn() }),
      applyOptions: chart.applyOptions,
      remove: chart.remove,
    };
  },
}));

vi.mock("@/components/theme", () => ({ useResolvedTheme: () => "dark" }));

import { MarketChart, priceFormatFor, withAlpha } from "./market-chart";

const sameDependencies = (
  left: readonly unknown[] | undefined,
  right: readonly unknown[] | undefined,
) =>
  left !== undefined &&
  right !== undefined &&
  left.length === right.length &&
  left.every((value, index) => Object.is(value, right[index]));

function render(points: CandleChartPoint[]) {
  harness.refIndex = 0;
  harness.effectIndex = 0;
  harness.pending = [];
  MarketChart({ points, mode: "area" });
  harness.refs[0]!.current = { clientWidth: 700 };

  for (const [index, next] of harness.pending.entries()) {
    const previous = harness.effects[index];
    if (previous && sameDependencies(previous.deps, next.deps)) continue;
    previous?.cleanup?.();
    const cleanup = next.create();
    harness.effects[index] = {
      ...next,
      ...(typeof cleanup === "function" ? { cleanup } : {}),
    };
  }
}

const point = (time: number, close: number, volume: number): CandleChartPoint => ({
  time,
  open: close - 0.1,
  high: close + 0.2,
  low: close - 0.2,
  close,
  volume,
});

describe("MarketChart component snapshot updates", () => {
  let originalResizeObserver: typeof ResizeObserver | undefined;

  beforeEach(() => {
    harness.refs = [];
    harness.effects = [];
    harness.pending = [];
    harness.refIndex = 0;
    harness.effectIndex = 0;
    harness.charts = [];
    originalResizeObserver = globalThis.ResizeObserver;
    globalThis.ResizeObserver = class {
      observe() {}
      disconnect() {}
    } as unknown as typeof ResizeObserver;
    vi.stubGlobal("getComputedStyle", () => ({ getPropertyValue: () => "", fontFamily: "" }));
  });

  afterEach(() => {
    for (const effect of harness.effects) effect.cleanup?.();
    if (originalResizeObserver) globalThis.ResizeObserver = originalResizeObserver;
    else Reflect.deleteProperty(globalThis, "ResizeObserver");
    vi.unstubAllGlobals();
  });

  it("replaces corrected same-length snapshots and shortened history on one mounted component", () => {
    const initial = [point(10, 1, 100), point(20, 2, 200), point(30, 3, 300)];
    render(initial);
    expect(harness.charts).toHaveLength(1);
    expect(harness.charts[0]!.price.setData).toHaveBeenLastCalledWith(
      initial.map(({ time, close }) => ({ time, value: close })),
    );

    const corrected = [point(10, 1.25, 125), initial[1]!, initial[2]!];
    render(corrected);
    expect(harness.charts).toHaveLength(1);
    expect(harness.charts[0]!.price.setData).toHaveBeenLastCalledWith(
      corrected.map(({ time, close }) => ({ time, value: close })),
    );
    expect(harness.charts[0]!.price.data).toEqual(
      corrected.map(({ time, close }) => ({ time, value: close })),
    );
    expect(harness.charts[0]!.volume.setData).toHaveBeenLastCalledWith(
      corrected.map(({ time, volume }) => ({ time, value: volume })),
    );
    expect(harness.charts[0]!.volume.data).toEqual(
      corrected.map(({ time, volume }) => ({ time, value: volume })),
    );
    expect(harness.charts[0]!.price.update).not.toHaveBeenCalled();
    const update = harness.charts[0]!.price.update as unknown as (
      datum: ChartDatum,
      historicalUpdate?: boolean,
    ) => void;
    expect(() => update({ time: 30, value: 3.5 })).not.toThrow();
    expect(harness.charts[0]!.price.data.at(-1)).toEqual({ time: 30, value: 3.5 });

    const shortened = corrected.slice(0, 2);
    render(shortened);
    expect(harness.charts).toHaveLength(2);
    expect(harness.charts[0]!.remove).toHaveBeenCalledOnce();
    expect(harness.charts[1]!.price.setData).toHaveBeenLastCalledWith(
      shortened.map(({ time, close }) => ({ time, value: close })),
    );
    expect(harness.charts[1]!.volume.setData).toHaveBeenLastCalledWith(
      shortened.map(({ time, volume }) => ({ time, value: volume })),
    );
  });
});

describe("withAlpha", () => {
  it("converts six-digit hex tokens and passes other colour forms through", () => {
    expect(withAlpha("#c8f54a", 0.22)).toBe("rgba(200, 245, 74, 0.22)");
    expect(withAlpha("#C8F54A", 0)).toBe("rgba(200, 245, 74, 0)");
    expect(withAlpha("rgba(1, 2, 3, 0.5)", 0.1)).toBe("rgba(1, 2, 3, 0.5)");
    expect(withAlpha("#fff", 0.1)).toBe("#fff");
  });
});

describe("priceFormatFor", () => {
  it("keeps about four significant digits of the highest price", () => {
    const at = (high: number) => [{ time: 1, open: high, high, low: high, close: high, volume: 0 }];
    expect(priceFormatFor([]).formatter(1.5)).toBe("1.50");
    expect(priceFormatFor(at(1.5)).formatter(1.5)).toBe("1.500");
    expect(priceFormatFor(at(0.00000001234)).formatter(0.00000001234)).toBe("0.00000001234");
    expect(priceFormatFor(at(1e-30)).minMove).toBe(1e-15);
  });
});
