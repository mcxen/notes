import { describe, expect, it } from "vitest";
import { createBlock } from "../../core/block-tree";
import type { Block, NotePage, StoredFile } from "../../core/types";
import { buildExport, planExport, safeName } from "./build";

function page(id: string, title: string, blocks: Block[], extra: Partial<NotePage> = {}): NotePage {
  return {
    id, kind: "document", icon: "", title, parent_id: null,
    block_tree: { by_id: Object.fromEntries(blocks.map((block) => [block.id, block])) },
    created_at: "2026", updated_at: "2026", created_by: "a", updated_by: "a", ...extra,
  } as NotePage;
}
const png: StoredFile = { path: "files/p/f1/pic.png", owner: "me", name: "pic.png", size: 3, type: "image/png" };
const pdf: StoredFile = { path: "files/p/f2/a-b.pdf", owner: "me", name: "a b.pdf", size: 3, type: "application/pdf" };

const pages = {
  root: page("root", "Plan: 2026/Q1", [
    createBlock("a", "heading2", { order: 1024, content: [{ text: "Goals" }] }),
    createBlock("b", "file", { order: 2048, file: png, content: [{ text: "caption" }] }),
    createBlock("c", "file", { order: 3072, file: pdf }),
    createBlock("d", "text", { order: 4096, content: [{ text: "see " }, { text: "⁣", mention: { kind: "image", file: png } }] }),
  ]),
  kid: page("kid", "Kid", [createBlock("k", "bullet", { order: 1024, content: [{ text: "child line" }] })], { parent_id: "root" }),
  twin: page("twin", "Kid", [createBlock("t", "text", { order: 1024, content: [{ text: "second" }] })], { parent_id: "root" }),
  gone: page("gone", "Trashed", [], { parent_id: "root", archived: true }),
} as Record<string, NotePage>;
const titleOf = (id: string) => pages[id]?.title ?? "";
const bytesOf = async () => new Uint8Array([1, 2, 3]);

function names(zip: Uint8Array): string[] {
  // The file names in the central directory, in order.
  const view = new DataView(zip.buffer, zip.byteOffset);
  const end = zip.length - 22;
  const count = view.getUint16(end + 10, true);
  let at = view.getUint32(end + 16, true);
  const found: string[] = [];
  for (let i = 0; i < count; i += 1) {
    const length = view.getUint16(at + 28, true);
    found.push(new TextDecoder().decode(zip.subarray(at + 46, at + 46 + length)));
    at += 46 + length + view.getUint16(at + 30, true) + view.getUint16(at + 32, true);
  }
  return found;
}

describe("exporting a page", () => {
  it("makes a safe file name of any title", () => {
    expect(safeName("Plan: 2026/Q1")).toBe("Plan 2026 Q1");
    expect(safeName("..  ")).toBe("Untitled");
    expect(safeName("a".repeat(200)).length).toBe(80);
  });

  it("plans the page, and its sub-pages when asked, never the trash", () => {
    expect(planExport(pages, "root", { subpages: false }, titleOf).pageIds).toEqual(["root"]);
    const plan = planExport(pages, "root", { subpages: true }, titleOf);
    expect(plan.pageIds).toEqual(["root", "kid", "twin"]);
    expect(plan.files.map((file) => file.name)).toEqual(["pic.png", "a b.pdf"]);
  });

  it("is one Markdown file when it holds no files and no sub-pages", async () => {
    const result = await buildExport({ pages, rootId: "kid", options: { format: "markdown", subpages: false, files: true }, titleOf, readFile: bytesOf });
    expect(result.name).toBe("Kid.md");
    expect(new TextDecoder().decode(result.bytes)).toBe("# Kid\n\n- child line\n");
  });

  it("is a zip with assets, and the pages link them", async () => {
    const result = await buildExport({ pages, rootId: "root", options: { format: "markdown", subpages: true, files: true }, titleOf, readFile: bytesOf });
    expect(result.name).toBe("Plan 2026 Q1.zip");
    expect(result.pages).toBe(3);
    expect(result.files).toBe(2);
    expect(names(result.bytes)).toEqual([
      "Plan 2026 Q1.md", "Plan 2026 Q1/Kid.md", "Plan 2026 Q1/Kid 2.md", "assets/1-pic.png", "assets/2-a b.pdf",
    ]);
    const text = new TextDecoder().decode(result.bytes);
    expect(text).toContain("![caption](assets/1-pic.png)");
    expect(text).toContain("[a b.pdf](assets/2-a%20b.pdf)");
    expect(text).toContain("see ![pic.png](assets/1-pic.png)");
    // A sub-page is a folder down, so its links would climb out of it.
    expect(names(result.bytes).some((n) => n.startsWith("Plan 2026 Q1/"))).toBe(true);
  });

  it("leaves the files out when asked, and says their names", async () => {
    const result = await buildExport({ pages, rootId: "root", options: { format: "markdown", subpages: false, files: false }, titleOf, readFile: bytesOf });
    expect(result.name).toBe("Plan 2026 Q1.md");
    const text = new TextDecoder().decode(result.bytes);
    expect(text).toContain("[a b.pdf](a%20b.pdf)");
    expect(text).not.toContain("assets/");
  });

  it("writes HTML with images, links and the title", async () => {
    const result = await buildExport({ pages, rootId: "root", options: { format: "html", subpages: false, files: true }, titleOf, readFile: bytesOf });
    expect(result.name).toBe("Plan 2026 Q1.zip");
    const html = new TextDecoder().decode(result.bytes);
    expect(html).toContain("<title>Plan: 2026/Q1</title>");
    expect(html).toContain('<figure><img src="assets/1-pic.png"');
    expect(html).toContain('<a href="assets/2-a%20b.pdf">a b.pdf</a>');
    expect(html).toContain("<h2>Goals</h2>");
  });

  it("skips a file whose bytes cannot be read, and still writes the page", async () => {
    const result = await buildExport({ pages, rootId: "root", options: { format: "markdown", subpages: false, files: true }, titleOf, readFile: async (file) => (file.name === "pic.png" ? null : new Uint8Array([9])) });
    expect(result.files).toBe(1);
    expect(new TextDecoder().decode(result.bytes)).toContain("[pic.png](pic.png)");
  });
});
