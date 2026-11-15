import { describe, expect, it } from "vitest";
import { createBlock } from "../../core/block-tree";
import type { BlockTree } from "../../core/types";
import { pageToHtml } from "./html";

function page(blocks: ReturnType<typeof createBlock>[]): string {
  const tree: BlockTree = { by_id: Object.fromEntries(blocks.map((block) => [block.id, block])) };
  return pageToHtml("Notes", tree, () => "", undefined, "en");
}

describe("HTML export of an equation", () => {
  it("writes the TeX source in a fenced pre", () => {
    const html = page([createBlock("m", "math", { content: [{ text: "E=mc^2" }] })]);
    expect(html).toContain('<pre class="math">$$E=mc^2$$</pre>');
  });

  it("escapes the source", () => {
    const html = page([createBlock("m", "math", { content: [{ text: "a<b & c" }] })]);
    expect(html).toContain('<pre class="math">$$a&lt;b &amp; c$$</pre>');
  });

  it("keeps a newline in the source", () => {
    const html = page([createBlock("m", "math", { content: [{ text: "a\nb" }] })]);
    expect(html).toContain("<pre class=\"math\">$$a\nb$$</pre>");
  });
});

describe("HTML export of columns", () => {
  it("writes the columns side by side, each with its share", () => {
    const html = page([
      createBlock("f", "columns", { order: 1024 }),
      createBlock("l", "column", { parent_id: "f", order: 1024, span: 1.5 }),
      createBlock("r", "column", { parent_id: "f", order: 2048 }),
      createBlock("a", "text", { parent_id: "l", content: [{ text: "left" }] }),
      createBlock("b", "text", { parent_id: "r", content: [{ text: "right" }] }),
    ]);
    expect(html).toMatch(/<div class="columns"[^>]*display:flex[^>]*><div class="column" style="flex:1.5 1 0;min-width:0"><p>left<\/p><\/div><div class="column" style="flex:1 1 0;min-width:0"><p>right<\/p><\/div><\/div>/);
  });
});
