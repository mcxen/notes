import { isMyGo, runtime } from 'mygo-runtime';
import { Notebooks } from './mygo';
import { committed, database, requested } from './storage';
export interface FileUrl { url: string | null; ready: Promise<string>; release(): void }
export const isRetryable = (_error: unknown): boolean => false;
export const fileKey = (path: string, spaceId?: string) => `${spaceId ?? 'personal'}\n${path}`;

/* The desktop app keeps attachments in the vault folder. The Go service writes
   them there, and its vault:// scheme serves them back under the vault's id:
   the address of a file is vault://localhost/<vault id>/<its path>, which
   WebView2 on Windows serves at http://vault.localhost/<vault id>/<its path>.
   In a browser they stay in IndexedDB, as before. */

let vaultId: Promise<string> | undefined;
function vaultIdentity(): Promise<string> {
  return vaultId ??= Notebooks.info().then(info => info.id);
}

async function vaultUrl(path: string): Promise<string> {
  const origin = runtime().platform === 'win32' ? 'http://vault.localhost' : 'vault://localhost';
  const segments = [await vaultIdentity(), ...path.split('/')].map(encodeURIComponent);
  return `${origin}/${segments.join('/')}`;
}

export async function base64Of(blob: Blob): Promise<string> {
  const bytes = new Uint8Array(await blob.arrayBuffer());
  let text = '';
  for (let start = 0; start < bytes.length; start += 8192) text += String.fromCharCode(...bytes.subarray(start, start + 8192));
  return btoa(text);
}

export async function writeFile(path: string, blob: Blob, spaceId?: string): Promise<void> {
  if (isMyGo()) {
    await Notebooks.writeFile(path, await base64Of(blob));
    return;
  }
  const db = await database();
  const transaction = db.transaction('files', 'readwrite');
  const done = committed(transaction);
  transaction.objectStore('files').put(blob, fileKey(path, spaceId));
  await done;
}

export async function readFile(path: string, spaceId?: string): Promise<Blob> {
  if (isMyGo()) {
    const response = await fetch(await vaultUrl(path));
    if (!response.ok) throw new Error('This attachment is missing from local storage.');
    return response.blob();
  }
  const db = await database();
  const blob = await requested(db.transaction('files').objectStore('files').get(fileKey(path, spaceId)));
  if (!(blob instanceof Blob)) throw new Error('This attachment is missing from local storage.');
  return blob;
}

export function fileUrl(path: string, spaceId?: string): FileUrl {
  if (isMyGo()) {
    let url: string | null = null;
    const ready = vaultUrl(path).then(address => {
      url = address;
      return address;
    });
    ready.catch(() => undefined);
    return { get url() { return url; }, ready, release() {} };
  }
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
