import { orderedBlocks } from "../../core/block-tree";
import { normalizeEmbed } from "../../core/embeds";
import { isImageType } from "../../core/files";
import { escapeHtml, inlinePlainText, inlineToHtml } from "../../core/inline-content";
import { tableCells, tableGrid, tableToHtml } from "../../core/simple-table";
import type { Block, BlockKind, BlockTree, InlineContent } from "../../core/types";
import { readableTree, type FileRef } from "../editor/adapters";

const LIST_TAG: Partial<Record<BlockKind, "ul" | "ol">> = { bullet: "ul", todo: "ul", numbered: "ol" };

/** A line's words as HTML, its pictures as <img> where the export keeps them. */
function inlineHtml(content: InlineContent, fileRef: FileRef | undefined): string {
  let html = "";
  let words: InlineContent = [];
  const flush = () => { if (words.length) html += inlineToHtml(words); words = []; };
  for (const run of content) {
    if (run.mention?.kind !== "image") { words.push(run); continue; }
    flush();
    const file = run.mention.file;
    const where = fileRef?.(file);
    html += where ? `<img src="${escapeHtml(encodeURI(where))}" alt="${escapeHtml(file.name)}">` : `<span>${escapeHtml(file.name)}</span>`;
  }
  flush();
  return html;
}

function blocksHtml(tree: BlockTree, parentId: string | null, fileRef: FileRef | undefined): string {
  let html = "";
  let open: "ul" | "ol" | null = null;
  for (const block of orderedBlocks(tree, parentId)) {
    const list = LIST_TAG[block.kind] ?? null;
    if (open !== list) {
      if (open) html += `</${open}>`;
      if (list) html += `<${list}>`;
      open = list;
    }
    html += blockHtml(tree, block, fileRef);
  }
  if (open) html += `</${open}>`;
  return html;
}

function blockHtml(tree: BlockTree, block: Block, fileRef: FileRef | undefined): string {
  const inner = inlineHtml(block.content, fileRef);
  const children = blocksHtml(tree, block.id, fileRef);
  switch (block.kind) {
    case "heading1": return `<h1>${inner}</h1>${children}`;
    case "heading2": return `<h2>${inner}</h2>${children}`;
    case "heading3": return `<h3>${inner}</h3>${children}`;
    case "todo": return `<li class="todo"><input type="checkbox" disabled${block.checked ? " checked" : ""}> ${inner}${children}</li>`;
    case "bullet":
    case "numbered": return `<li>${inner}${children}</li>`;
    // A toggle's lines are written open: outside Notes nothing is folded away.
    case "toggle": return `<details open><summary>${inner}</summary>${children}</details>`;
    case "quote": return `<blockquote>${inner}</blockquote>${children}`;
    case "callout": return `<div class="callout"><span class="callout-icon">${escapeHtml(block.icon || "💡")}</span><div>${inner}${children}</div></div>`;
    case "code": return `<pre><code>${escapeHtml(inlinePlainText(block.content))}</code></pre>`;
    case "divider": return "<hr>";
    case "table": {
      const grid = tableGrid(block.table);
      return tableToHtml(tableCells(grid), grid.header_row);
    }
    // A block is its paragraphs; outside Notes it has no mark of its own.
    case "page": return children;
    case "file": {
      const file = block.file;
      if (!file) return "";
      const where = fileRef?.(file);
      if (where && isImageType(file.type)) {
        return `<figure><img src="${escapeHtml(encodeURI(where))}" alt="${escapeHtml(file.name)}">${inner ? `<figcaption>${inner}</figcaption>` : ""}</figure>`;
      }
      const link = where ? `<a href="${escapeHtml(encodeURI(where))}">${escapeHtml(file.name)}</a>` : escapeHtml(file.name);
      return `<p>${link}${inner ? ` — ${inner}` : ""}</p>`;
    }
    case "embed": {
      const address = normalizeEmbed(block.embed)?.url;
      return address ? `<p><a href="${escapeHtml(address)}">${escapeHtml(address)}</a></p>` : "";
    }
    default: return `<p>${inner}</p>${children}`;
  }
}

const STYLE = `
:root { color-scheme: light dark; }
body { max-width: 760px; margin: 48px auto; padding: 0 20px; font: 16px/1.65 -apple-system, BlinkMacSystemFont, "Segoe UI", "PingFang SC", "Microsoft YaHei", sans-serif; color: #37352f; background: #fff; }
h1 { font-size: 2.1em; line-height: 1.2; margin: 0 0 .6em; } h2 { margin: 1.4em 0 .4em; } h3 { margin: 1.2em 0 .3em; }
p { margin: .35em 0; } img { max-width: 100%; height: auto; border-radius: 6px; } figure { margin: 1em 0; } figcaption { color: #787774; font-size: .9em; }
blockquote { margin: .6em 0; padding-left: 1em; border-left: 3px solid currentColor; } hr { border: 0; border-top: 1px solid rgba(55,53,47,.16); margin: 1.4em 0; }
code { font: .9em ui-monospace, SFMono-Regular, Menlo, monospace; background: rgba(135,131,120,.15); padding: .1em .35em; border-radius: 4px; }
pre { background: rgba(135,131,120,.12); padding: 14px 16px; border-radius: 8px; overflow: auto; } pre code { background: none; padding: 0; }
li.todo { list-style: none; margin-left: -1.2em; } .callout { display: flex; gap: 10px; background: rgba(135,131,120,.1); padding: 14px 16px; border-radius: 8px; margin: .8em 0; }
table { border-collapse: collapse; margin: 1em 0; } td, th { border: 1px solid rgba(55,53,47,.16); padding: 6px 10px; text-align: left; }
details > summary { cursor: pointer; } a { color: #2383e2; }
@media (prefers-color-scheme: dark) { body { color: rgba(255,255,255,.9); background: #191919; } figcaption { color: #9b9a97; } hr, td, th { border-color: rgba(255,255,255,.14); } }
`;

/** A page as a whole HTML document: its name, its lines, its pictures where the
 *  export keeps them. */
export function pageToHtml(title: string, tree: BlockTree, titleOf: (pageId: string) => string, fileRef: FileRef | undefined, lang: string): string {
  const body = blocksHtml(readableTree(tree, titleOf, Boolean(fileRef)), null, fileRef);
  return `<!doctype html>\n<html lang="${escapeHtml(lang)}">\n<head>\n<meta charset="utf-8">\n<meta name="viewport" content="width=device-width, initial-scale=1">\n<title>${escapeHtml(title)}</title>\n<style>${STYLE}</style>\n</head>\n<body>\n<h1>${escapeHtml(title)}</h1>\n${body}\n</body>\n</html>\n`;
}
