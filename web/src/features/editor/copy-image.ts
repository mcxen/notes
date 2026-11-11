/* Selected paragraphs, copied as a picture.

   The paragraphs are cloned into a stage of their own beside the page — inside
   the same parent, so the page's stylesheet still reaches them — drawn into a
   PNG by html-to-image, and put on the system clipboard: through the desktop
   app's clipboard where there is one, the browser's elsewhere. The page itself
   is never touched: no selection outline, no handles, no caret in the picture. */

import { toCanvas } from "html-to-image";
import { isMyGo } from "mygo-runtime";
import { Notebooks } from "../../platform/mygo";
import { base64Of } from "../../platform/local-files";

/** Controls that belong to editing, not to the paragraphs: left out of the picture. */
const CHROME = [
  ".block__add", ".block__handle", ".page-frame__handle", ".block__code-bar button",
  ".remote-caret", ".block__drop", "[data-copy-image-skip]",
].join(",");

const MARGIN = 24;
const MAX_SIDE = 16000;

/** The rows of `ids` in page order — those that are on screen. */
function rowsOf(root: HTMLElement, ids: readonly string[]): HTMLElement[] {
  const wanted = new Set(ids);
  return [...root.querySelectorAll<HTMLElement>("[data-block-id]")].filter((row) => wanted.has(row.dataset.blockId ?? ""));
}

/** Draws the paragraphs `ids` of the editor `root` as a PNG. */
export async function renderBlocksPng(root: HTMLElement, ids: readonly string[]): Promise<Blob> {
  const rows = rowsOf(root, ids);
  const parent = root.parentElement;
  if (!rows.length || !parent) throw new Error("Nothing to copy.");

  const styles = getComputedStyle(document.documentElement);
  const paper = styles.getPropertyValue("--paper").trim() || "#ffffff";
  const stage = document.createElement("div");
  stage.className = `${root.className} copy-image-stage`.replace("is-selecting", "");
  // The stage is drawn as it stands, so it sits in normal flow; its holder is
  // what is kept out of sight.
  const holder = document.createElement("div");
  holder.setAttribute("aria-hidden", "true");
  holder.style.cssText = "position:fixed;left:-100000px;top:0;pointer-events:none;";
  stage.style.cssText = [
    `width:${Math.max(240, Math.round(root.getBoundingClientRect().width))}px`,
    `padding:${MARGIN}px`, "box-sizing:content-box", `background:${paper}`,
  ].join(";");
  for (const row of rows) {
    const copy = row.cloneNode(true) as HTMLElement;
    copy.classList.remove("is-selected", "is-dragging");
    copy.removeAttribute("data-drop");
    copy.querySelectorAll(CHROME).forEach((node) => node.remove());
    copy.querySelectorAll("[contenteditable]").forEach((node) => node.removeAttribute("contenteditable"));
    stage.append(copy);
  }
  holder.append(stage);
  parent.append(holder);
  try {
    const width = stage.offsetWidth;
    const height = stage.offsetHeight;
    const pixelRatio = Math.max(1, Math.min(2, MAX_SIDE / Math.max(width, height, 1), window.devicePixelRatio || 1));
    // The pictures are not drawn by the renderer: it draws the page into an image
    // of its own, and a photo that big (read from the vault and written out as
    // text) was left blank there in a macOS web view. They are lifted out of the
    // clone and painted on the finished canvas, where a picture is only pixels.
    const pictures = liftPictures(stage);
    const options = { pixelRatio, backgroundColor: paper, cacheBust: false, width, height } as const;
    // WebKit draws pictures only on the second pass (html-to-image's note). Its
    // user agent has no "Safari" in a web view, so it is told by AppleWebKit.
    if (isWebKit()) await toCanvas(stage, options);
    const canvas = await toCanvas(stage, options);
    await paintPictures(canvas, pictures, canvas.width / width);
    const blob = await new Promise<Blob | null>((done) => canvas.toBlob(done, "image/png"));
    if (!blob) throw new Error("The picture could not be drawn.");
    return blob;
  } finally {
    holder.remove();
  }
}

/** Whether this is WebKit (Safari, a macOS web view), not Chromium. */
export function isWebKit(agent: string = navigator.userAgent): boolean {
  return /AppleWebKit/.test(agent) && !/Chrom(e|ium)|Edg\//.test(agent);
}

/** The address a CSS `background-image` names, or null when it names none. */
export function backgroundUrl(value: string): string | null {
  const match = /url\(\s*(["']?)(.*?)\1\s*\)/.exec(value);
  return match && match[2] ? match[2] : null;
}

interface Picture {
  url: string;
  /** Where it stands in the stage, in CSS pixels. */
  x: number;
  y: number;
  width: number;
  height: number;
  radius: number;
  /** Fills its box (cropped) rather than stretching to it. */
  cover: boolean;
}

/** Takes the pictures out of the stage — a picture among the words is a
 *  background on its token, and an image is an element — leaving their boxes
 *  empty, and says where each stood. */
function liftPictures(stage: HTMLElement): Picture[] {
  const origin = stage.getBoundingClientRect();
  const found: Picture[] = [];
  for (const element of stage.querySelectorAll<HTMLElement>("*")) {
    const url = element instanceof HTMLImageElement ? element.currentSrc || element.src : backgroundUrl(element.style.backgroundImage);
    if (!url) continue;
    const box = element.getBoundingClientRect();
    if (box.width < 1 || box.height < 1) continue;
    const look = getComputedStyle(element);
    found.push({
      url,
      x: box.left - origin.left,
      y: box.top - origin.top,
      width: box.width,
      height: box.height,
      radius: parseFloat(look.borderTopLeftRadius) || 0,
      cover: element instanceof HTMLImageElement ? look.objectFit === "cover" : look.backgroundSize === "cover",
    });
    if (element instanceof HTMLImageElement) element.style.visibility = "hidden";
    else element.style.backgroundImage = "none";
  }
  return found;
}

/** Paints the pictures on the canvas at `ratio` pixels to a CSS pixel. A
 *  picture that cannot be read leaves its box as it was. */
async function paintPictures(canvas: HTMLCanvasElement, pictures: readonly Picture[], ratio: number): Promise<void> {
  const context = canvas.getContext("2d");
  if (!context) return;
  for (const picture of pictures) {
    let bitmap: ImageBitmap | null = null;
    try {
      const response = await fetch(picture.url);
      if (!response.ok) continue;
      bitmap = await createImageBitmap(await response.blob());
    } catch {
      continue;
    }
    try {
      const x = picture.x * ratio;
      const y = picture.y * ratio;
      const w = picture.width * ratio;
      const h = picture.height * ratio;
      context.save();
      context.beginPath();
      if (picture.radius > 0 && "roundRect" in context) context.roundRect(x, y, w, h, picture.radius * ratio);
      else context.rect(x, y, w, h);
      context.clip();
      if (picture.cover) {
        const scale = Math.max(w / bitmap.width, h / bitmap.height);
        const sw = w / scale;
        const sh = h / scale;
        context.drawImage(bitmap, (bitmap.width - sw) / 2, (bitmap.height - sh) / 2, sw, sh, x, y, w, h);
      } else {
        context.drawImage(bitmap, x, y, w, h);
      }
      context.restore();
    } finally {
      bitmap.close();
    }
  }
}

/** Puts a PNG on the clipboard. */
export async function writePngToClipboard(blob: Blob): Promise<void> {
  if (isMyGo()) {
    await Notebooks.copyImage(await base64Of(blob));
    return;
  }
  if (!navigator.clipboard?.write || typeof ClipboardItem === "undefined") throw new Error("This browser cannot copy pictures.");
  await navigator.clipboard.write([new ClipboardItem({ "image/png": blob })]);
}

/** Copies the paragraphs `ids` of the editor `root` as a picture. */
export async function copyBlocksAsImage(root: HTMLElement, ids: readonly string[]): Promise<void> {
  await writePngToClipboard(await renderBlocksPng(root, ids));
}
