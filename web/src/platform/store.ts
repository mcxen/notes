/* The saved records of a notebook, behind one small interface. In the desktop
   app they live in a vault folder, reached through the Go service
   (store-mygo.ts); in a browser, `vite dev` keeps them in IndexedDB
   (store-browser.ts). Everything above this file is the same for both. */
import { isMyGo } from 'mygo-runtime';
import type { RecordWrite } from './records';
import { browserStore } from './store-browser';
import { mygoStore } from './store-mygo';

/** A saved record: its collection, its id and its value. */
export interface StoredRow { collection: string; id: string; value: Record<string, unknown> }

/** A saved record that changed: its value now, or null when it was removed. */
export interface RowChange { collection: string; id: string; value: Record<string, unknown> | null }

export interface RecordStore {
  /** The saved records of one scope (the member's own is `personal`). */
  snapshot(scope: string): Promise<StoredRow[]>;
  /** The history records of one page, for a store whose snapshot leaves
   *  history out (the desktop app's). A store without it has them all in its
   *  snapshot. */
  versions?(scope: string, pageId: string): Promise<StoredRow[]>;
  /** Saves one write. Resolves to the record as saved, or null once it is removed. */
  write(scope: string, write: RecordWrite): Promise<Record<string, unknown> | null>;
  /** Calls `listener` when saved records change for another reason than this
   *  window's own writes: with the records that changed, or with null when the
   *  store cannot say which, so the scope has to be read again. */
  onChange(listener: (changes: RowChange[] | null) => void): () => void;
}

/** The store this window saves to. */
export function recordStore(): RecordStore {
  return isMyGo() ? mygoStore : browserStore;
}
