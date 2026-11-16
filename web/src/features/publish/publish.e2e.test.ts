// The publisher against a running worker (`wrangler dev --local` in share-worker/).
// Skipped unless SHARE_E2E_URL and SHARE_E2E_TOKEN are set:
//   SHARE_E2E_URL=http://127.0.0.1:8795 SHARE_E2E_TOKEN=… npx vitest run publish.e2e
import { describe, expect, it } from "vitest";
import { createBlock, pageLine } from "../../core/block-tree";
import type { Block, NotePage, StoredFile } from "../../core/types";
import { publishPages, unpublishPages, type ShareTransport } from "./publisher";

// The web app's types have no Node; this runs under vitest, which does.
const env = (globalThis as unknown as { process: { env: Record<string, string | undefined> } }).process.env;
const URL_ = env.SHARE_E2E_URL;
const TOKEN = env.SHARE_E2E_TOKEN;

function page(id: string, title: string, blocks: Block[], extra: Partial<NotePage> = {}): NotePage {
  return {
    id, kind: "document", icon: "", title, parent_id: null,
    block_tree: { by_id: Object.fromEntries(blocks.map((block) => [block.id, block])) },
    created_at: "2026", updated_at: "2026", created_by: "a", updated_by: "a", ...extra,
  } as NotePage;
}

/** What Go does for the app, done here with fetch: the same calls, the same worker. */
function fetchTransport(base: string, token: string, keep: Map<string, string>): ShareTransport {
  const call = async (path: string, init: RequestInit & { headers?: Record<string, string> } = {}) => {
    const res = await fetch(`${base}${path}`, { ...init, headers: { authorization: `Bearer ${token}`, ...init.headers } });
    if (!res.ok && res.status !== 404) throw new Error(`${init.method ?? "GET"} ${path}: ${res.status} ${await res.text()}`);
    return res;
  };
  return {
    check: async () => {
      const info = await (await call("/api/v1/info")).json() as { app: string; migrations: { pending: number } };
      if (info.app !== "notes-share" || info.migrations.pending) throw new Error("not a ready worker");
    },
    publish: async (pageId, id, body) => {
      const reply = await (await call(`/api/v1/pages/${id}`, { method: "PUT", headers: { "content-type": "application/json" }, body: JSON.stringify(body) })).json() as { id: string; url: string; missing: string[] };
      keep.set(pageId, id);
      return reply;
    },
    upload: async (id, name, type, bytes) => { await call(`/api/v1/pages/${id}/files/${name}`, { method: "PUT", headers: { "content-type": type }, body: bytes as BodyInit }); },
    unpublish: async (pageId) => { await call(`/api/v1/pages/${keep.get(pageId)}`, { method: "DELETE" }); keep.delete(pageId); },
  };
}

const png: StoredFile = { path: "files/p/f1/pic.png", owner: "me", name: "pic.png", size: 8, type: "image/png" };
const PNG_BYTES = new Uint8Array([137, 80, 78, 71, 13, 10, 26, 10, 0, 0, 0, 0]);

const pages: Record<string, NotePage> = {
  board: page("board", "开发看板", [createBlock("b1", "text", { order: 1024, content: [{ text: "我们在做什么" }] })], {
    kind: "database",
    parent_id: "home",
    database: {
      title_name: "任务",
      properties: [
        { id: "s", name: "状态", type: "status", status_options: [{ id: "o1", name: "进行中", color: "blue", group: "progress" }] },
        { id: "c", name: "分类", type: "select", options: ["编辑器"] },
      ],
      views: [],
    } as unknown as NotePage["database"],
  }),
  card: page("card", "分栏布局", [
    createBlock("c1", "heading2", { order: 1024, content: [{ text: "做了什么" }] }),
    createBlock("c2", "file", { order: 2048, file: png }),
    createBlock("c3", "math", { order: 3072, content: [{ text: "E=mc^2" }] }),
    createBlock("c4", "text", { order: 4096, content: [{ text: "<script>alert(1)</script> & done" }] }),
  ], { parent_id: "board", row_order: 1, properties: { s: "进行中", c: "编辑器" }, icon: "🧱" }),
  home: page("home", "Home", [pageLine("l1", "board", { order: 1024, expanded: true }), pageLine("l2", "secret", { order: 2048 })]),
  secret: page("secret", "Private", [createBlock("s1", "text", { order: 1024, content: [{ text: "never published" }] })]),
};
const titleOf = (id: string) => pages[id]?.title ?? "";

describe.skipIf(!URL_ || !TOKEN)("publishing to a running worker", () => {
  it("publishes a board with its rows, serves them, links them and takes them off", async () => {
    const keep = new Map<string, string>();
    const transport = fetchTransport(URL_!, TOKEN!, keep);
    const input = {
      pages, titleOf, existing: {}, maxFileBytes: 1 << 20,
      readFile: async () => PNG_BYTES,
      draw: async () => ({ math: (s: string) => `<span class="katex">${s}</span>`, diagram: () => null }),
      transport,
    };
    const report = await publishPages({ ...input, rootId: "home", subpages: true, files: true });
    // Home, the board under it (shown in place on Home, and a page of its own), its row; not the private page.
    expect(report.pages.map((p) => p.pageId)).toEqual(["home", "board", "card"]);
    expect(report.linksAsText).toBe(1);
    const [home, board, card] = report.pages;

    // Home links to the board's address, and the table in it links the row.
    const homeHtml = await (await fetch(home.url)).text();
    expect(homeHtml).toContain("<title>Home</title>");
    expect(homeHtml).toContain('<p class="page-link">Private</p>');
    expect(homeHtml).not.toContain("never published");
    expect(homeHtml).toContain(`<a href="/${card.id}">分栏布局</a>`);

    // The board's own page: its words, its table, the status as a tag.
    const boardHtml = await (await fetch(board.url)).text();
    expect(boardHtml).toContain("我们在做什么");
    expect(boardHtml).toContain('<span class="tag tag-blue">进行中</span>');
    expect(boardHtml).toContain("<th>任务</th>");

    // The row: its icon, its trail up through the board to Home, the equation, the picture from R2, no live script.
    const cardRes = await fetch(card.url);
    const cardHtml = await cardRes.text();
    expect(cardHtml).toContain("🧱");
    expect(cardHtml).toMatch(/<nav class="crumbs"[^>]*><a href="\/[^"]+">Home<\/a><span>\/<\/span><a href="\/[^"]+">开发看板<\/a>/);
    expect(cardHtml).toContain('<span class="katex">E=mc^2</span>');
    expect(cardHtml).toContain("&lt;script&gt;alert(1)&lt;/script&gt; &amp; done");
    expect(cardHtml).not.toContain("<script>alert");
    expect(cardRes.headers.get("content-security-policy")).toContain("default-src 'none'");
    const src = /<img src="(\/f\/[^"]+)"/.exec(cardHtml)![1];
    const image = await fetch(new URL(src, URL_!));
    expect(image.status).toBe(200);
    expect(new Uint8Array(await image.arrayBuffer())).toEqual(PNG_BYTES);

    // Publishing again is an update at the same addresses, and sends no file twice.
    const existing = Object.fromEntries(report.pages.map((p) => [p.pageId, { id: p.id }]));
    const again = await publishPages({ ...input, existing, rootId: "home", subpages: true, files: true });
    expect(again.pages.map((p) => p.url)).toEqual(report.pages.map((p) => p.url));
    expect(again.filesSent).toBe(0);

    // Off the web: the pages and the picture are gone.
    expect(await unpublishPages({ pages, titleOf, rootId: "home", subpages: true, existing, transport })).toBe(3);
    expect((await fetch(home.url)).status).toBe(404);
    expect((await fetch(new URL(src, URL_!))).status).toBe(404);
  });
});
