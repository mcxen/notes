import { orderedBlocks } from "../../core/block-tree";
import { inlinePlainText } from "../../core/inline-content";
import type { BlockTree } from "../../core/types";

export interface Heading {
  id: string;
  /** 1 for a heading 1, down to 3. */
  level: 1 | 2 | 3;
  text: string;
}

const LEVELS: Record<string, 1 | 2 | 3 | undefined> = { heading1: 1, heading2: 2, heading3: 3 };

/** The headings of a page's body, in reading order. A heading with no words is
 *  left out: there is nothing to name it by. */
export function headingsOf(tree: BlockTree): Heading[] {
  const found: Heading[] = [];
  for (const block of orderedBlocks(tree)) {
    const level = LEVELS[block.kind];
    if (!level) continue;
    const text = inlinePlainText(block.content).replace(/\s+/g, " ").trim();
    if (text) found.push({ id: block.id, level, text });
  }
  return found;
}

/** The heading a page is at: the last one whose top has passed `line`, else the
 *  first. `tops` are the headings' distances from the top of the view, in order. */
export function currentHeading(tops: readonly number[], line: number): number {
  let current = 0;
  for (let index = 0; index < tops.length; index += 1) {
    if (tops[index] <= line) current = index;
    else break;
  }
  return current;
}
