import type { BlockTree, NotePage, StoredFile } from "../../core/types";
import { blockTreeToMarkdown, type FileRef } from "../editor/adapters";
import { pageToHtml } from "./html";
import { zipStore, type ZipEntry } from "./zip";

export type ExportFormat = "markdown" | "html";

export interface ExportOptions {
  format: ExportFormat;
  /** The pages under the page too, each in a folder of its parent's name. */
  subpages: boolean;
  /** The pictures and files the pages hold, kept in `assets/` and linked from them. */
  files: boolean;
}

export interface ExportInput {
  pages: Record<string, NotePage>;
  rootId: string;
  options: ExportOptions;
  titleOf(pageId: string): string;
  /** A file's bytes, or null when they cannot be read. */
  readFile(file: StoredFile): Promise<Uint8Array | null>;
  lang?: string;
}

export interface ExportResult {
  name: string;
  mime: string;
  bytes: Uint8Array;
  pages: number;
  files: number;
}

/** What an export would hold, without reading a byte: the dialog's summary. */
export interface ExportPlan {
  pageIds: string[];
  files: StoredFile[];
}

const EXTENSION: Record<ExportFormat, string> = { markdown: "md", html: "html" };

/** A name that is safe as one file or folder name on any system. */
export function safeName(title: string, fallback = "Untitled"): string {
  const name = title
    .replace(/[\\/:*?"<>|\u0000-\u001f]/g, " ")
    .replace(/\s+/g, " ")
    .trim()
    .replace(/^\.+/, "")
    .replace(/[. ]+$/, "")
    .slice(0, 80)
    .trim();
  return name || fallback;
}

/** The page, and under it its sub-pages (and a database's rows) when asked, in
 *  the order a person reads them: each page before what is under it, siblings by
 *  name. Pages in the trash are left out. */
export function planPages(pages: Record<string, NotePage>, rootId: string, subpages: boolean, titleOf: (id: string) => string): string[] {
  if (!pages[rootId]) return [];
  const children = new Map<string, string[]>();
  for (const page of Object.values(pages)) {
    if (!page.parent_id || page.archived) continue;
    const siblings = children.get(page.parent_id) ?? [];
    siblings.push(page.id);
    children.set(page.parent_id, siblings);
  }
  const order: string[] = [];
  const seen = new Set<string>();
  const visit = (id: string) => {
    if (seen.has(id)) return;
    seen.add(id);
    order.push(id);
    if (!subpages) return;
    for (const child of (children.get(id) ?? []).toSorted((a, b) => titleOf(a).localeCompare(titleOf(b)))) visit(child);
  };
  visit(rootId);
  return order;
}

/** Every file a body holds, once each: a file's line and a picture in the words. */
export function filesOf(tree: BlockTree): StoredFile[] {
  const found = new Map<string, StoredFile>();
  const add = (file: StoredFile | undefined) => { if (file?.path) found.set(fileKey(file), file); };
  for (const block of Object.values(tree.by_id)) {
    add(block.file);
    for (const run of block.content) if (run.mention?.kind === "image") add(run.mention.file);
  }
  return [...found.values()];
}

export function fileKey(file: StoredFile): string {
  return `${file.space ?? ""}|${file.owner}|${file.path}`;
}

export function planExport(pages: Record<string, NotePage>, rootId: string, options: Pick<ExportOptions, "subpages">, titleOf: (id: string) => string): ExportPlan {
  const pageIds = planPages(pages, rootId, options.subpages, titleOf);
  const files = new Map<string, StoredFile>();
  for (const id of pageIds) for (const file of filesOf(pages[id].block_tree)) files.set(fileKey(file), file);
  return { pageIds, files: [...files.values()] };
}

/** `name`, or `name 2`, `name 3`… when `taken` has it already (case-blind, as a disk). */
function unique(taken: Set<string>, name: string, extension = ""): string {
  let candidate = name;
  for (let n = 2; taken.has((candidate + extension).toLowerCase()); n += 1) candidate = `${name} ${n}`;
  taken.add((candidate + extension).toLowerCase());
  return candidate + extension;
}

export async function buildExport(input: ExportInput): Promise<ExportResult> {
  const { pages, rootId, options, titleOf } = input;
  const extension = EXTENSION[options.format];
  const plan = planExport(pages, rootId, options, titleOf);
  const encoder = new TextEncoder();

  // Where each file goes in the archive: assets/<n>-<name>, read once.
  const assets = new Map<string, string>();
  const entries: ZipEntry[] = [];
  let kept = 0;
  if (options.files) {
    const taken = new Set<string>();
    let n = 0;
    for (const file of plan.files) {
      const bytes = await input.readFile(file);
      if (!bytes) continue;
      n += 1;
      const base = safeName(file.name.replace(/\.[^.]*$/, ""), "file");
      const ext = /\.[A-Za-z0-9]{1,8}$/.exec(file.name)?.[0] ?? "";
      const name = unique(taken, `${n}-${base}`, ext);
      assets.set(fileKey(file), `assets/${name}`);
      entries.push({ path: `assets/${name}`, data: bytes });
      kept += 1;
    }
  }

  // Where each page goes: its own folder holds its sub-pages, as Notion's export does.
  const where = new Map<string, { folder: string; file: string }>();
  const rootName = safeName(titleOf(rootId));
  const foldersTaken = new Map<string, Set<string>>();
  const parentFolder = (id: string): string => {
    const parent = pages[id].parent_id;
    return parent && where.has(parent) ? where.get(parent)!.folder : "";
  };
  for (const id of plan.pageIds) {
    const folderOfPage = id === rootId ? "" : parentFolder(id);
    const taken = foldersTaken.get(folderOfPage) ?? new Set<string>();
    foldersTaken.set(folderOfPage, taken);
    const file = unique(taken, id === rootId ? rootName : safeName(titleOf(id)), `.${extension}`);
    const stem = file.slice(0, -(extension.length + 1));
    // A page's own folder is its name, inside the folder it lives in.
    where.set(id, { folder: folderOfPage ? `${folderOfPage}/${stem}` : stem, file: folderOfPage ? `${folderOfPage}/${file}` : file });
  }

  // Pages in reading order, then the assets.
  const pageEntries: ZipEntry[] = [];
  for (const id of plan.pageIds) {
    const page = pages[id];
    const path = where.get(id)!.file;
    const prefix = "../".repeat(path.split("/").length - 1);
    const fileRef: FileRef | undefined = options.files
      ? (file) => { const asset = assets.get(fileKey(file)); return asset ? prefix + asset : null; }
      : undefined;
    const title = titleOf(id) || "Untitled";
    const text = options.format === "markdown"
      ? `# ${title}\n\n${blockTreeToMarkdown(page.block_tree, titleOf, fileRef)}\n`
      : pageToHtml(title, page.block_tree, titleOf, fileRef, input.lang ?? "en");
    pageEntries.push({ path, data: encoder.encode(text) });
  }

  const ordered = pageEntries;
  const assetEntries = entries;
  const mime = options.format === "markdown" ? "text/markdown;charset=utf-8" : "text/html;charset=utf-8";
  if (ordered.length === 1 && assetEntries.length === 0) {
    return { name: `${rootName}.${extension}`, mime, bytes: ordered[0].data, pages: 1, files: 0 };
  }
  return {
    name: `${rootName}.zip`,
    mime: "application/zip",
    bytes: zipStore([...ordered, ...assetEntries]),
    pages: ordered.length,
    files: kept,
  };
}
