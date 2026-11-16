import { describe, expect, it } from "vitest";
import { createBlock, pageLine } from "../../core/block-tree";
import type { Block, NotePage, StoredFile } from "../../core/types";
import { contentName, newPublicationId, publishPages, sourcesIn, unpublishPages, type Drawn, type ShareTransport, type SharedPage } from "./publisher";

function page(id: string, title: string, blocks: Block[], extra: Partial<NotePage> = {}): NotePage {
  return {
    id, kind: "document", icon: "", title, parent_id: null,
    block_tree: { by_id: Object.fromEntries(blocks.map((block) => [block.id, block])) },
    created_at: "2026", updated_at: "2026", created_by: "a", updated_by: "a", ...extra,
  } as NotePage;
}

const png: StoredFile = { path: "files/p/f1/pic.png", owner: "me", name: "pic.png", size: 3, type: "image/png" };
const big: StoredFile = { path: "files/p/f2/huge.bin", owner: "me", name: "huge.bin", size: 99, type: "application/octet-stream" };

const board = page("board", "Product board", [
  createBlock("b1", "text", { order: 1024, content: [{ text: "What we build." }] }),
], {
  kind: "database",
  database: {
    properties: [
      { id: "s", name: "Status", type: "status", status_options: [{ id: "o1", name: "Doing", color: "blue", group: "progress" }] },
      { id: "c", name: "Area", type: "select", options: ["Editor"] },
    ],
    views: [],
  } as unknown as NotePage["database"],
});
const pages: Record<string, NotePage> = {
  board,
  card1: page("card1", "Columns", [
    createBlock("c1", "text", { order: 1024, content: [{ text: "Side by side." }] }),
    createBlock("c2", "file", { order: 2048, file: png }),
    createBlock("c3", "math", { order: 3072, content: [{ text: "x^2" }] }),
    createBlock("c4", "code", { order: 4096, language: "mermaid", content: [{ text: "flowchart LR\nA-->B" }] }),
  ], { parent_id: "board", row_order: 1, properties: { s: "Doing", c: "Editor" } }),
  card2: page("card2", "Charts <b>", [createBlock("d1", "file", { order: 1024, file: big })], { parent_id: "board", row_order: 2 }),
  trashed: page("trashed", "Old", [], { parent_id: "board", archived: true }),
  doc: page("doc", "Notes", [
    pageLine("l1", "card1", { order: 1024 }),
    pageLine("l2", "board", { order: 2048, expanded: true }),
    pageLine("l3", "elsewhere", { order: 3072 }),
  ]),
  elsewhere: page("elsewhere", "Private", [createBlock("e1", "text", { order: 1024, content: [{ text: "secret words" }] })]),
};
const titleOf = (id: string) => pages[id]?.title ?? "";

/** A worker that keeps what it is sent, and says which files it lacks. */
function fakeWorker() {
  const published = new Map<string, SharedPage>();
  const files = new Map<string, Uint8Array>();
  const calls: string[] = [];
  const transport: ShareTransport = {
    check: async () => { calls.push("check"); },
    publish: async (pageId, id, body) => {
      calls.push(`publish ${pageId}`);
      published.set(id, body);
      return { id, url: `https://x.test/${id}`, missing: body.files.filter((name) => !files.has(`${id}/${name}`)) };
    },
    upload: async (id, name, _type, bytes) => { calls.push(`upload ${name}`); files.set(`${id}/${name}`, bytes); },
    unpublish: async (pageId) => { calls.push(`unpublish ${pageId}`); },
  };
  return { published, files, calls, transport };
}

const draw = async (): Promise<Drawn> => ({
  math: (source) => `<span class="katex">${source}</span>`,
  diagram: (source) => `<svg data-source="${source.length}"></svg>`,
});

const base = {
  pages, titleOf, existing: {} as Record<string, { id: string }>, maxFileBytes: 1000,
  readFile: async (file: StoredFile) => (file.path === big.path ? new Uint8Array(5000) : new Uint8Array([1, 2, 3])),
  draw,
};

describe("publishing a page", () => {
  it("names a file by its bytes, so the same bytes are the same name", async () => {
    const a = await contentName(new Uint8Array([1, 2, 3]), "pic.PNG");
    expect(a).toMatch(/^[0-9a-f]{24}\.png$/);
    expect(await contentName(new Uint8Array([1, 2, 3]), "other.png")).toBe(a);
    expect(await contentName(new Uint8Array([1, 2, 4]), "pic.png")).not.toBe(a);
  });

  it("makes ids of twelve letters and digits", () => {
    const id = newPublicationId();
    expect(id).toMatch(/^[a-z0-9]{12}$/);
    expect(newPublicationId()).not.toBe(id);
  });

  it("finds the equations and the diagrams to draw", () => {
    expect(sourcesIn(Object.values(pages))).toEqual({ math: ["x^2"], diagram: ["flowchart LR\nA-->B"] });
  });

  it("sends the page and what is under it, each page before its rows, and never the trash", async () => {
    const worker = fakeWorker();
    const report = await publishPages({ ...base, rootId: "board", subpages: true, files: true, transport: worker.transport });
    // Siblings by name, as an export orders them.
    expect(report.pages.map((p) => p.pageId)).toEqual(["board", "card2", "card1"]);
    expect(worker.calls[0]).toBe("check");
    const [board, , card1] = report.pages;
    const sentBoard = worker.published.get(board.id)!;
    const sentCard = worker.published.get(card1.id)!;
    // A row's trail leads to the board it is in; the board has none.
    expect(sentCard.parent).toBe(board.id);
    expect(sentBoard.parent).toBeUndefined();
    // The board is its words, then its table: each row linked, its values in cells.
    expect(sentBoard.html).toContain("What we build.");
    expect(sentBoard.html).toContain(`<a href="/${card1.id}">Columns</a>`);
    expect(sentBoard.html).toContain('<span class="tag tag-blue">Doing</span>');
    expect(sentBoard.html).toContain("Charts &lt;b&gt;");
    expect(sentBoard.html).not.toContain("Old");
    // The equation is typeset and the diagram drawn, the picture is the worker's file.
    expect(sentCard.html).toContain('<div class="math"><span class="katex">x^2</span></div>');
    expect(sentCard.html).toContain("<svg data-source=");
    expect(sentCard.html).toMatch(new RegExp(`<img src="/f/${card1.id}/[0-9a-f]{24}\\.png"`));
  });

  it("sends only the files the worker lacks, and leaves out the ones over the limit", async () => {
    const worker = fakeWorker();
    const report = await publishPages({ ...base, rootId: "board", subpages: true, files: true, transport: worker.transport });
    expect(report.filesSent).toBe(1);
    expect(report.filesSkipped).toEqual(["huge.bin"]);
    expect(worker.calls.filter((call) => call.startsWith("upload"))).toHaveLength(1);
    // Published again at the same ids: nothing is sent twice.
    const existing = Object.fromEntries(report.pages.map((p) => [p.pageId, { id: p.id }]));
    const again = await publishPages({ ...base, existing, rootId: "board", subpages: true, files: true, transport: worker.transport });
    expect(again.pages.map((p) => p.id)).toEqual(report.pages.map((p) => p.id));
    expect(again.filesSent).toBe(0);
  });

  it("leaves the files out when asked", async () => {
    const worker = fakeWorker();
    const report = await publishPages({ ...base, rootId: "card1", subpages: false, files: false, transport: worker.transport });
    expect(worker.published.get(report.pages[0].id)!.files).toEqual([]);
    expect(worker.published.get(report.pages[0].id)!.html).not.toContain("<img");
  });

  it("links a page that is published and leaves the name of one that is not", async () => {
    const worker = fakeWorker();
    const report = await publishPages({ ...base, existing: { card1: { id: "cardcardcard" } }, rootId: "doc", subpages: false, files: false, transport: worker.transport });
    const html = worker.published.get(report.pages[0].id)!.html;
    expect(html).toContain('<a href="/cardcardcard">Columns</a>');
    // The private page's name is shown, its words never, and nothing links to it.
    expect(html).toContain('<p class="page-link">Private</p>');
    expect(html).not.toContain("secret words");
    // The database shown in place is its table, its rows as names.
    expect(html).toContain('<table class="db">');
    // The private page, and the row that is not published.
    expect(report.linksAsText).toBe(2);
  });

  it("keeps a page's parent trail when the page above it is published already", async () => {
    const worker = fakeWorker();
    await publishPages({ ...base, existing: { board: { id: "boardboardbd" } }, rootId: "card1", subpages: false, files: false, transport: worker.transport });
    expect([...worker.published.values()][0].parent).toBe("boardboardbd");
  });

  it("stops at the first page the worker refuses, and says which was done", async () => {
    const worker = fakeWorker();
    let sent = 0;
    const transport: ShareTransport = {
      ...worker.transport,
      publish: async (pageId, id, body) => {
        if (sent++ === 1) throw new Error("the worker is out of room");
        return worker.transport.publish(pageId, id, body);
      },
    };
    await expect(publishPages({ ...base, rootId: "board", subpages: true, files: false, transport })).rejects.toThrow("out of room");
    expect(worker.published.size).toBe(1);
  });

  it("refuses a worker that cannot take it before sending anything", async () => {
    const worker = fakeWorker();
    const transport: ShareTransport = { ...worker.transport, check: async () => { throw new Error("the token is wrong"); } };
    await expect(publishPages({ ...base, rootId: "board", subpages: true, files: true, transport })).rejects.toThrow("token");
    expect(worker.published.size).toBe(0);
  });

  it("takes pages off the web, the rows before the board they are in", async () => {
    const worker = fakeWorker();
    const existing = { board: { id: "a" }, card1: { id: "b" }, card2: { id: "c" } };
    const removed = await unpublishPages({ pages, titleOf, rootId: "board", subpages: true, existing, transport: worker.transport });
    expect(removed).toBe(3);
    expect(worker.calls).toEqual(["unpublish card1", "unpublish card2", "unpublish board"]);
    const only = fakeWorker();
    expect(await unpublishPages({ pages, titleOf, rootId: "board", subpages: false, existing, transport: only.transport })).toBe(1);
  });
});
