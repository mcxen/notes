import { changed, committed, database, readRecords, requested, type StoredRecord } from './storage';
export interface Backup { format: 'open-notes'; version: 1; createdAt: string; records: StoredRecord[]; files: { key: string; type: string; data: string }[] }
export async function exportBackup(): Promise<Backup> {
  const db = await database();
  const transaction = db.transaction(['records', 'files']);
  const done = committed(transaction);
  const [records, keys, blobs] = await Promise.all([
    requested(transaction.objectStore('records').getAll()) as Promise<StoredRecord[]>,
    requested(transaction.objectStore('files').getAllKeys()),
    requested(transaction.objectStore('files').getAll()) as Promise<Blob[]>,
  ]);
  await done;
  const files = await Promise.all(blobs.map(async (blob, i) => {
    const bytes = new Uint8Array(await blob.arrayBuffer());
    let text = '';
    for (let start = 0; start < bytes.length; start += 8192) text += String.fromCharCode(...bytes.subarray(start, start + 8192));
    return { key: String(keys[i]), type: blob.type, data: btoa(text) };
  }));
  return { format: 'open-notes', version: 1, createdAt: new Date().toISOString(), records, files };
}
export function parseBackup(text: string): Backup {
  const parsed = JSON.parse(text) as Backup;
  if (!parsed || parsed.format !== 'open-notes' || parsed.version !== 1 || !Array.isArray(parsed.records) || !Array.isArray(parsed.files)) throw new Error('Choose an Open Notes backup JSON file.');
  const collections = new Set(['pages', 'blocks', 'versions', 'workspace_settings']);
  const seen = new Set<string>();
  for (const row of parsed.records) {
    if (!row || row.scope !== 'personal' || !collections.has(row.collection) || typeof row.id !== 'string' || !row.id || !row.value || typeof row.value !== 'object' || Array.isArray(row.value)) throw new Error('The backup contains an invalid record.');
    const key = JSON.stringify([row.scope, row.collection, row.id]);
    if (seen.has(key)) throw new Error('The backup contains duplicate records.');
    seen.add(key);
  }
  const keys = new Set<string>();
  for (const file of parsed.files) {
    if (!file || typeof file.key !== 'string' || !file.key.startsWith('personal\n') || typeof file.type !== 'string' || typeof file.data !== 'string' || keys.has(file.key)) throw new Error('The backup contains an invalid attachment.');
    atob(file.data); keys.add(file.key);
  }
  return parsed;
}
export async function restoreBackup(backup: Backup): Promise<void> {
  // Validate again so callers cannot bypass validation. Prepare blobs before opening a transaction.
  const safe = parseBackup(JSON.stringify(backup));
  const files = safe.files.map(file => ({ key: file.key, blob: new Blob([Uint8Array.from(atob(file.data), char => char.charCodeAt(0))], { type: file.type }) }));
  const db = await database();
  const transaction = db.transaction(['records', 'files'], 'readwrite');
  const done = committed(transaction);
  const records = transaction.objectStore('records');
  const store = transaction.objectStore('files');
  records.clear(); store.clear();
  safe.records.forEach(row => records.put(row));
  files.forEach(file => store.put(file.blob, file.key));
  await done; changed();
}
export async function hasLocalNotes(): Promise<boolean> { return (await readRecords()).some(row => row.collection === 'pages'); }
