import { describe, expect, it, vi } from "vitest";
import {
  HIGHLIGHTS_STALE_MS,
  highlightsStale,
  progressPercent,
  startVisiblePolling,
} from "./highlights";

describe("progressPercent", () => {
  it("formats basis points as a one-decimal percentage", () => {
    expect(progressPercent(8000)).toBe("80.0%");
    expect(progressPercent(1234)).toBe("12.3%");
    expect(progressPercent(0)).toBe("0.0%");
  });

  it("shows a sub-0.1% progress as <0.1% rather than 0.0%", () => {
    expect(progressPercent(1)).toBe("<0.1%");
    expect(progressPercent(9)).toBe("<0.1%");
    expect(progressPercent(10)).toBe("0.1%");
  });

  it("clamps out-of-range and invalid values", () => {
    expect(progressPercent(12_000)).toBe("100.0%");
    expect(progressPercent(-5)).toBe("0.0%");
    expect(progressPercent(Number.NaN)).toBe("0.0%");
  });
});

describe("highlightsStale", () => {
  it("is false before any success and within two refresh intervals", () => {
    expect(highlightsStale(null, 1_000_000)).toBe(false);
    expect(highlightsStale(1_000_000, 1_000_000 + HIGHLIGHTS_STALE_MS)).toBe(false);
  });

  it("is true once the kept data is older than two refresh intervals", () => {
    expect(highlightsStale(1_000_000, 1_000_001 + HIGHLIGHTS_STALE_MS)).toBe(true);
  });
});

describe("startVisiblePolling", () => {
  function fakeDocument() {
    const target = new EventTarget();
    const doc = {
      visibilityState: "visible" as "visible" | "hidden",
      addEventListener: target.addEventListener.bind(target),
      removeEventListener: target.removeEventListener.bind(target),
      setVisibility(state: "visible" | "hidden") {
        doc.visibilityState = state;
        target.dispatchEvent(new Event("visibilitychange"));
      },
    };
    return doc;
  }

  it("ticks while visible, skips while hidden, and refreshes on becoming visible", () => {
    vi.useFakeTimers();
    try {
      const doc = fakeDocument();
      const tick = vi.fn();
      const stop = startVisiblePolling(tick, 1000, doc as unknown as Document);

      vi.advanceTimersByTime(1000);
      expect(tick).toHaveBeenCalledTimes(1);

      doc.setVisibility("hidden");
      vi.advanceTimersByTime(3000);
      expect(tick).toHaveBeenCalledTimes(1);

      doc.setVisibility("visible");
      expect(tick).toHaveBeenCalledTimes(2);

      stop();
      vi.advanceTimersByTime(3000);
      doc.setVisibility("hidden");
      doc.setVisibility("visible");
      expect(tick).toHaveBeenCalledTimes(2);
    } finally {
      vi.useRealTimers();
    }
  });
});
