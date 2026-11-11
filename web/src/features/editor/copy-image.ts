/* Selected paragraphs, copied as a picture.

   The paragraphs are cloned into a stage of their own beside the page — inside
   the same parent, so the page's stylesheet still reaches them — drawn into a
   PNG by html-to-image, and put on the system clipboard: through the desktop
   app's clipboard where there is one, the browser's elsewhere. The page itself
   is never touched: no selection outline, no handles, no caret in the picture. */

import { toBlob } from "html-to-image";
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
    // Images are read by the picture's renderer; wait until each has loaded.
    await Promise.all([...stage.querySelectorAll("img")].map((image) => (
      image.complete ? undefined : new Promise<void>((done) => { image.onload = image.onerror = () => done(); })
    )));
    const width = stage.offsetWidth;
    const height = stage.offsetHeight;
    const pixelRatio = Math.max(1, Math.min(2, MAX_SIDE / Math.max(width, height, 1), window.devicePixelRatio || 1));
    const options = { pixelRatio, backgroundColor: paper, cacheBust: false, width, height } as const;
    // Safari draws images only on the second pass (html-to-image's note).
    const safari = /Safari\//.test(navigator.userAgent) && !/Chrom(e|ium)\//.test(navigator.userAgent);
    if (safari) await toBlob(stage, options);
    const blob = await toBlob(stage, options);
    if (!blob) throw new Error("The picture could not be drawn.");
    return blob;
  } finally {
    holder.remove();
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
