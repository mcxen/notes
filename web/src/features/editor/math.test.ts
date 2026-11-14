import { describe, expect, it } from "vitest";
import { createBlock } from "../../core/block-tree";
import { inlinePlainText } from "../../core/inline-content";
import { blockTreeToMarkdown, markdownShortcut, markdownToBlockTree } from "./adapters";
import { blockShortcut } from "./block-registry";
import { slashCommands } from "./SlashMenu";

describe("an equation block", () => {
  it("is typed with $$ and picked as Equation", () => {
    expect(blockShortcut("$$")).toEqual({ kind: "math" });
    expect(markdownShortcut("$$")).toEqual({ kind: "math" });
    expect(slashCommands("equation").some((command) => command.kind === "math" && command.label === "Equation")).toBe(true);
    for (const alias of ["math", "latex", "tex", "formula", "katex"]) {
      expect(slashCommands(alias).some((command) => command.kind === "math")).toBe(true);
    }
  });

  it("keeps the TeX source as one plain run", () => {
    const block = createBlock("m", "math", { content: [{ text: "a_b", marks: { italic: true } }, { text: "c", marks: { code: true } }] });
    expect(block.content).toEqual([{ text: "a_bc" }]);
  });

  it("reads and writes a display equation", () => {
    let n = 0;
    const tree = markdownToBlockTree("$$\nE = mc^2\n$$\n$$a_b$$\n```\n$$\n```\n$$x\n", () => `b${n += 1}`);
    const blocks = Object.values(tree.by_id).toSorted((left, right) => left.order - right.order);
    expect(blocks.map((block) => block.kind)).toEqual(["math", "math", "code", "text"]);
    expect(inlinePlainText(blocks[0].content)).toBe("E = mc^2");
    expect(blocks[1].content).toEqual([{ text: "a_b" }]);
    expect(inlinePlainText(blocks[2].content)).toBe("$$");
    expect(inlinePlainText(blocks[3].content)).toBe("$$x");
    const markdown = blockTreeToMarkdown(tree, () => "");
    expect(markdown).toContain("$$\nE = mc^2\n$$");
    expect(markdown).toContain("$$\na_b\n$$");
  });

  it("writes an empty equation without a blank line inside", () => {
    const tree = markdownToBlockTree("$$\n$$", () => "e");
    const block = Object.values(tree.by_id)[0];
    expect(block.kind).toBe("math");
    expect(inlinePlainText(block.content)).toBe("");
    expect(blockTreeToMarkdown(tree, () => "")).toBe("$$\n$$");
  });

  it("drops the list indent from an equation nested under an item", () => {
    const tree = markdownToBlockTree("- item\n  $$\n  x^2\n  $$", () => "n");
    const math = Object.values(tree.by_id).find((block) => block.kind === "math");
    expect(inlinePlainText(math?.content ?? [])).toBe("x^2");
  });
});
