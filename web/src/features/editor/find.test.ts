import { describe, expect, it } from "vitest";
import { createBlock } from "../../core/block-tree";
import { inlinePlainText, MENTION_CHAR } from "../../core/inline-content";
import type { Block, BlockTree, InlineContent } from "../../core/types";
import { applyEditorTransaction } from "./editor-commands";
import { findInText, findInTree, replaceMatches } from "./find";

function treeOf(...blocks: Array<Partial<Block> & { id: string; content: InlineContent }>): BlockTree {
  const by_id: Record<string, Block> = {};
  blocks.forEach((partial, index) => {
    by_id[partial.id] = { ...createBlock(partial.id), order: (index + 1) * 1024, ...partial };
  });
  return { by_id };
}

const plain = (text: string): InlineContent => [{ text }];
const textOf = (tree: BlockTree, id: string) => inlinePlainText(tree.by_id[id].content);

describe("findInText", () => {
  it("ignores case unless asked not to", () => {
    expect(findInText("Cat cat CAT", "cat")).toEqual([[0, 3], [4, 7], [8, 11]]);
    expect(findInText("Cat cat CAT", "cat", { caseSensitive: true, wholeWord: false })).toEqual([[4, 7]]);
  });

  it("takes the query literally, not as a pattern", () => {
    expect(findInText("a.b a+b axb", "a.b")).toEqual([[0, 3]]);
    expect(findInText("(x) [y] $5", "(x)")).toEqual([[0, 3]]);
  });

  it("finds whole words only when asked", () => {
    const whole = { caseSensitive: false, wholeWord: true };
    expect(findInText("cat concat cat_s cat!", "cat", whole)).toEqual([[0, 3], [17, 20]]);
    expect(findInText("a cat", "cat", whole)).toEqual([[2, 5]]);
  });

  it("does not look for nothing", () => {
    expect(findInText("abc", "")).toEqual([]);
    expect(findInText("abc", MENTION_CHAR)).toEqual([]);
  });

  it("finds Chinese words", () => {
    expect(findInText("你好，你好吗", "你好")).toEqual([[0, 2], [3, 5]]);
  });
});

describe("findInTree", () => {
  it("lists matches in reading order, block by block", () => {
    const tree = treeOf(
      { id: "a", content: plain("one two one") },
      { id: "b", content: plain("none") },
      { id: "c", content: plain("two") },
    );
    expect(findInTree(tree, "one")).toEqual([
      { blockId: "a", start: 0, end: 3 },
      { blockId: "a", start: 8, end: 11 },
      { blockId: "b", start: 1, end: 4 },
    ]);
  });

  it("does not find words across two lines, nor in a mention", () => {
    const tree = treeOf(
      { id: "a", content: plain("fo") },
      { id: "b", content: plain("o") },
      { id: "c", content: [{ text: "x" }, { text: MENTION_CHAR, mention: { kind: "date", date: "2026-01-01" } }, { text: "x" }] },
    );
    expect(findInTree(tree, "foo")).toEqual([]);
    expect(findInTree(tree, "x")).toHaveLength(2);
  });
});

describe("replaceMatches", () => {
  it("replaces one match and keeps the rest", () => {
    const tree = treeOf({ id: "a", content: plain("one two one") });
    const [first] = findInTree(tree, "one");
    const next = applyEditorTransaction(tree, replaceMatches(tree, [first], "1")!);
    expect(textOf(next, "a")).toBe("1 two one");
  });

  it("replaces every match, in several blocks, with text of another length", () => {
    const tree = treeOf(
      { id: "a", content: plain("one two one") },
      { id: "b", content: plain("one") },
    );
    const next = applyEditorTransaction(tree, replaceMatches(tree, findInTree(tree, "one"), "three")!);
    expect(textOf(next, "a")).toBe("three two three");
    expect(textOf(next, "b")).toBe("three");
  });

  it("deletes the words when the replacement is empty", () => {
    const tree = treeOf({ id: "a", content: plain("a-b-c") });
    const next = applyEditorTransaction(tree, replaceMatches(tree, findInTree(tree, "-"), "")!);
    expect(textOf(next, "a")).toBe("abc");
  });

  it("keeps the marks of the words it replaces, and of the words around them", () => {
    const tree = treeOf({ id: "a", content: [{ text: "say " }, { text: "hello", marks: { bold: true } }, { text: " now" }] });
    const next = applyEditorTransaction(tree, replaceMatches(tree, findInTree(tree, "hello"), "hi")!);
    expect(next.by_id.a.content).toEqual([{ text: "say " }, { text: "hi", marks: { bold: true } }, { text: " now" }]);
  });

  it("keeps a mention where it is", () => {
    const mention = { text: MENTION_CHAR, mention: { kind: "date" as const, date: "2026-01-01" } };
    const tree = treeOf({ id: "a", content: [{ text: "ab" }, mention, { text: "ab" }] });
    const next = applyEditorTransaction(tree, replaceMatches(tree, findInTree(tree, "ab"), "c")!);
    expect(next.by_id.a.content.map((run) => run.text)).toEqual(["c", MENTION_CHAR, "c"]);
    expect(next.by_id.a.content[1].mention).toEqual(mention.mention);
  });

  it("changes nothing when there is nothing to change", () => {
    expect(replaceMatches(treeOf({ id: "a", content: plain("x") }), [], "y")).toBeNull();
  });
});
