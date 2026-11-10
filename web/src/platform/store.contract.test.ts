import 'fake-indexeddb/auto';
import { afterEach, describe, expect, it } from 'vitest';
import { restoreBackup } from './backup';
import type { RecordWrite } from './records';
import { browserStore } from './store-browser';

/* The store contract, shared with the desktop vault: the same scenarios run
   against the browser's store here and against internal/vault in Go
   (testdata/store-contract.json). Both must end with the same records. */
import contract from '../../../testdata/store-contract.json';

const empty = () => ({ format: 'open-notes' as const, version: 1 as const, createdAt: new Date().toISOString(), records: [], files: [] });
afterEach(async () => { await restoreBackup(empty()); });

describe('record store contract (browser)', () => {
  for (const scenario of contract.scenarios) {
    it(scenario.name, async () => {
      for (const write of scenario.writes) await browserStore.write('personal', write as RecordWrite);
      const rows = await browserStore.snapshot('personal');
      const byKey = new Map(rows.map(row => [`${row.collection}/${row.id}`, row.value]));
      for (const want of scenario.expect) {
        expect(byKey.get(`${want.collection}/${want.id}`)).toEqual(want.value);
      }
      for (const gone of scenario.absent) {
        expect(byKey.has(`${gone.collection}/${gone.id}`)).toBe(false);
      }
    });
  }
});
