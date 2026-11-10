import { describe, expect, it } from 'vitest';
import { Plane } from './plane';
import type { RecordWrite } from './records';
import type { RecordStore, RowChange, StoredRow } from './store';

/** An in-memory store that behaves as the vault and IndexedDB stores do, with
 *  hooks to delay a save, fail it, or announce a change from elsewhere. */
function memoryStore() {
  const rows = new Map<string, StoredRow>();
  let listener: ((changes: RowChange[] | null) => void) | null = null;
  let gate: Promise<void> = Promise.resolve();
  let fail: Error | null = null;
  let snapshots = 0;
  const store: RecordStore = {
    async snapshot(scope) {
      snapshots += 1;
      return scope === 'personal' ? [...rows.values()].map(row => ({ ...row })) : [];
    },
    async write(_scope, write: RecordWrite) {
      await gate;
      if (fail) throw fail;
      const key = `${write.collection}/${write.id}`;
      if (write.action === 'delete') {
        rows.delete(key);
        return null;
      }
      const value = write.action === 'put'
        ? write.value as Record<string, unknown>
        : { ...(rows.get(key)?.value ?? {}), ...write.patch };
      rows.set(key, { collection: write.collection, id: write.id, value });
      return value;
    },
    onChange(next) {
      listener = next;
      return () => { listener = null; };
    },
  };
  return {
    store,
    rows,
    snapshots: () => snapshots,
    emit: (changes: RowChange[] | null) => listener?.(changes),
    delayWrites: () => {
      let release!: () => void;
      gate = new Promise<void>(resolve => { release = resolve; });
      return release;
    },
    failWrites: (error: Error | null) => { fail = error; },
  };
}

const preferences = { collection: 'workspace_settings', id: 'preferences' } as const;

describe('Plane over a record store', () => {
  it('shows a write at once, and keeps the record the store saved', async () => {
    const memory = memoryStore();
    const plane = await Plane.open(undefined, memory.store);
    await plane.write({ action: 'put', collection: 'workspace_settings', id: 'preferences', value: { theme: 'dark' } });
    expect(plane.get(preferences)).toEqual({ theme: 'dark' });
    expect(memory.rows.get('workspace_settings/preferences')?.value).toEqual({ theme: 'dark' });
    await plane.close();
  });

  it('shows a pending write before the store saves it, and drops it when the save fails', async () => {
    const memory = memoryStore();
    const plane = await Plane.open(undefined, memory.store);
    await plane.write({ action: 'put', collection: 'workspace_settings', id: 'preferences', value: { theme: 'light' } });
    const release = memory.delayWrites();
    const pending = plane.write({ action: 'patch', collection: 'workspace_settings', id: 'preferences', patch: { theme: 'dark' } });
    expect(plane.get(preferences)).toEqual({ theme: 'dark' });
    memory.failWrites(new Error('disk full'));
    release();
    await expect(pending).rejects.toThrow('disk full');
    expect(plane.get(preferences)).toEqual({ theme: 'light' });
    await plane.close();
  });

  it('applies changes from elsewhere one record at a time, without reading the scope again', async () => {
    const memory = memoryStore();
    const plane = await Plane.open(undefined, memory.store);
    const before = memory.snapshots();
    memory.emit([{ collection: 'workspace_settings', id: 'preferences', value: { theme: 'light' } }]);
    expect(plane.get(preferences)).toEqual({ theme: 'light' });
    memory.emit([{ collection: 'workspace_settings', id: 'preferences', value: null }]);
    expect(plane.get(preferences)).toBeUndefined();
    expect(memory.snapshots()).toBe(before);
    await plane.close();
  });

  it('keeps a pending local write over a change to the same record from elsewhere', async () => {
    const memory = memoryStore();
    const plane = await Plane.open(undefined, memory.store);
    await plane.write({ action: 'put', collection: 'workspace_settings', id: 'preferences', value: { theme: 'light', roots: [] } });
    const release = memory.delayWrites();
    const pending = plane.write({ action: 'patch', collection: 'workspace_settings', id: 'preferences', patch: { theme: 'dark' } });
    const external = { theme: 'system', roots: ['x'] };
    memory.rows.set('workspace_settings/preferences', { collection: 'workspace_settings', id: 'preferences', value: external });
    memory.emit([{ collection: 'workspace_settings', id: 'preferences', value: external }]);
    expect(plane.get(preferences)).toEqual({ theme: 'dark', roots: ['x'] });
    release();
    await pending;
    expect(plane.get(preferences)).toEqual({ theme: 'dark', roots: ['x'] });
    await plane.close();
  });

  it('reads the whole scope again when the store cannot say what changed', async () => {
    const memory = memoryStore();
    const plane = await Plane.open(undefined, memory.store);
    const before = memory.snapshots();
    memory.rows.set('workspace_settings/preferences', { collection: 'workspace_settings', id: 'preferences', value: { theme: 'ink' } });
    memory.emit(null);
    await new Promise(resolve => setTimeout(resolve, 0));
    expect(memory.snapshots()).toBe(before + 1);
    expect(plane.get(preferences)).toEqual({ theme: 'ink' });
    await plane.close();
  });

  it('ignores changes to collections the notebook does not keep', async () => {
    const memory = memoryStore();
    const plane = await Plane.open(undefined, memory.store);
    memory.emit([{ collection: 'presence', id: 'x', value: { a: 1 } }]);
    expect(plane.get(preferences)).toBeUndefined();
    await plane.close();
  });
});
