import { describe, expect, it } from "vitest";
import { createBlock, orderedBlocks } from "../../core/block-tree";
import { inlinePlainText } from "../../core/inline-content";
import type { Block, BlockTree } from "../../core/types";
import {
  applyEditorTransaction,
  addColumn,
  columnShares,
  deleteBlocks,
  MAX_COLUMNS,
  newColumns,
  resizeColumns,
  unwrapColumns,
  withTidyColumns,
  type EditorTransaction,
} from "./editor-commands";
import { blockTreeToMarkdown } from "./adapters";
import { layout } from "./layout";

let counter = 0;
const id = () => `block_${(counter += 1).toString().padStart(4, "0")}`;
const text = (blockId: string, words: string, parent: string | null, order: number): Block => (
  createBlock(blockId, "text", { parent_id: parent, order, content: [{ text: words }] })
);
const tree = (...blocks: Block[]): BlockTree => ({ by_id: Object.fromEntries(blocks.map((block) => [block.id, block])) });

/** The words of the lines of a tree in reading order, `|` marking where a column ends. */
function reading(source: BlockTree): string {
  const shown = layout(source);
  const out: string[] = [];
  const walk = (items: typeof shown.items) => {
    for (const item of items) {
      if (item.kind === "row") out.push(inlinePlainText(item.row.block.content) || "·");
      else if (item.kind === "columns") item.columns.forEach((column, at) => { if (at) out.push("|"); walk(column.items); });
      else walk(item.items);
    }
  };
  walk(shown.items);
  return out.join(" ");
}

/** A page of `before`, a frame of two columns holding "a" and "b" and "c", and `after`. */
function framed(): BlockTree {
  return tree(
    text("before", "before", null, 1024),
    createBlock("frame", "columns", { order: 2048 }),
    createBlock("left", "column", { parent_id: "frame", order: 1024 }),
    createBlock("right", "column", { parent_id: "frame", order: 2048 }),
    text("a", "a", "left", 1024),
    text("b", "b", "left", 2048),
    text("c", "c", "right", 1024),
    text("after", "after", null, 3072),
  );
}

describe("columns", () => {
  it("are laid out as a frame of cells, their lines read in order and at the frame's depth", () => {
    const shown = layout(framed());
    expect(shown.rows.map((row) => `${row.block.id}@${row.depth}`)).toEqual(["before@0", "a@0", "b@0", "c@0", "after@0"]);
    expect(reading(framed())).toBe("before a b | c after");
  });

  it("are written as their lines one after another in Markdown", () => {
    expect(blockTreeToMarkdown(framed(), () => "")).toBe("before\na\nb\nc\nafter");
  });

  it("are made after a line, or in place of an empty one", () => {
    const page = tree(text("one", "words", null, 1024), text("two", "", null, 2048));
    const after = newColumns(page, "one", 3, id)!;
    expect(reading(applyEditorTransaction(page, after.transaction))).toBe("words · | · | · ·");
    expect(after.transaction.focus?.id).toBe(after.focusId);

    const instead = applyEditorTransaction(page, newColumns(page, "two", 2, id)!.transaction);
    expect(instead.by_id.two).toBeUndefined();
    expect(reading(instead)).toBe("words · | ·");
    expect(newColumns(page, "one", 1, id)).toBeNull();
    expect(newColumns(page, "one", MAX_COLUMNS + 1, id)).toBeNull();
  });

  it("are followed by a line when they end the page, to write below them", () => {
    const page = tree(text("one", "words", null, 1024));
    const made = newColumns(page, "one", 2, id)!;
    const mended = applyEditorTransaction(page, withTidyColumns(page, made.transaction, id));
    expect(reading(mended)).toBe("words · | · ·");
    // Deleting that line brings another back; one with a line after it is left alone.
    const last = orderedBlocks(mended).at(-1)!;
    const again = applyEditorTransaction(mended, withTidyColumns(mended, { mutations: [{ action: "delete", blockId: last.id }] }, id));
    expect(orderedBlocks(again).at(-1)?.kind).toBe("text");
    const framedPage = framed();
    expect(withTidyColumns(framedPage, { mutations: [{ action: "delete", blockId: "a" }] }, id).mutations).toHaveLength(1);
  });

  it("take another column after the last, up to the most", () => {
    let page = framed();
    const first = addColumn(page, "frame", id)!;
    page = applyEditorTransaction(page, first.transaction);
    expect(reading(page)).toBe("before a b | c | · after");
    expect(page.by_id[first.focusId].parent_id).toBe(orderedBlocks(page, "frame").at(-1)!.id);
    while (addColumn(page, "frame", id)) page = applyEditorTransaction(page, addColumn(page, "frame", id)!.transaction);
    expect(orderedBlocks(page, "frame")).toHaveLength(MAX_COLUMNS);
    expect(addColumn(page, "before", id)).toBeNull();
  });

  it("do not nest", () => {
    const inside = applyEditorTransaction(framed(), { mutations: [] });
    expect(newColumns(inside, "a", 2, id)).toBeNull();
  });

  it("are taken apart where they stood, their lines in order", () => {
    const apart = applyEditorTransaction(framed(), unwrapColumns(framed(), "frame")!);
    expect(reading(apart)).toBe("before a b c after");
    expect(Object.values(apart.by_id).some((block) => block.kind === "column" || block.kind === "columns")).toBe(false);
    expect(orderedBlocks(apart).map((block) => block.id)).toEqual(["before", "a", "b", "c", "after"]);
  });

  it("lose a column that is emptied, and a frame left with one column", () => {
    const page = framed();
    // The only line of the right column goes: the column goes, and the frame with it.
    const removed: EditorTransaction = { mutations: [{ action: "delete", blockId: "c" }] };
    const mended = applyEditorTransaction(page, withTidyColumns(page, removed, id));
    expect(reading(mended)).toBe("before a b after");
    expect(Object.values(mended.by_id).some((block) => block.kind === "column" || block.kind === "columns")).toBe(false);
    // Deleting a line that leaves every column something changes nothing else.
    const one: EditorTransaction = { mutations: [{ action: "delete", blockId: "a" }] };
    expect(reading(applyEditorTransaction(page, withTidyColumns(page, one, id)))).toBe("before b | c after");
  });

  it("are mended in the step of the change that broke them", () => {
    const page = framed();
    const wiped = deleteBlocks(page, ["a", "b"], id())!;
    const mended = withTidyColumns(page, wiped, id);
    expect(mended.mutations.length).toBeGreaterThan(wiped.mutations.length);
    expect(reading(applyEditorTransaction(page, mended))).toBe("before c after");
  });

  it("let go of the lines of a column that is in no frame", () => {
    const loose = tree(
      createBlock("col", "column", { order: 1024 }),
      text("x", "x", "col", 1024),
      text("y", "y", null, 2048),
    );
    // Drawn as lines at once, whatever has not mended it yet.
    expect(reading(loose)).toBe("x y");
    // A change that moves a line mends it.
    const moved: EditorTransaction = { mutations: [{ action: "put", block: { ...loose.by_id.y, parent_id: null, order: 3072 } }, { action: "delete", blockId: "zz" }] };
    const mended = applyEditorTransaction(loose, withTidyColumns(loose, moved, id));
    expect(reading(mended)).toBe("x y");
    expect(mended.by_id.col).toBeUndefined();
  });

  it("leave a page with none exactly as it was, and cost it nothing to check", () => {
    const page = tree(text("a", "a", null, 1024), text("b", "b", null, 2048));
    const change: EditorTransaction = { mutations: [{ action: "delete", blockId: "a" }] };
    expect(withTidyColumns(page, change, id)).toBe(change);
  });

  it("are resized by the pair, within the least a column may have", () => {
    const page = framed();
    expect(columnShares(page, "frame").map((entry) => entry.share)).toEqual([0.5, 0.5]);
    const resized = applyEditorTransaction(page, resizeColumns(page, "left", 0.7)!);
    const [left, right] = columnShares(resized, "frame");
    expect(left.share).toBeCloseTo(0.7, 3);
    expect(right.share).toBeCloseTo(0.3, 3);
    expect(resized.by_id.left.span).toBeCloseTo(1.4, 3);
    const narrow = columnShares(applyEditorTransaction(page, resizeColumns(page, "left", 0.01)!), "frame");
    expect(narrow[0].share).toBeCloseTo(0.12, 3);
    expect(resizeColumns(page, "right", 0.5)).toBeNull();
  });
});
