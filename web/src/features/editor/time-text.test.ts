import { describe, expect, it } from "vitest";
import { TIME_PARTS, timeText, timeValue, type TimePart } from "./time-text";

// Local time, so the fields read the same wherever the tests run.
const now = new Date(2026, 10, 12, 16, 8, 21);
const parts = (...chosen: TimePart[]) => new Set(chosen);

describe("timeText", () => {
  it("writes all six parts in Chinese, with the units", () => {
    expect(timeText(now, new Set(TIME_PARTS), true)).toBe("2026年11月12日 16时08分21秒");
  });

  it("writes all six parts in English, as numbers with separators", () => {
    expect(timeText(now, new Set(TIME_PARTS), false)).toBe("2026-11-12 16:08:21");
  });

  it("writes only the date, and only the clock", () => {
    expect(timeText(now, parts("year", "month", "day"), true)).toBe("2026年11月12日");
    expect(timeText(now, parts("year", "month", "day"), false)).toBe("2026-11-12");
    expect(timeText(now, parts("hour", "minute", "second"), true)).toBe("16时08分21秒");
    expect(timeText(now, parts("hour", "minute", "second"), false)).toBe("16:08:21");
  });

  it("leaves out the parts that are not chosen, keeping the rest in order", () => {
    expect(timeText(now, parts("hour", "minute"), true)).toBe("16时08分");
    expect(timeText(now, parts("hour", "minute"), false)).toBe("16:08");
  });

  it("sets a gap between chosen parts apart with a space, not a separator", () => {
    expect(timeText(now, parts("year", "day"), false)).toBe("2026 12");
    expect(timeText(now, parts("hour", "second"), false)).toBe("16 21");
    expect(timeText(now, parts("year", "month", "day", "minute"), false)).toBe("2026-11-12 08");
  });

  it("writes a single part alone", () => {
    expect(timeText(now, parts("year"), true)).toBe("2026年");
    expect(timeText(now, parts("year"), false)).toBe("2026");
  });

  it("writes nothing when no part is chosen", () => {
    expect(timeText(now, parts(), true)).toBe("");
    expect(timeText(now, parts(), false)).toBe("");
  });

  it("pads the month, day, hour, minute and second to two digits", () => {
    const early = new Date(2026, 0, 5, 9, 3, 7);
    expect(timeText(early, new Set(TIME_PARTS), true)).toBe("2026年01月05日 09时03分07秒");
    expect(timeText(early, new Set(TIME_PARTS), false)).toBe("2026-01-05 09:03:07");
  });
});

describe("timeValue", () => {
  it("gives each part of the time as it is written", () => {
    expect(TIME_PARTS.map((part) => timeValue(now, part))).toEqual(["2026", "11", "12", "16", "08", "21"]);
  });
});
