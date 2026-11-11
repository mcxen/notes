import { describe, expect, it } from "vitest";
import { createBlock } from "../../core/block-tree";
import type { Block, BlockTree } from "../../core/types";
import { blockTreeToMarkdown } from "./adapters";
import { applyEditorTransaction, splitIntoToggle } from "./editor-commands";
import { layout } from "./layout";

function tree(...blocks: Block[]): BlockTree {
  return { by_id: Object.fromEntries(blocks.map((block) => [block.id, block])) };
}

const toggle = (collapsed: boolean) => tree(
  createBlock("t", "toggle", { order: 1024, content: [{ text: "Title" }], collapsed }),
  createBlock("c1", "text", { parent_id: "t", order: 1024, content: [{ text: "inside one" }] }),
  createBlock("c2", "text", { parent_id: "t", order: 2048, content: [{ text: "inside two" }] }),
  createBlock("after", "text", { order: 2048, content: [{ text: "after" }] }),
);

describe("a toggle in the layout", () => {
  it("shows what it holds while open", () => {
    expect(layout(toggle(false)).rows.map((row) => row.block.id)).toEqual(["t", "c1", "c2", "after"]);
  });

  it("hides what it holds while folded, and the line after it stays", () => {
    expect(layout(toggle(true)).rows.map((row) => row.block.id)).toEqual(["t", "after"]);
  });

  it("can be asked to show a reader's own choice", () => {
    expect(layout(toggle(true), () => true).rows).toHaveLength(4);
    expect(layout(toggle(false), () => false).rows).toHaveLength(2);
  });

  it("is only folded when it says true", () => {
    expect(createBlock("x", "toggle", { collapsed: false }).collapsed).toBeUndefined();
    expect(createBlock("x", "toggle", { collapsed: true }).collapsed).toBe(true);
    // A fold on a line that is not a toggle means nothing.
    expect(createBlock("x", "bullet", { collapsed: true }).collapsed).toBeUndefined();
  });
});

describe("Enter in an open toggle", () => {
  it("cuts its words at the caret and starts its first line with the rest", () => {
    const open = toggle(false);
    const result = splitIntoToggle(open, "t", 2, "n");
    expect(result).not.toBeNull();
    const next = applyEditorTransaction(open, result!);
    expect(next.by_id.t.content).toEqual([{ text: "Ti" }]);
    expect(next.by_id.n).toMatchObject({ parent_id: "t", kind: "text", content: [{ text: "tle" }] });
    expect(next.by_id.n.order).toBeLessThan(next.by_id.c1.order);
    expect(result!.focus).toEqual({ id: "n", offset: 0 });
  });
});

describe("a toggle outside Notes", () => {
  it("is written with everything it holds, folded or not", () => {
    const text = blockTreeToMarkdown(toggle(true), () => "");
    expect(text).toBe("- Title\n  inside one\n  inside two\nafter");
  });
});

import { blocksHaveMark, colorBlocks, markBlocks, selectionStyle } from "./editor-commands";

describe("styling selected lines at once", () => {
  const lines = () => tree(
    createBlock("a", "text", { order: 1024, content: [{ text: "one" }] }),
    createBlock("b", "bullet", { order: 2048, content: [{ text: "two " }, { text: "bold", marks: { bold: true } }], color: "red" }),
    createBlock("c", "code", { order: 3072, content: [{ text: "code()" }] }),
    createBlock("d", "divider", { order: 4096 }),
  );

  it("sets a mark across every word of every line, and not in code", () => {
    const source = lines();
    expect(blocksHaveMark(source, ["a", "b", "c", "d"], "bold")).toBe(false);
    const next = applyEditorTransaction(source, markBlocks(source, ["a", "b", "c", "d"], "bold")!);
    expect(next.by_id.a.content).toEqual([{ text: "one", marks: { bold: true } }]);
    expect(next.by_id.b.content).toEqual([{ text: "two bold", marks: { bold: true } }]);
    expect(next.by_id.c.content).toEqual([{ text: "code()" }]);
    expect(blocksHaveMark(next, ["a", "b", "c", "d"], "bold")).toBe(true);
  });

  it("clears it when every line already has it", () => {
    const once = applyEditorTransaction(lines(), markBlocks(lines(), ["a", "b"], "italic")!);
    const twice = applyEditorTransaction(once, markBlocks(once, ["a", "b"], "italic")!);
    expect(twice.by_id.a.content).toEqual([{ text: "one" }]);
    expect(blocksHaveMark(twice, ["a", "b"], "italic")).toBe(false);
  });

  it("does nothing for lines with no words", () => {
    expect(markBlocks(lines(), ["d"], "bold")).toBeNull();
    expect(blocksHaveMark(lines(), ["d"], "bold")).toBe(false);
  });

  it("says what the lines share", () => {
    expect(selectionStyle(lines(), ["a", "b"])).toEqual({});
    expect(selectionStyle(lines(), ["b"])).toEqual({ kind: "bullet", color: "red" });
    const coloured = applyEditorTransaction(lines(), colorBlocks(lines(), ["a", "b"], "blue")!);
    expect(selectionStyle(coloured, ["a", "b"])).toEqual({ color: "blue" });
  });
});
