/* The desktop app's record store: the vault folder behind the Go service. The
   vault holds the member's own notebook only, so other scopes are empty. */
import { Notebooks, events, type Write } from './mygo';
import type { RecordWrite } from './records';
import type { RecordStore } from './store';

const PERSONAL = 'personal';

function vaultWrite(write: RecordWrite): Write {
  if (write.action === 'delete') return { action: 'delete', collection: write.collection, id: write.id };
  if (write.action === 'put') return { action: 'put', collection: write.collection, id: write.id, value: write.value as Record<string, unknown> };
  return { action: 'patch', collection: write.collection, id: write.id, patch: write.patch };
}

export const mygoStore: RecordStore = {
  async snapshot(scope) {
    if (scope !== PERSONAL) return [];
    const snapshot = await Notebooks.snapshot();
    return snapshot.records.map(row => ({ collection: row.collection, id: row.id, value: row.value }));
  },
  async versions(scope, pageId) {
    if (scope !== PERSONAL) return [];
    const rows = await Notebooks.versions(pageId);
    return rows.map(row => ({ collection: row.collection, id: row.id, value: row.value }));
  },
  async write(_scope, write) {
    const ack = await Notebooks.write(vaultWrite(write));
    return ack.value ?? null;
  },
  onChange(listener) {
    return events.vaultChanged.on(({ changes }) => listener(changes.map(change => ({
      collection: change.collection,
      id: change.id,
      value: change.deleted ? null : change.value,
    }))));
  },
};
