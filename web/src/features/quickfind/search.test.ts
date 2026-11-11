import { describe, expect, it } from "vitest";
import { createBlock } from "../../core/block-tree";
import type { NotePage } from "../../core/types";
import { excerptOf, markRanges, quickFind } from "./search";

function page(id: string, title: string, lines: string[] = [], options: Partial<NotePage> = {}): NotePage {
  const by_id = Object.fromEntries(lines.map((text, index) => [`${id}_b${index}`, createBlock(`${id}_b${index}`, "text", {
    order: (index + 1) * 1024, content: [{ text }],
  })]));
  return {
    id, title, icon: "", kind: "document", parent_id: null, block_tree: { by_id },
    created_at: "2026-09-21T00:00:00Z", updated_at: `2026-09-2${lines.length}T00:00:00Z`, created_by: "a", updated_by: "a",
    ...options,
  };
}

const pages = (...list: NotePage[]) => Object.fromEntries(list.map((item) => [item.id, item]));

describe("what ⌘K finds", () => {
  it("ranks names that start with the words above names that hold them above lines", () => {
    const all = pages(
      page("a", "Weekly plan", ["nothing here"]),
      page("b", "Plan for the week"),
      page("c", "Groceries", ["a plan for dinner"]),
    );
    expect(quickFind(all, "plan").map((hit) => hit.page.id)).toEqual(["b", "a", "c"]);
  });

  it("needs every word, in the title or in one line, or spread over the page", () => {
    const all = pages(
      page("one", "Trip", ["book the flight to Lisbon", "pack a coat"]),
      page("two", "Notes", ["flight booked", "Lisbon hotel"]),
      page("three", "Other", ["flight only"]),
    );
    const hits = quickFind(all, "flight lisbon");
    expect(hits.map((hit) => hit.page.id)).toEqual(["one", "two"]);
    expect(hits[0].blockId).toBe("one_b0");
    expect(hits[0].lines).toBe(1);
  });

  it("cuts the line around the match, and marks the words", () => {
    const long = `${"filler ".repeat(40)}needle ${"filler ".repeat(40)}`;
    const excerpt = excerptOf(long, ["needle"]);
    expect(excerpt.text.startsWith("…")).toBe(true);
    expect(excerpt.text.length).toBeLessThan(130);
    const [[from, to]] = excerpt.marks;
    expect(excerpt.text.slice(from, to)).toBe("needle");
  });

  it("merges overlapping marks", () => {
    expect(markRanges("banana", ["ban", "nan", "xyz"])).toEqual([[0, 5]]);
  });

  it("leaves the trash out, and lists recent pages for no words", () => {
    const all = pages(page("a", "Old"), page("b", "New", ["x"]), page("t", "Trashed", [], { archived: true } as Partial<NotePage>));
    expect(quickFind(all, "trashed")).toEqual([]);
    expect(quickFind(all, "  ").map((hit) => hit.page.id)).toEqual(["b", "a"]);
  });
});
