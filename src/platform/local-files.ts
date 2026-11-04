import { committed, database, requested } from './storage';
export interface FileUrl { url: string | null; ready: Promise<string>; release(): void }
export const isRetryable = (_error: unknown): boolean => false;
export const fileKey = (path: string, spaceId?: string) => `${spaceId ?? 'personal'}\n${path}`;
export async function writeFile(path: string, blob: Blob, spaceId?: string): Promise<void> {
  const db = await database();
  const transaction = db.transaction('files', 'readwrite');
  const done = committed(transaction);
  transaction.objectStore('files').put(blob, fileKey(path, spaceId));
  await done;
}
export async function readFile(path: string, spaceId?: string): Promise<Blob> {
  const db = await database();
  const blob = await requested(db.transaction('files').objectStore('files').get(fileKey(path, spaceId)));
  if (!(blob instanceof Blob)) throw new Error('This attachment is missing from local storage.');
  return blob;
}
export function fileUrl(path: string, spaceId?: string): FileUrl {
  let released = false;
  let url: string | null = null;
  const ready = readFile(path, spaceId).then(blob => {
    if (released) throw new Error('Attachment view closed.');
    url = URL.createObjectURL(blob);
    return url;
  });
  ready.catch(() => undefined);
  return { get url() { return url; }, ready, release() { released = true; if (url) URL.revokeObjectURL(url); } };
}
