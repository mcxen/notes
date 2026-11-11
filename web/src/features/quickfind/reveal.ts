/* A search hit opens its page scrolled to the line that matched. The search
   leaves the wish here; the editor of that page takes it once the line is on
   screen. A wish nobody takes in a few seconds is forgotten. */

const LIFETIME_MS = 5000;
let wish: { pageId: string; blockId: string; at: number } | null = null;

const listeners = new Set<() => void>();

export function requestReveal(pageId: string, blockId: string): void {
  wish = { pageId, blockId, at: Date.now() };
  listeners.forEach((listener) => listener());
}

/** Calls `listener` when a wish is made — for the editor already showing its page. */
export function onReveal(listener: () => void): () => void {
  listeners.add(listener);
  return () => { listeners.delete(listener); };
}

/** The block to scroll to on `pageId`, if the search asked for one. */
export function peekReveal(pageId: string): string | null {
  if (wish && Date.now() - wish.at > LIFETIME_MS) wish = null;
  return wish && wish.pageId === pageId ? wish.blockId : null;
}

export function clearReveal(): void {
  wish = null;
}
