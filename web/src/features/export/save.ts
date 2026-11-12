/** Hands bytes to the reader as a file: the browser's download. In the desktop
 *  app a download is saved to the Downloads folder, a name that is taken
 *  numbered (MyGo does both), so the desktop and a browser take the same path. */
export function saveBytes(name: string, bytes: Uint8Array, mime: string): void {
  const url = URL.createObjectURL(new Blob([bytes as BlobPart], { type: mime }));
  const link = document.createElement("a");
  link.href = url;
  link.download = name;
  link.rel = "noopener";
  document.body.append(link);
  link.click();
  link.remove();
  setTimeout(() => URL.revokeObjectURL(url), 30_000);
}
