/* The parts of a time a line can take, in the order the time picker shows
   them. */
export type TimePart = "year" | "month" | "day" | "hour" | "minute" | "second";

export const TIME_PARTS: readonly TimePart[] = ["year", "month", "day", "hour", "minute", "second"];

const pad = (value: number) => String(value).padStart(2, "0");

/** One part of `now` as it is written: "2026", "11", "12", "16", "08", "21". */
export function timeValue(now: Date, part: TimePart): string {
  switch (part) {
    case "year": return String(now.getFullYear());
    case "month": return pad(now.getMonth() + 1);
    case "day": return pad(now.getDate());
    case "hour": return pad(now.getHours());
    case "minute": return pad(now.getMinutes());
    case "second": return pad(now.getSeconds());
  }
}

function joined(parts: readonly (string | false)[], separator: string): string {
  return parts.filter((part): part is string => typeof part === "string" && part !== "").join(separator);
}

/** The chosen parts of `now` as plain text, in the page's language:
 *  "2026年11月12日 16时08分21秒" in Chinese, "2026-11-12 16:08:21" otherwise.
 *  The date and the clock are set apart by a space. Nothing is written when no
 *  part is chosen. */
export function timeText(now: Date, parts: ReadonlySet<TimePart>, chinese: boolean): string {
  const has = (part: TimePart) => parts.has(part);
  if (chinese) {
    const date = joined([
      has("year") && `${timeValue(now, "year")}年`,
      has("month") && `${timeValue(now, "month")}月`,
      has("day") && `${timeValue(now, "day")}日`,
    ], "");
    const clock = joined([
      has("hour") && `${timeValue(now, "hour")}时`,
      has("minute") && `${timeValue(now, "minute")}分`,
      has("second") && `${timeValue(now, "second")}秒`,
    ], "");
    return joined([date, clock], " ");
  }
  // Neighbours in the same group are joined by their separator; a gap (a part
  // left out between them) is a space, so "2026 12" is not read as a month.
  const chosen = TIME_PARTS.filter(has);
  return chosen.reduce((text, part, index) => {
    if (index === 0) return timeValue(now, part);
    const before = chosen[index - 1];
    const sameGroup = isDate(part) === isDate(before);
    const adjacent = TIME_PARTS.indexOf(part) === TIME_PARTS.indexOf(before) + 1;
    const separator = !sameGroup ? " " : !adjacent ? " " : isDate(part) ? "-" : ":";
    return `${text}${separator}${timeValue(now, part)}`;
  }, "");
}

function isDate(part: TimePart): boolean {
  return part === "year" || part === "month" || part === "day";
}
