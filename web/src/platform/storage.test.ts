import 'fake-indexeddb/auto';
import { afterEach, describe, expect, it } from 'vitest';
import { Plane } from './plane';
import { exportBackup, parseBackup, restoreBackup } from './backup';
import { readFile, writeFile } from './local-files';

const empty = () => ({ format: 'open-notes' as const, version: 1 as const, createdAt: new Date().toISOString(), records: [], files: [] });
afterEach(async () => { await restoreBackup(empty()); });

describe('persistent local notebook', () => {
  it('keeps committed changes after closing and reopening the notebook', async () => {
    const first = await Plane.open();
    await first.write({ action: 'put', collection: 'workspace_settings', id: 'preferences', value: { theme: 'dark' } });
    await first.close();
    const second = await Plane.open();
    expect(second.get({ collection: 'workspace_settings', id: 'preferences' })).toEqual({ theme: 'dark' });
    await second.close();
  });
  it('applies patches against the stored value so simultaneous field updates both survive', async () => {
    const first = await Plane.open();
    const second = await Plane.open();
    await first.write({ action: 'put', collection: 'workspace_settings', id: 'preferences', value: { theme: 'light', roots: ['a'] } });
    await Promise.all([
      first.write({ action: 'patch', collection: 'workspace_settings', id: 'preferences', patch: { theme: 'dark' } }),
      second.write({ action: 'patch', collection: 'workspace_settings', id: 'preferences', patch: { roots: null, sidebar_width: 280 } }),
    ]);
    const reopened = await Plane.open();
    expect(reopened.get({ collection: 'workspace_settings', id: 'preferences' })).toEqual({ theme: 'dark', sidebar_width: 280 });
    await Promise.all([first.close(), second.close(), reopened.close()]);
  });
  it('keeps the newest text visible while rapid edits are still committing', async () => {
    const plane = await Plane.open();
    await plane.write({ action: 'put', collection: 'workspace_settings', id: 'preferences', value: { roots: [] } });
    const writes: Promise<unknown>[] = [];
    let newest = 0;
    let regressed = false;
    const stop = plane.watch(records => {
      const length = (records.workspace_settings.get('preferences') as { roots?: string[] })?.roots?.length ?? 0;
      if (length < newest) regressed = true;
    });
    for (let i = 1; i <= 30; i++) {
      newest = i;
      writes.push(plane.write({ action: 'patch', collection: 'workspace_settings', id: 'preferences', patch: { roots: Array.from({ length: i }, (_, k) => String(k)) } }));
      expect((plane.get({ collection: 'workspace_settings', id: 'preferences' }) as { roots: string[] }).roots).toHaveLength(i);
      await new Promise(resolve => setTimeout(resolve, 0));
    }
    await Promise.all(writes);
    expect(regressed).toBe(false);
    stop(); await plane.close();
    const reopened = await Plane.open();
    expect((reopened.get({ collection: 'workspace_settings', id: 'preferences' }) as { roots: string[] }).roots).toHaveLength(30);
    await reopened.close();
  });
  it('round trips records and binary attachments in a complete backup', async () => {
    const plane = await Plane.open();
    await plane.write({ action: 'put', collection: 'workspace_settings', id: 'preferences', value: { theme: 'dark' } });
    await writeFile('photos/test.bin', new Blob([new Uint8Array([0, 128, 255, 42])], { type: 'application/octet-stream' }));
    const backup = parseBackup(JSON.stringify(await exportBackup()));
    await restoreBackup(empty());
    await expect(readFile('photos/test.bin')).rejects.toThrow('missing');
    await restoreBackup(backup);
    expect(new Uint8Array(await (await readFile('photos/test.bin')).arrayBuffer())).toEqual(new Uint8Array([0, 128, 255, 42]));
    expect((await exportBackup()).records).toEqual(backup.records);
    await plane.close();
  });
  it('rejects invalid restore data before modifying existing records', async () => {
    const plane = await Plane.open();
    await plane.write({ action: 'put', collection: 'workspace_settings', id: 'preferences', value: { theme: 'dark' } });
    await expect(restoreBackup({ ...empty(), files: [{ key: 'personal\nx', type: 'text/plain', data: '%%%invalid' }] })).rejects.toThrow();
    expect((await exportBackup()).records).toHaveLength(1);
    await plane.close();
  });
});
