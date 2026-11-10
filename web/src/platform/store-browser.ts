/* The browser's record store: IndexedDB, for `vite dev`. A committed write means
   its transaction committed. */
import type { RecordWrite } from './records';
import type { RecordStore } from './store';
import { changed, committed, database, mergePatch, onChange, readRecords, requested } from './storage';

async function persist(scope: string, write: RecordWrite): Promise<Record<string, unknown> | null> {
  const db = await database();
  const transaction = db.transaction('records', 'readwrite');
  const done = committed(transaction);
  const store = transaction.objectStore('records');
  const key = [scope, write.collection, write.id];
  let value: Record<string, unknown> | null;
  if (write.action === 'delete') {
    store.delete(key);
    value = null;
  } else if (write.action === 'put') {
    value = write.value as Record<string, unknown>;
    store.put({ scope, collection: write.collection, id: write.id, value });
  } else {
    const before = await requested(store.get(key));
    value = mergePatch(before?.value ?? {}, write.patch);
    store.put({ scope, collection: write.collection, id: write.id, value });
  }
  await done;
  changed();
  return value;
}

export const browserStore: RecordStore = {
  async snapshot(scope) {
    return (await readRecords())
      .filter(row => row.scope === scope)
      .map(({ collection, id, value }) => ({ collection, id, value }));
  },
  write: persist,
  onChange(listener) {
    return onChange(() => listener(null));
  },
};
