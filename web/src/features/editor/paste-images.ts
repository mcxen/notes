import { isMyGo } from "mygo-runtime";
import { Notebooks } from "../../platform/mygo";
import type { ImageSlot } from "./html-paste";

/* The pictures of a page pasted from the web, as files. In the desktop app
   Go fetches them (a page may not read another site's pictures, and Go can
   refuse addresses that are not on the open internet); a picture that came in
   the HTML as data is already in hand. */

/** The picture kinds a note keeps and a page draws. */
const KINDS = new Set(["image/png", "image/jpeg", "image/gif", "image/webp", "image/bmp"]);
const MAX_BYTES = 7.5 * 1024 * 1024;

const EXTENSIONS: Record<string, string> = { "image/png": "png", "image/jpeg": "jpg", "image/gif": "gif", "image/webp": "webp", "image/bmp": "bmp" };

function fileOf(bytes: BlobPart, type: string, name: string): File | null {
  if (!KINDS.has(type)) return null;
  const file = new File([bytes], name, { type });
  return file.size > 0 && file.size <= MAX_BYTES ? file : null;
}

function nameFor(type: string, name?: string): string {
  return name || `image.${EXTENSIONS[type] ?? "png"}`;
}

function fromBase64(data: string): Uint8Array<ArrayBuffer> {
  const binary = atob(data);
  const bytes = new Uint8Array(binary.length);
  for (let i = 0; i < binary.length; i += 1) bytes[i] = binary.charCodeAt(i);
  return bytes;
}

/** A picture as a file, or null when it cannot be had: the host refuses it,
 *  it is gone, it is not a picture of a kind a note keeps, or it is too large.
 *  Never throws. */
export async function fetchRemoteImage(src: string): Promise<File | null> {
  try {
    if (src.startsWith("data:")) {
      const blob = await (await fetch(src)).blob();
      return fileOf(blob, blob.type, nameFor(blob.type));
    }
    if (isMyGo()) {
      const image = await Notebooks.fetchImage(src);
      return fileOf(fromBase64(image.data), image.type, nameFor(image.type, image.name));
    }
    // A browser: only what the other site lets a page read.
    const response = await fetch(src, { mode: "cors", credentials: "omit" });
    if (!response.ok) return null;
    const blob = await response.blob();
    return fileOf(blob, blob.type, nameFor(blob.type, decodeURIComponent(new URL(src).pathname.split("/").pop() ?? "")));
  } catch {
    return null;
  }
}

/** Fetches the pictures of a paste a few at a time, and gives each to `place`
 *  as it comes, unless `stillThere` says its line has changed since. Returns
 *  how many could not be had. */
export async function settleImages(
  images: readonly ImageSlot[],
  stillThere: (image: ImageSlot) => boolean,
  place: (file: File, image: ImageSlot) => Promise<void>,
  fetchImage: (src: string) => Promise<File | null> = fetchRemoteImage,
  parallel = 3,
): Promise<number> {
  let next = 0;
  let missed = 0;
  const worker = async () => {
    while (next < images.length) {
      const image = images[next];
      next += 1;
      const file = await fetchImage(image.src);
      if (!file) {
        missed += 1;
        continue;
      }
      if (stillThere(image)) await place(file, image);
    }
  };
  await Promise.all(Array.from({ length: Math.min(parallel, images.length) }, worker));
  return missed;
}
