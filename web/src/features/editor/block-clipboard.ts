/* Blocks on the clipboard.

   Copying blocks puts three things on the clipboard, the way Notion does: the
   blocks themselves, for pasting back into Notes exactly — kinds, nesting,
   marks, checkboxes, the pages behind page blocks; HTML, for a rich-text
   editor elsewhere; and Markdown, for plain text. The blocks ride under their
   own type and again inside the HTML, because some clipboards (another
   browser, an app shell) keep only the standard types.

   Anything read back is untrusted — it may have been copied from any page on
   the web — so it goes through the same normalisation as stored content:
   unknown kinds become text and only safe links survive. */

import { normalizeBlockTree, orderedBlocks } from "../../core/block-tree";
import { normalizeEmbed } from "../../core/embeds";
import { escapeHtml, inlinePlainText, inlineToHtml } from "../../core/inline-content";
import { tableCells, tableGrid, tableToHtml } from "../../core/simple-table";
import type { Block, BlockKind, BlockTree, InlineMarks } from "../../core/types";
import { blockTreeToMarkdown, markdownToFragment, readableTree } from "./adapters";
import { BLOCK_DEFINITIONS } from "./block-registry";
import type { HtmlFragment } from "./html-paste";

export const BLOCKS_MIME = "application/x-notes-blocks";

export interface CopiedBlocks {
  /** The blocks as a body of their own: top-level blocks are its roots. */
  tree: BlockTree;
  /** Cut rather than copied. Pasting a cut page block moves the page. */
  cut: boolean;
  /** The page the blocks came from. */
  source: string;
}

interface Payload {
  notes: 1;
  cut: boolean;
  source: string;
  blocks: Block[];
}

const KINDS = new Set<BlockKind>(BLOCK_DEFINITIONS.map((definition) => definition.kind));
const LIST_TAG: Partial<Record<BlockKind, "ul" | "ol">> = { bullet: "ul", todo: "ul", numbered: "ol" };

function blocksHtml(tree: BlockTree, parentId: string | null): string {
  let html = "";
  let open: "ul" | "ol" | null = null;
  for (const block of orderedBlocks(tree, parentId)) {
    const list = LIST_TAG[block.kind] ?? null;
    if (open !== list) {
      if (open) html += `</${open}>`;
      if (list) html += `<${list}>`;
      open = list;
    }
    const inner = inlineToHtml(block.content);
    const children = blocksHtml(tree, block.id);
    switch (block.kind) {
      case "heading1": html += `<h1>${inner}</h1>${children}`; break;
      case "heading2": html += `<h2>${inner}</h2>${children}`; break;
      case "heading3": html += `<h3>${inner}</h3>${children}`; break;
      case "todo": html += `<li>${block.checked ? "☑" : "☐"} ${inner}${children}</li>`; break;
      case "bullet":
      case "toggle":
      case "numbered": html += `<li>${inner}${children}</li>`; break;
      case "quote": html += `<blockquote>${inner}</blockquote>${children}`; break;
      case "code": html += `<pre><code>${escapeHtml(inlinePlainText(block.content))}</code></pre>`; break;
      // The source, fenced, without KaTeX: a paste elsewhere still shows the equation.
      case "math": html += `<pre class="math">$$${escapeHtml(inlinePlainText(block.content))}$$</pre>`; break;
      case "divider": html += "<hr>"; break;
      case "table": {
        const grid = tableGrid(block.table);
        html += tableToHtml(tableCells(grid), grid.header_row);
        break;
      }
      // A block is its paragraphs; outside Notes it has no mark of its own.
      case "page": html += children; break;
      // Pasted elsewhere, columns are their lines one after another.
      case "columns":
      case "column": html += children; break;
      case "file": html += `<p>${escapeHtml(block.file?.name ?? "")}${inner ? ` — ${inner}` : ""}</p>`; break;
      case "embed": {
        const address = normalizeEmbed(block.embed)?.url;
        if (address) html += `<p><a href="${escapeHtml(address)}">${escapeHtml(address)}</a></p>`;
        break;
      }
      default: html += `<p>${inner}</p>${children}`;
    }
  }
  if (open) html += `</${open}>`;
  return html;
}

export function writeBlocks(
  data: DataTransfer,
  copied: CopiedBlocks,
  titleOf: (pageId: string) => string,
): void {
  const payload: Payload = { notes: 1, cut: copied.cut, source: copied.source, blocks: Object.values(copied.tree.by_id) };
  const json = JSON.stringify(payload);
  data.setData(BLOCKS_MIME, json);
  data.setData("text/html", `<meta charset="utf-8"><div data-notes-blocks="${escapeHtml(json)}">${blocksHtml(readableTree(copied.tree, titleOf), null)}</div>`);
  data.setData("text/plain", blockTreeToMarkdown(copied.tree, titleOf));
}

/* The blocks inside copied HTML: a browser re-serialises it (wrapping it,
   re-quoting it), and reading it as a document undoes whatever it did. */
function payloadFromHtml(html: string): string {
  if (!html.includes("data-notes-blocks")) return "";
  const holder = new DOMParser().parseFromString(html, "text/html").querySelector("[data-notes-blocks]");
  return holder?.getAttribute("data-notes-blocks") ?? "";
}

/** Blocks copied from Notes, or null for anything else. */
export function readBlocks(data: Pick<DataTransfer, "getData">): CopiedBlocks | null {
  const raw = data.getData(BLOCKS_MIME) || payloadFromHtml(data.getData("text/html"));
  if (!raw) return null;
  let payload: Partial<Payload>;
  try {
    payload = JSON.parse(raw) as Partial<Payload>;
  } catch {
    return null;
  }
  if (payload?.notes !== 1 || !Array.isArray(payload.blocks)) return null;
  const candidates = Object.fromEntries(payload.blocks
    .filter((block): block is Block => Boolean(block) && typeof block === "object" && typeof block.id === "string")
    .map((block) => [block.id, { ...block, kind: KINDS.has(block.kind) ? block.kind : "text" }]));
  const tree = normalizeBlockTree({ by_id: candidates });
  // A block whose parent did not come along is a root of what was copied.
  for (const block of Object.values(tree.by_id)) {
    if (block.parent_id && !tree.by_id[block.parent_id]) block.parent_id = null;
  }
  if (!Object.keys(tree.by_id).length) return null;
  return { tree, cut: payload.cut === true, source: typeof payload.source === "string" ? payload.source : "" };
}

/** Text of more than one line (or a single-line Markdown image) as blocks
 *  and the pictures it names. A single line of plain words is not blocks: it
 *  is pasted among the words like anything typed. */
export function textToFragment(text: string, idFactory: () => string): HtmlFragment | null {
  const trimmed = text.trim();
  if (!trimmed) return null;
  const fragment = markdownToFragment(text, idFactory);
  if (!Object.keys(fragment.tree.by_id).length) return null;
  if (!/\r?\n/.test(trimmed) && fragment.images.length === 0) return null;
  return fragment;
}

export function textToBlocks(text: string, idFactory: () => string): BlockTree | null {
  return textToFragment(text, idFactory)?.tree ?? null;
}

function isBareAutolink(text: string, marks?: InlineMarks): boolean {
  if (!marks) return true;
  const keys = Object.keys(marks);
  if (keys.length === 0) return true;
  if (keys.length !== 1 || !marks.link) return false;
  return marks.link === text || marks.link.replace(/\/$/, "") === text.replace(/\/$/, "");
}

function isPlainTextFragment(fragment: HtmlFragment): boolean {
  if (fragment.images.length > 0) return false;
  return Object.values(fragment.tree.by_id).every(
    (block) => block.kind === "text" && block.content.every((run) => isBareAutolink(run.text, run.marks)),
  );
}

/** Prefers Markdown over HTML when the clipboard HTML has no rich elements of
 *  its own (e.g. plain paragraphs/divs or auto-linked URLs from a webpage or
 *  chat app) and the plain text carries Markdown structure or images. */
export function chooseWebOrMarkdownFragment(html: HtmlFragment | null, markdown: HtmlFragment | null): HtmlFragment | null {
  if (!html) return markdown;
  if (markdown && isPlainTextFragment(html) && !isPlainTextFragment(markdown)) return markdown;
  return html;
}
