/* What ⌘K finds. Pure: pages in, hits out, so that it can be tested and so
   that agents of the window can find pages the same way. */

import { orderedBlocks, pageName } from "../../core/block-tree";
import { inlinePlainText, inlineReadableText } from "../../core/inline-content";
import { pageAncestry } from "../../core/page-tree";
import { tableCells, tableGrid } from "../../core/simple-table";
import type { NotePage } from "../../core/types";
import { readableDescriber } from "../editor/mentions";

/** The longest list of hits. */
export const MAX_HITS = 30;
/** The width of the line shown under a hit, in characters. */
const EXCERPT = 110;

export interface Excerpt {
  text: string;
  /** Where the words searched for are in `text`, as [start, end) pairs. */
  marks: Array<[number, number]>;
}

export interface Hit {
  page: NotePage;
  crumb: string;
  /** The first line that holds the words, cut around them. */
  excerpt?: Excerpt;
  /** The block of that line: the page opens scrolled to it. */
  blockId?: string;
  /** How many lines of the page hold the words (more than one is said). */
  lines: number;
}

/** The words of a query: lower-cased, one of each. */
export function queryWords(query: string): string[] {
  return [...new Set(query.toLowerCase().split(/\s+/).filter(Boolean))];
}

/** Where `words` occur in `text`, merged and in order. */
export function markRanges(text: string, words: readonly string[]): Array<[number, number]> {
  const lower = text.toLowerCase();
  const found: Array<[number, number]> = [];
  for (const word of words) {
    for (let at = lower.indexOf(word); at >= 0; at = lower.indexOf(word, at + word.length)) found.push([at, at + word.length]);
  }
  found.sort((left, right) => left[0] - right[0] || right[1] - left[1]);
  const merged: Array<[number, number]> = [];
  for (const range of found) {
    const last = merged.at(-1);
    if (last && range[0] <= last[1]) last[1] = Math.max(last[1], range[1]);
    else merged.push([range[0], range[1]]);
  }
  return merged;
}

/** `text` cut to about `EXCERPT` characters around its first match, with the
 *  words marked. A line that fits is shown whole. */
export function excerptOf(text: string, words: readonly string[]): Excerpt {
  const flat = text.replace(/\s+/g, " ").trim();
  const first = markRanges(flat, words)[0]?.[0] ?? 0;
  if (flat.length <= EXCERPT) return { text: flat, marks: markRanges(flat, words) };
  const start = Math.max(0, Math.min(first - Math.floor(EXCERPT / 4), flat.length - EXCERPT));
  const end = Math.min(flat.length, start + EXCERPT);
  const cut = `${start > 0 ? "…" : ""}${flat.slice(start, end)}${end < flat.length ? "…" : ""}`;
  return { text: cut, marks: markRanges(cut, words) };
}

/** What ⌘K finds for `query`: names that start with it, then names that hold
 *  all its words, then pages with a line that holds them all (that line, cut
 *  around the words), then pages whose lines hold them between them — at most
 *  `MAX_HITS`, each with the pages above it. Without words, the eight pages
 *  changed last. Pages in the trash are never found. */
export function quickFind(allPages: Record<string, NotePage>, query: string): Hit[] {
  const words = queryWords(query);
  const pages = Object.values(allPages).filter((page) => !page.archived);
  const describe = (page: NotePage): string => pageAncestry(allPages, page.id)
    .slice(0, -1)
    .map((item) => pageName(item))
    .join(" / ");

  if (!words.length) {
    return pages
      .toSorted((left, right) => right.updated_at.localeCompare(left.updated_at))
      .slice(0, 8)
      .map((page) => ({ page, crumb: describe(page), lines: 0 }));
  }

  const phrase = words.join(" ");
  const holdsAll = (text: string) => { const lower = text.toLowerCase(); return words.every((word) => lower.includes(word)); };
  // A mention is found — and shown — by the title it reads as.
  const readable = readableDescriber((pageId) => {
    const target = allPages[pageId];
    return target ? pageName(target) : "";
  });
  const scored: Array<{ hit: Hit; rank: number; updated: string }> = [];
  for (const page of pages) {
    // A block is a page, found by its name — or, unnamed, its first line.
    const title = pageName(page);
    const lower = title.toLowerCase();
    if (lower.startsWith(phrase) || lower.startsWith(words[0]) && holdsAll(title)) {
      scored.push({ hit: { page, crumb: describe(page), lines: 0 }, rank: 0, updated: page.updated_at });
      continue;
    }
    if (holdsAll(title)) {
      scored.push({ hit: { page, crumb: describe(page), lines: 0 }, rank: 1, updated: page.updated_at });
      continue;
    }
    const lines = orderedBlocks(page.block_tree)
      .map((block) => ({
        blockId: block.id,
        // A table's words are its cells'.
        text: block.kind === "table"
          ? tableCells(tableGrid(block.table)).flat().map(inlinePlainText).filter(Boolean).join(" · ")
          : inlineReadableText(block.content, readable),
      }))
      .filter((line) => line.text);
    const whole = lines.filter((line) => holdsAll(line.text));
    if (whole.length) {
      scored.push({
        hit: { page, crumb: describe(page), excerpt: excerptOf(whole[0].text, words), blockId: whole[0].blockId, lines: whole.length },
        rank: 2,
        updated: page.updated_at,
      });
      continue;
    }
    // Words spread over several lines: the page has them all, each somewhere.
    if (words.length > 1 && words.every((word) => lines.some((line) => line.text.toLowerCase().includes(word)))) {
      const some = lines.find((line) => words.some((word) => line.text.toLowerCase().includes(word)));
      scored.push({
        hit: { page, crumb: describe(page), excerpt: some ? excerptOf(some.text, words) : undefined, blockId: some?.blockId, lines: 0 },
        rank: 3,
        updated: page.updated_at,
      });
    }
  }
  return scored
    .toSorted((left, right) => left.rank - right.rank || right.updated.localeCompare(left.updated))
    .slice(0, MAX_HITS)
    .map((entry) => entry.hit);
}
