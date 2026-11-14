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
