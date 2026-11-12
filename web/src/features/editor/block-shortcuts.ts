import type { BlockKind } from "../../core/types";

/** The block a key combination turns a line into, as Notion's keys do:
 *  ⌘1, ⌘2 and ⌘3 make a heading; ⌘⌥0 a paragraph, ⌘⌥1–3 headings, ⌘⌥4 a to-do,
 *  ⌘⌥5 a bulleted, ⌘⌥6 a numbered list, ⌘⌥7 a toggle, ⌘⌥8 code; and ⌘⇧7, ⌘⇧8 and
 *  ⌘⇧9 a numbered list, a bulleted list and a to-do. Read from the physical key
 *  (`code`), since ⌥ and a digit type another character on a Mac. Null for any
 *  other combination. */
export function kindForKeys(event: { code: string; metaKey: boolean; ctrlKey: boolean; shiftKey: boolean; altKey: boolean }): BlockKind | null {
  if (!(event.metaKey || event.ctrlKey)) return null;
  const digit = /^Digit(\d)$/.exec(event.code)?.[1];
  if (!digit) return null;
  if (!event.altKey && !event.shiftKey) {
    return ({ 1: "heading1", 2: "heading2", 3: "heading3" } as Record<string, BlockKind>)[digit] ?? null;
  }
  if (event.altKey && !event.shiftKey) {
    return ({ 0: "text", 1: "heading1", 2: "heading2", 3: "heading3", 4: "todo", 5: "bullet", 6: "numbered", 7: "toggle", 8: "code" } as Record<string, BlockKind>)[digit] ?? null;
  }
  if (event.shiftKey && !event.altKey) {
    return ({ 7: "numbered", 8: "bullet", 9: "todo" } as Record<string, BlockKind>)[digit] ?? null;
  }
  return null;
}
