/** IndexedDB is the source of truth. Successful writes mean the transaction committed. */
export const DATABASE_NAME = 'open-notes-v1';
let opening: Promise<IDBDatabase> | undefined;
export function database(): Promise<IDBDatabase> {
  return opening ??= new Promise((resolve, reject) => {
    const request = indexedDB.open(DATABASE_NAME, 1);
    request.onupgradeneeded = () => {
      request.result.createObjectStore('records', { keyPath: ['scope', 'collection', 'id'] });
      request.result.createObjectStore('files');
    };
    request.onsuccess = () => {
      const db = request.result;
      db.onversionchange = () => { db.close(); opening = undefined; };
      resolve(db);
    };
    request.onerror = () => { opening = undefined; reject(request.error); };
    request.onblocked = () => { opening = undefined; reject(new Error('Close other Notes tabs to update local storage.')); };
  });
}
export function requested<T>(request: IDBRequest<T>): Promise<T> {
  return new Promise((resolve, reject) => {
    request.onsuccess = () => resolve(request.result);
    request.onerror = () => reject(request.error);
  });
}
export function committed(transaction: IDBTransaction): Promise<void> {
  return new Promise((resolve, reject) => {
    transaction.oncomplete = () => resolve();
    transaction.onabort = () => reject(transaction.error ?? new Error('Local storage transaction failed.'));
    transaction.onerror = () => reject(transaction.error ?? new Error('Local storage is unavailable.'));
  });
}
export type StoredRecord = { scope: string; collection: string; id: string; value: Record<string, unknown> };
const events = new EventTarget();
const channel = typeof BroadcastChannel === 'undefined' ? null : new BroadcastChannel('open-notes-changes');
channel?.addEventListener('message', () => events.dispatchEvent(new Event('change')));
export function changed(): void { events.dispatchEvent(new Event('change')); channel?.postMessage('change'); }
export function onChange(listener: () => void): () => void {
  events.addEventListener('change', listener);
  return () => events.removeEventListener('change', listener);
}
export async function readRecords(): Promise<StoredRecord[]> {
  const db = await database();
  return requested(db.transaction('records').objectStore('records').getAll());
}
export function mergePatch(target: Record<string, unknown>, patch: Record<string, unknown>): Record<string, unknown> {
  const next = { ...target };
  for (const [key, value] of Object.entries(patch)) {
    if (value === null) delete next[key];
    else if (typeof value === 'object' && !Array.isArray(value)) {
      const current = next[key];
      next[key] = mergePatch(current && typeof current === 'object' && !Array.isArray(current) ? current as Record<string, unknown> : {}, value as Record<string, unknown>);
    } else next[key] = value;
  }
  return next;
}
