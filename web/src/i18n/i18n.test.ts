import { describe, expect, it } from "vitest";
import { zh } from "./zh";

const files = import.meta.glob<string>(["../**/*.ts", "../**/*.tsx", "!../**/*.test.*", "!../i18n/**"], {
  query: "?raw",
  import: "default",
  eager: true,
});

/** The texts the pages pass to t() and tp() as literals. */
function literals(): Map<string, string> {
  const found = new Map<string, string>();
  const call = /\bt\(\s*"((?:[^"\\]|\\.)*)"|\btp\([^,]+,\s*"(?:[^"\\]|\\.)*",\s*"((?:[^"\\]|\\.)*)"/g;
  for (const [file, text] of Object.entries(files)) {
    for (const match of text.matchAll(call)) {
      const raw = match[1] ?? match[2];
      if (raw !== undefined) found.set(JSON.parse(`"${raw}"`) as string, file);
    }
  }
  return found;
}

describe("the Chinese texts", () => {
  it("cover every text the pages say through t()", () => {
    const missing = [...literals()].filter(([text]) => !(text in zh)).map(([text, file]) => `${text}  (${file})`);
    expect(missing).toEqual([]);
  });

  it("keep the places of the English: every {name} is still there", () => {
    const places = (text: string) => (text.match(/\{\w+\}/g) ?? []).toSorted().join(",");
    const wrong = Object.entries(zh).filter(([en, cn]) => places(en) !== places(cn)).map(([en]) => en);
    expect(wrong).toEqual([]);
  });
});
