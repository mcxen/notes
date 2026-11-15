import { describe, expect, it } from "vitest";
import type { DatabaseProperty, NotePage } from "../../core/types";
import { arcPath, chartSlices } from "./chart-data";

const row = (id: string, values: Record<string, string>): NotePage => ({ id, title: id, properties: values } as unknown as NotePage);

const status = {
  id: "s", name: "Status", type: "status", default_status: "wanted",
  status_options: [
    { id: "done", name: "Done", color: "green", group: "complete" },
    { id: "wanted", name: "Wanted", color: "gray", group: "todo" },
    { id: "doing", name: "Doing", color: "blue", group: "progress" },
  ],
} as DatabaseProperty;

const area = { id: "a", name: "Area", type: "select", options: ["Sync", "Editor", "Other"] } as DatabaseProperty;

describe("a chart of a database", () => {
  it("counts the rows by status, in the order the status keeps its options, with the colours it gave", () => {
    const rows = [row("1", { s: "Done" }), row("2", { s: "Doing" }), row("3", {}), row("4", { s: "Done" }), row("5", { s: " Doing " })];
    const slices = chartSlices(rows, status);
    // To-do first, then in progress, then complete.
    expect(slices.map((slice) => [slice.name, slice.count, slice.tone])).toEqual([
      ["Wanted", 1, "gray"], ["Doing", 2, "blue"], ["Done", 2, "green"],
    ]);
    expect(slices.reduce((sum, slice) => sum + slice.share, 0)).toBeCloseTo(1, 6);
    expect(slices[1].rows.map((r) => r.id)).toEqual(["2", "5"]);
  });

  it("counts a select by its options, keeps one nobody has, and puts the rows with none last", () => {
    const rows = [row("1", { a: "Sync" }), row("2", { a: "Sync" }), row("3", {}), row("4", { a: "Gone" })];
    const slices = chartSlices(rows, area);
    expect(slices.map((slice) => [slice.name, slice.count])).toEqual([["Sync", 2], ["Editor", 0], ["Other", 0], ["", 2]]);
    expect(new Set(slices.slice(0, 3).map((slice) => slice.tone)).size).toBe(3);
    expect(slices[3].tone).toBe("default");
  });

  it("has nothing to count in a database with no rows, and divides by nothing never", () => {
    const slices = chartSlices([], area);
    expect(slices.every((slice) => slice.count === 0 && slice.share === 0)).toBe(true);
    expect(chartSlices([], { ...area, options: [] } as DatabaseProperty)).toEqual([]);
  });
});

describe("the arc of a ring", () => {
  it("is a closed path between two radii", () => {
    const path = arcPath(50, 50, 40, 25, 0, 0.25);
    expect(path.startsWith("M 50 10")).toBe(true);
    expect(path.endsWith("Z")).toBe(true);
    expect(path).not.toMatch(/NaN/);
  });

  it("takes the long way round past half a turn, and a whole turn as two halves", () => {
    expect(arcPath(50, 50, 40, 25, 0, 0.75)).toMatch(/A 40 40 0 1 1/);
    expect(arcPath(50, 50, 40, 25, 0, 0.25)).toMatch(/A 40 40 0 0 1/);
    const whole = arcPath(50, 50, 40, 25, 0, 1);
    expect(whole.match(/A 40 40/g)).toHaveLength(2);
    expect(whole).not.toMatch(/NaN/);
  });
});
