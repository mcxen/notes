// @vitest-environment happy-dom
import { describe, expect, it } from "vitest";
import { orderedBlocks } from "../../core/block-tree";
import { inlinePlainText } from "../../core/inline-content";
import type { Block, BlockTree } from "../../core/types";
import { htmlToFragment, MAX_PASTED_IMAGES } from "./html-paste";

let counter = 0;
const id = () => `block_${(counter += 1).toString(16).padStart(8, "0")}`;

function read(html: string) {
  counter = 0;
  return htmlToFragment(html, id);
}

/** The blocks of a fragment in reading order, each with its depth: `kind text` at two spaces a level. */
function outline(tree: BlockTree, parent: string | null = null, depth = 0): string[] {
  return orderedBlocks(tree, parent).flatMap((block: Block) => [
    `${"  ".repeat(depth)}${block.kind} ${JSON.stringify(inlinePlainText(block.content))}`,
    ...outline(tree, block.id, depth + 1),
  ]);
}

describe("content copied from a web page", () => {
  it("keeps headings, paragraphs, links, lists, quotes, code and rules, and the pictures", () => {
    const fragment = read(`<meta charset="utf-8"><!--StartFragment-->
      <article class="markdown-body">
        <h1>Project <a href="https://example.com/#anchor"><svg><path d="M0"/></svg></a></h1>
        <p>A <strong>bold</strong> and <em>slanted</em> word, with <a href="https://example.com/docs">a link</a>.</p>
        <p><a href="https://ci.example.com"><img src="https://img.shields.io/badge/build-passing.svg" alt="build"></a></p>
        <h2>Install</h2>
        <ul><li>First<ul><li>Nested</li></ul></li><li><input type="checkbox" checked> Done</li></ul>
        <ol><li><p>One</p></li><li>Two</li></ol>
        <blockquote><p>Said</p><p>Twice</p></blockquote>
        <div class="highlight highlight-source-js"><pre>const a = 1;\nconsole.log(a);\n</pre><clipboard-copy>copy</clipboard-copy></div>
        <hr>
        <p><img src="https://example.com/shot.png" alt="Screenshot"></p>
      </article><!--EndFragment-->`);
    expect(fragment).not.toBeNull();
    expect(outline(fragment!.tree)).toEqual([
      'heading1 "Project"',
      'text "A bold and slanted word, with a link."',
      'text "https://img.shields.io/badge/build-passing.svg"'.replace("https://img.shields.io/badge/build-passing.svg", "build"),
      'heading2 "Install"',
      'bullet "First"',
      '  bullet "Nested"',
      'todo "Done"',
      'numbered "One"',
      'numbered "Two"',
      'quote "Said"',
      '  text "Twice"',
      'code "const a = 1;\\nconsole.log(a);"',
      "divider \"\"",
      'text "Screenshot"',
    ]);
    const blocks = Object.values(fragment!.tree.by_id);
    expect(blocks.find((block) => block.kind === "code")?.language).toBe("js");
    expect(blocks.find((block) => block.kind === "todo")?.checked).toBe(true);
    const marks = blocks.find((block) => inlinePlainText(block.content).startsWith("A bold"))!.content.map((run) => run.marks ?? {});
    expect(marks.some((mark) => mark.bold)).toBe(true);
    expect(marks.some((mark) => mark.italic)).toBe(true);
    expect(marks.some((mark) => mark.link === "https://example.com/docs")).toBe(true);
  });

  it("names each picture, and stands a line with its address as a link in its place", () => {
    const fragment = read('<p>Before</p><p><img src="https://example.com/a.png" alt="First"> <img src="https://example.com/b.jpg"></p><p>After</p>')!;
    expect(fragment.images.map((image) => image.src)).toEqual(["https://example.com/a.png", "https://example.com/b.jpg"]);
    const [first, second] = fragment.images.map((image) => fragment.tree.by_id[image.blockId]);
    expect(first.content).toEqual([{ text: "First", marks: { link: "https://example.com/a.png" } }]);
    // A picture with no words stands for itself by its address.
    expect(inlinePlainText(second.content)).toBe("https://example.com/b.jpg");
    expect(outline(fragment.tree).map((line) => line.split(" ")[0])).toEqual(["text", "text", "text", "text"]);
  });

  it("takes pictures as data and leaves out what cannot be fetched or is not a picture", () => {
    const fragment = read(`<p>Words</p>
      <img src="data:image/png;base64,iVBORw0KGgo=" alt="inline">
      <img src="data:image/svg+xml;base64,PHN2Zz48L3N2Zz4=" alt="vector">
      <img src="/relative/path.png" alt="relative">
      <img src="javascript:alert(1)" alt="script">
      <img src="https://t.example.com/pixel.gif" width="1" height="1">
      <img src="https://example.com/real.png" alt="real" width="300">`)!;
    expect(fragment.images.map((image) => image.src)).toEqual(["data:image/png;base64,iVBORw0KGgo=", "https://example.com/real.png"]);
    const words = outline(fragment.tree);
    // A picture that cannot be had keeps its words, one that is a pixel is nothing.
    expect(words).toContain('text "vector"');
    expect(words).toContain('text "relative"');
    expect(words).toContain('text "script"');
    expect(words.join("\n")).not.toContain("pixel");
  });

  it("reads tables as tables and drops what runs", () => {
    const fragment = read(`<p>Intro</p><table><thead><tr><th>Name</th><th>Qty</th></tr></thead><tbody><tr><td>Apple</td><td>3</td></tr></tbody></table>
      <script>alert(1)</script><style>p{color:red}</style><p onclick="steal()">Kept <a href="javascript:alert(1)">words</a></p><button>Buy</button>`)!;
    const table = Object.values(fragment.tree.by_id).find((block) => block.kind === "table")!;
    expect(Object.keys(table.table!.rows)).toHaveLength(2);
    expect(table.table!.header_row).toBe(true);
    const text = outline(fragment.tree).join("\n");
    expect(text).toContain('text "Kept words"');
    expect(text).not.toMatch(/alert|steal|Buy|color:red/);
    const link = Object.values(fragment.tree.by_id).flatMap((block) => block.content).find((run) => run.text === "words");
    expect(link?.marks?.link).toBeUndefined();
  });

  it("is not blocks for a line of unmarked words, which the editor pastes among the words", () => {
    expect(read("<meta charset='utf-8'><span style='color: red'>just some words</span>")).toBeNull();
    expect(read("<b style='font-weight:normal' id='docs-internal-guid-1'><span>plain text from Docs</span></b>")).toBeNull();
    expect(read("")).toBeNull();
    // But a link, or a picture, or two lines is more than the words.
    expect(read('<span>see <a href="https://example.com">this</a></span>')).not.toBeNull();
    expect(read("<p>one</p><p>two</p>")).not.toBeNull();
  });

  it("reads Google Docs' styles as marks", () => {
    const fragment = read('<b style="font-weight:normal"><p><span style="font-weight:700">Bold</span> and <span style="font-style:italic;text-decoration:line-through">both</span></p></b>')!;
    const runs = Object.values(fragment.tree.by_id)[0].content;
    expect(runs.find((run) => run.text === "Bold")?.marks).toEqual({ bold: true });
    expect(runs.find((run) => run.text === "both")?.marks).toMatchObject({ italic: true, strike: true });
  });

  it("takes only so many pictures and so many blocks", () => {
    const many = Array.from({ length: MAX_PASTED_IMAGES + 10 }, (_, i) => `<img src="https://example.com/${i}.png" alt="n${i}">`).join("");
    const fragment = read(many)!;
    expect(fragment.images).toHaveLength(MAX_PASTED_IMAGES);
    // The ones past the limit are only their words.
    expect(Object.keys(fragment.tree.by_id).length).toBe(MAX_PASTED_IMAGES + 10);
  });
});
