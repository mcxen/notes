import { describe, expect, it } from "vitest";
import { actualSize, canScroll, clampPan, FIT, limitsFor, panLimits, percentOf, scrollBy, wheelFactor, zoomAbout } from "./viewer-zoom";

const limits = { min: 0.25, max: 8 };

describe("zooming the picture viewer", () => {
  it("keeps the spot under the pointer where it is", () => {
    const point = { x: 100, y: -40 };
    const before = { scale: 2, x: 30, y: 10 };
    const after = zoomAbout(before, 1.5, point, limits);
    // The picture's own point under the pointer, before and after.
    const under = (z: typeof before) => ({ x: (point.x - z.x) / z.scale, y: (point.y - z.y) / z.scale });
    expect(under(after).x).toBeCloseTo(under(before).x);
    expect(under(after).y).toBeCloseTo(under(before).y);
    expect(after.scale).toBeCloseTo(3);
  });

  it("stays within the limits, and fitted at exactly 1", () => {
    expect(zoomAbout(FIT, 100, { x: 0, y: 0 }, limits).scale).toBe(8);
    expect(zoomAbout(FIT, 0.001, { x: 0, y: 0 }, limits).scale).toBe(0.25);
    expect(zoomAbout({ scale: 2, x: 50, y: 50 }, 0.5, { x: 9, y: 9 }, limits)).toEqual(FIT);
  });

  it("reads actual size and the readout from the picture's own width", () => {
    expect(actualSize(500, 2000)).toBe(4);
    expect(percentOf(1, 500, 2000)).toBe(25);
    expect(percentOf(4, 500, 2000)).toBe(100);
    expect(limitsFor(500, 2000).max).toBe(16);
    expect(limitsFor(500, 600).max).toBe(8);
  });

  it("turns a pinch more finely than a wheel", () => {
    expect(wheelFactor(-100, false)).toBeGreaterThan(1);
    expect(wheelFactor(100, false)).toBeLessThan(1);
    expect(wheelFactor(-10, true)).toBeGreaterThan(wheelFactor(-10, false));
  });
});

/* A 600 x 4000 picture fitted in a 976 x 684 stage is a strip 103 x 684. */
const stage = { width: 976, height: 684 };
const strip = { width: 103, height: 684 };

describe("moving a very tall picture within its viewer", () => {
  it("cannot be moved while it fits, and is held in the middle", () => {
    expect(panLimits(1, stage, strip)).toEqual({ x: 0, y: 0 });
    expect(clampPan({ scale: 1, x: 40, y: -30 }, stage, strip)).toEqual({ scale: 1, x: 0, y: 0 });
    expect(clampPan({ scale: 0.5, x: 40, y: -30 }, stage, strip).y).toBe(0);
  });

  it("reaches the top and the bottom at every zoom, and no further", () => {
    for (const scale of [1.5, 2, 4, 8, 20]) {
      const half = (strip.height * scale - stage.height) / 2;
      expect(panLimits(scale, stage, strip).y).toBeCloseTo(half);
      // Dragged as far down as it goes: the picture's top edge is at the stage's top.
      expect(clampPan({ scale, x: 0, y: 1e6 }, stage, strip).y).toBeCloseTo(half);
      // And as far up: its foot is at the stage's foot.
      expect(clampPan({ scale, x: 0, y: -1e6 }, stage, strip).y).toBeCloseTo(-half);
    }
  });

  it("stays across the stage while it is narrower than it, and pans sideways once wider", () => {
    expect(panLimits(4, stage, strip).x).toBe(0);
    expect(panLimits(20, stage, strip).x).toBeCloseTo((strip.width * 20 - stage.width) / 2);
  });

  it("scrolls down to the foot by wheel, and stops there", () => {
    let zoom = { scale: 2, x: 0, y: (strip.height * 2 - stage.height) / 2 };
    expect(canScroll(zoom, 0, 100, stage, strip)).toBe(true);
    const seen: number[] = [];
    for (let i = 0; i < 20; i++) { zoom = scrollBy(zoom, 0, 100, stage, strip); seen.push(zoom.y); }
    expect(zoom.y).toBeCloseTo(-(strip.height * 2 - stage.height) / 2);
    expect(Math.min(...seen)).toBeCloseTo(zoom.y);
  });

  it("zooms rather than scrolls where the picture fits the way the wheel turns", () => {
    expect(canScroll(FIT, 0, 100, stage, strip)).toBe(false);
    // Zoomed taller than the stage but still narrower: a sideways scroll has nowhere to go.
    expect(canScroll({ scale: 2, x: 0, y: 0 }, 30, 0, stage, strip)).toBe(false);
    expect(canScroll({ scale: 2, x: 0, y: 0 }, 30, 100, stage, strip)).toBe(true);
  });

  it("keeps a zoom's result within reach of the picture", () => {
    const area = { stage, fit: strip };
    // Zoomed about the top-left corner of the stage, far from the strip: held in range.
    const zoomed = zoomAbout({ scale: 2, x: 0, y: 0 }, 2, { x: -480, y: -340 }, limits, area);
    const limit = panLimits(zoomed.scale, stage, strip);
    expect(Math.abs(zoomed.y)).toBeLessThanOrEqual(limit.y + 1e-9);
    expect(Math.abs(zoomed.x)).toBeLessThanOrEqual(limit.x + 1e-9);
    // A wide picture's ordinary zoom is left as it was.
    const wide = { width: 900, height: 300 };
    expect(zoomAbout(FIT, 2, { x: 0, y: 0 }, limits, { stage, fit: wide })).toEqual({ scale: 2, x: 0, y: 0 });
  });
});
