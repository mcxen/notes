import { describe, expect, it } from "vitest";
import { createBlock } from "../../core/block-tree";
import type { BlockTree } from "../../core/types";
import { currentHeading, headingsOf } from "./headings";

function tree(...blocks: Array<[string, string, string]>): BlockTree {
  return {
    by_id: Object.fromEntries(blocks.map(([id, kind, text], index) => [
      id,
      createBlock(id, kind as never, { order: (index + 1) * 1024, content: text ? [{ text }] : [] }),
    ])),
  };
}

describe("the headings of a page", () => {
  it("are its heading blocks, in reading order, with their levels", () => {
    const found = headingsOf(tree(
      ["a", "heading1", "Plan"], ["b", "text", "words"], ["c", "heading2", "Steps"], ["d", "heading3", "One"], ["e", "bullet", "item"],
    ));
    expect(found).toEqual([
      { id: "a", level: 1, text: "Plan" }, { id: "c", level: 2, text: "Steps" }, { id: "d", level: 3, text: "One" },
    ]);
  });

  it("leave out a heading with no words, and tidy the spaces of one with", () => {
    const found = headingsOf(tree(["a", "heading2", ""], ["b", "heading2", "  Two \n words  "]));
    expect(found).toEqual([{ id: "b", level: 2, text: "Two words" }]);
  });
});

describe("the heading a page is at", () => {
  it("is the last whose top has passed the line, and the first before any has", () => {
    expect(currentHeading([300, 700, 1200], 96)).toBe(0);
    expect(currentHeading([-500, 40, 700], 96)).toBe(1);
    expect(currentHeading([-900, -400, -10], 96)).toBe(2);
    expect(currentHeading([], 96)).toBe(0);
  });
});
