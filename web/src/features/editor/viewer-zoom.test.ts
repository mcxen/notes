import { describe, expect, it } from "vitest";
import { actualSize, FIT, limitsFor, percentOf, wheelFactor, zoomAbout } from "./viewer-zoom";

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
