/* Find and replace within one page. Pure: a tree and a query in, matches out;
   matches and a replacement in, one transaction out — so that it can be tested,
   and so that "Replace all" is a single step to undo. */

import { blocksInOrder } from "../../core/block-tree";
import { inlinePlainText, MENTION_CHAR, replaceRange } from "../../core/inline-content";
import type { Block, BlockTree } from "../../core/types";
import { blockDefinition } from "./block-registry";
import type { EditorTransaction } from "./editor-commands";
import { updateBlockContent } from "./editor-commands";

export interface FindOptions {
  caseSensitive: boolean;
  /** A match must not touch a letter or digit on either side. */
  wholeWord: boolean;
}

export const FIND_DEFAULTS: FindOptions = { caseSensitive: false, wholeWord: false };

/** Where the words searched for are: a block, and a [start, end) range of
 *  its plain text (a mention counts as one character, as everywhere). */
export interface FindMatch {
  blockId: string;
  start: number;
  end: number;
}

const WORD = String.raw`[\p{L}\p{N}_]`;

/** The pattern for `query`, or null when there is nothing to look for. The
 *  words are taken literally, never as a pattern; the mention's invisible
 *  character is not something to search for. */
function patternFor(query: string, options: FindOptions): RegExp | null {
  const words = query.replaceAll(MENTION_CHAR, "");
  if (!words) return null;
  const literal = words.replace(/[.*+?^${}()|[\]\\]/g, String.raw`\$&`);
  const before = options.wholeWord && new RegExp(`^${WORD}`, "u").test(words) ? `(?<!${WORD})` : "";
  const after = options.wholeWord && new RegExp(`${WORD}$`, "u").test(words) ? `(?!${WORD})` : "";
  // `i` with `u` folds case one character to one, so offsets stay the text's own.
  return new RegExp(`${before}${literal}${after}`, options.caseSensitive ? "gu" : "giu");
}

/** Where `query` occurs in `text`, in order, never overlapping. */
export function findInText(text: string, query: string, options: FindOptions = FIND_DEFAULTS): Array<[number, number]> {
  const pattern = patternFor(query, options);
  if (!pattern) return [];
  return [...text.matchAll(pattern)].map((match) => [match.index, match.index + match[0].length] as [number, number]);
}

/** Whether a block's words can be found and changed. */
function searchable(block: Block): boolean {
  return blockDefinition(block.kind).editable && block.content.length > 0;
}

/** Every match on the page, in reading order. A match is within one block's
 *  words: one that would cross two lines is not a match. */
export function findInTree(tree: BlockTree, query: string, options: FindOptions = FIND_DEFAULTS): FindMatch[] {
  const pattern = patternFor(query, options);
  if (!pattern) return [];
  const found: FindMatch[] = [];
  for (const block of blocksInOrder(tree)) {
    if (!searchable(block)) continue;
    const text = inlinePlainText(block.content);
    for (const match of text.matchAll(pattern)) {
      found.push({ blockId: block.id, start: match.index, end: match.index + match[0].length });
    }
  }
  return found;
}

/** The change that puts `replacement` in place of every one of `matches`: a
 *  new run takes the marks the words had where they began. An equation's
 *  source and a code block's words stay plain, as they are stored. */
export function replaceMatches(tree: BlockTree, matches: readonly FindMatch[], replacement: string): EditorTransaction | null {
  const byBlock = new Map<string, FindMatch[]>();
  for (const match of matches) byBlock.set(match.blockId, [...byBlock.get(match.blockId) ?? [], match]);
  const mutations: EditorTransaction["mutations"] = [];
  for (const [blockId, inBlock] of byBlock) {
    const block = tree.by_id[blockId];
    if (!block) continue;
    // From the last to the first, so that each range is still where it was.
    let content = block.content;
    for (const match of inBlock.toSorted((left, right) => right.start - left.start)) {
      content = replaceRange(content, match.start, match.end, replacement);
    }
    const change = updateBlockContent(tree, blockId, content);
    if (change) mutations.push(...change.mutations);
  }
  return mutations.length ? { mutations } : null;
}
