import { describe, expect, it } from "vitest";
import { createBlock } from "../../core/block-tree";
import type { Block, BlockTree } from "../../core/types";
import { applyEditorTransaction, moveBlocks } from "./editor-commands";
import { layout } from "./layout";

function tree(...blocks: Block[]): BlockTree {
  return { by_id: Object.fromEntries(blocks.map((block) => [block.id, block])) };
}

const page = () => tree(
  createBlock("list", "bullet", { order: 1024, content: [{ text: "List" }] }),
  createBlock("c1", "bullet", { parent_id: "list", order: 1024, content: [{ text: "one" }] }),
  createBlock("c2", "bullet", { parent_id: "list", order: 2048, content: [{ text: "two" }] }),
  createBlock("after", "text", { order: 2048, content: [{ text: "after" }] }),
  createBlock("moved", "text", { order: 3072, content: [{ text: "moved" }] }),
);

const rowsAfter = (edge: "above" | "below" | "inside", id: string) => {
  const before = page();
  const move = moveBlocks(before, ["moved"], { id, edge });
  expect(move).not.toBeNull();
  return layout(applyEditorTransaction(before, move!)).rows.map((row) => row.block.id);
};

describe("a line dropped on one that holds others", () => {
  it("lands first of what it holds when dropped inside: where the mark is drawn", () => {
    expect(rowsAfter("inside", "list")).toEqual(["list", "moved", "c1", "c2", "after"]);
  });

  it("lands after everything it holds when dropped below", () => {
    expect(rowsAfter("below", "list")).toEqual(["list", "c1", "c2", "moved", "after"]);
  });

  it("lands before it when dropped above", () => {
    expect(rowsAfter("above", "list")).toEqual(["moved", "list", "c1", "c2", "after"]);
  });

  it("lands inside a line that holds nothing yet", () => {
    expect(rowsAfter("inside", "after")).toEqual(["list", "c1", "c2", "after", "moved"]);
  });
});
