import type { NotePage, StoredFile } from "../../core/types";
import { fileKey, filesOf, planPages } from "../export/build";
import { emojiOf, publishedBody } from "./publish-body";

/** What the worker's page takes: see internal/share (Page). */
export interface SharedPage {
  title: string;
  html: string;
  icon?: string;
  parent?: string;
  position?: number;
  files: string[];
}

/** The calls a publish makes. In the app they go through Go, which holds the token. */
export interface ShareTransport {
  /** Throws, in words, when the worker cannot take what is sent: a wrong address or token, one that is out of date. */
  check(): Promise<void>;
  /** Publishes `pageId` as `id`: the same id updates it. Answers where it is and which of its files the worker lacks. */
  publish(pageId: string, id: string, page: SharedPage): Promise<{ id: string; url: string; missing: string[] }>;
  upload(id: string, name: string, type: string, bytes: Uint8Array): Promise<void>;
  unpublish(pageId: string): Promise<void>;
}

/** Equations and diagrams, drawn before the pages are made: drawing is not instant. */
export interface Drawn {
  math(source: string): string | null;
  diagram(source: string): string | null;
}

export interface PublishInput {
  pages: Record<string, NotePage>;
  rootId: string;
  /** The pages under the page too (a database's rows among them). */
  subpages: boolean;
  /** The pictures and files the pages hold. */
  files: boolean;
  titleOf(pageId: string): string;
  /** Where pages are published already, by page id. */
  existing: Record<string, { id: string }>;
  /** The largest file sent, in bytes. */
  maxFileBytes: number;
  /** A file's bytes, or null when they cannot be read. */
  readFile(file: StoredFile): Promise<Uint8Array | null>;
  /** Draws what the pages hold that the worker cannot: every source in `sources`. */
  draw(sources: { math: string[]; diagram: string[] }): Promise<Drawn>;
  transport: ShareTransport;
  onProgress?(done: number, total: number, title: string): void;
}

export interface PublishReport {
  /** The pages now on the web, in the order they were sent. */
  pages: Array<{ pageId: string; id: string; title: string; url: string }>;
  filesSent: number;
  /** Names of files left out: unreadable, or over the limit. */
  filesSkipped: string[];
  /** Lines that stand for a page that is not published, left as its name. */
  linksAsText: number;
}

const ID_ALPHABET = "abcdefghijklmnopqrstuvwxyz0123456789";

/** Twelve letters and digits: hard to guess, short to share. */
export function newPublicationId(random: (n: number) => Uint8Array = (n) => crypto.getRandomValues(new Uint8Array(n))): string {
  return Array.from(random(12), (byte) => ID_ALPHABET[byte % ID_ALPHABET.length]).join("");
}

function extensionOf(name: string): string {
  const match = /\.([A-Za-z0-9]{1,10})$/.exec(name);
  return match ? `.${match[1].toLowerCase()}` : "";
}

/** A file's name on the worker: a hash of its bytes and its extension. A file that did not change is never sent twice. */
export async function contentName(bytes: Uint8Array, fileName: string): Promise<string> {
  const digest = new Uint8Array(await crypto.subtle.digest("SHA-256", bytes as BufferSource));
  const hex = Array.from(digest.slice(0, 12), (byte) => byte.toString(16).padStart(2, "0")).join("");
  return `${hex}${extensionOf(fileName)}`;
}

/** What drawing needs to find in the pages: the equations and the diagrams. */
export function sourcesIn(pages: NotePage[]): { math: string[]; diagram: string[] } {
  const math = new Set<string>();
  const diagram = new Set<string>();
  for (const page of pages) {
    for (const block of Object.values(page.block_tree.by_id)) {
      const text = block.content.map((run) => run.text).join("");
      if (block.kind === "math" && text.trim()) math.add(text);
      if (block.kind === "code" && block.language === "mermaid" && text.trim()) diagram.add(text);
    }
  }
  return { math: [...math], diagram: [...diagram] };
}

/** Publishes a page, and under it its sub-pages when asked: each page first, then what is under it. */
export async function publishPages(input: PublishInput): Promise<PublishReport> {
  const { pages, transport } = input;
  const plan = planPages(pages, input.rootId, input.subpages, input.titleOf);
  if (!plan.length) throw new Error("There is nothing to publish.");
  await transport.check();

  const ids = new Map<string, string>();
  for (const pageId of plan) ids.set(pageId, input.existing[pageId]?.id ?? newPublicationId());
  // A page is reached at "/<id>"; the worker sends it on to its address with the title in it.
  const idOf = (pageId: string): string | null => ids.get(pageId) ?? input.existing[pageId]?.id ?? null;
  const addressOf = (pageId: string): string | null => {
    const id = idOf(pageId);
    return id ? `/${id}` : null;
  };

  const drawn = await input.draw(sourcesIn(plan.map((pageId) => pages[pageId])));
  const report: PublishReport = { pages: [], filesSent: 0, filesSkipped: [], linksAsText: 0 };

  for (const [index, pageId] of plan.entries()) {
    const page = pages[pageId];
    const id = ids.get(pageId)!;
    const title = input.titleOf(pageId) || "Untitled";
    input.onProgress?.(index, plan.length, title);

    // The files this page holds, read once and named by their bytes.
    const names = new Map<string, string>();
    const bytesByName = new Map<string, { bytes: Uint8Array; type: string }>();
    if (input.files) {
      for (const file of filesOf(page.block_tree)) {
        const bytes = await input.readFile(file);
        if (!bytes || bytes.byteLength > input.maxFileBytes) {
          report.filesSkipped.push(file.name);
          continue;
        }
        const name = await contentName(bytes, file.name);
        names.set(fileKey(file), name);
        bytesByName.set(name, { bytes, type: file.type || "application/octet-stream" });
      }
    }

    const body = publishedBody(page, {
      pages,
      titleOf: input.titleOf,
      addressOf,
      fileRef: input.files ? (file) => { const name = names.get(fileKey(file)); return name ? `/f/${id}/${name}` : null; } : undefined,
      hooks: drawn,
    });
    report.linksAsText += body.linksAsText;

    const parent = page.parent_id ? idOf(page.parent_id) : null;
    const reply = await transport.publish(pageId, id, {
      title,
      html: body.html,
      ...(emojiOf(page.icon) ? { icon: emojiOf(page.icon) } : {}),
      ...(parent ? { parent } : {}),
      position: index,
      files: [...bytesByName.keys()],
    });
    for (const name of reply.missing) {
      const file = bytesByName.get(name);
      if (!file) continue;
      await transport.upload(id, name, file.type, file.bytes);
      report.filesSent += 1;
    }
    report.pages.push({ pageId, id, title, url: reply.url });
  }
  input.onProgress?.(plan.length, plan.length, "");
  return report;
}

/** Takes a page off the web, and the pages under it when asked. Returns how many were taken off. */
export async function unpublishPages(input: Pick<PublishInput, "pages" | "rootId" | "subpages" | "titleOf" | "existing" | "transport">): Promise<number> {
  const plan = planPages(input.pages, input.rootId, input.subpages, input.titleOf);
  let removed = 0;
  // Last pages first: a page is never left with a trail to one that is gone.
  for (const pageId of plan.toReversed()) {
    if (!input.existing[pageId]) continue;
    await input.transport.unpublish(pageId);
    removed += 1;
  }
  return removed;
}
