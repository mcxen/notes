/* A drawn diagram as a file: the SVG Mermaid made, made to stand on its own,
   and a PNG drawn from it. Mermaid's colours are solid by the time they are in
   the SVG (see mermaid-render.ts), so the file looks as the page does without
   the page's stylesheet, and its labels are SVG text, not HTML in a
   foreignObject — a canvas that drew one would be refused by some web views. */

export interface DiagramSize {
  width: number;
  height: number;
}

/** The size a diagram is drawn at, from its viewBox. */
export function diagramSize(svg: string): DiagramSize | null {
  const tag = /<svg\b[^>]*>/.exec(svg)?.[0];
  const box = tag && /\bviewBox\s*=\s*"([^"]*)"/.exec(tag);
  if (!box) return null;
  const [, , width, height] = box[1].trim().split(/[\s,]+/).map(Number);
  return Number.isFinite(width) && Number.isFinite(height) && width > 0 && height > 0 ? { width, height } : null;
}

/** The SVG with a size of its own (the viewBox's, times `scale`) in place of
 *  "fill the block", and a `background` painted behind it when one is given. */
export function standaloneSvg(svg: string, { scale = 1, background = null }: { scale?: number; background?: string | null } = {}): string {
  const size = diagramSize(svg);
  const tag = /<svg\b[^>]*>/.exec(svg)?.[0];
  if (!size || !tag) return svg;
  const width = Math.round(size.width * scale);
  const height = Math.round(size.height * scale);
  let open = tag
    .replace(/\s(?:width|height|style)\s*=\s*"[^"]*"/g, "")
    .replace(/^<svg\b/, `<svg width="${width}" height="${height}"`);
  if (!/\sxmlns\s*=/.test(open)) open = open.replace(/^<svg\b/, '<svg xmlns="http://www.w3.org/2000/svg"');
  const view = /\bviewBox\s*=\s*"([^"]*)"/.exec(tag)?.[1].trim().split(/[\s,]+/).map(Number) ?? [0, 0, size.width, size.height];
  const paint = background ? `<rect x="${view[0]}" y="${view[1]}" width="${size.width}" height="${size.height}" fill="${background}"/>` : "";
  return svg.replace(tag, open + paint);
}

export function svgBlob(svg: string): Blob {
  return new Blob([svg], { type: "image/svg+xml;charset=utf-8" });
}

const MAX_SIDE = 8000;

/** A PNG of the diagram, twice its size (less if that would be very large). */
export async function svgToPng(svg: string, background: string): Promise<Blob> {
  const size = diagramSize(svg);
  if (!size) throw new Error("The diagram has no size.");
  const ratio = Math.max(1, Math.min(2, MAX_SIDE / Math.max(size.width, size.height)));
  const source = svgBlob(standaloneSvg(svg, { scale: ratio, background }));
  const url = URL.createObjectURL(source);
  try {
    const image = new Image();
    image.src = url;
    await image.decode();
    const canvas = document.createElement("canvas");
    canvas.width = Math.round(size.width * ratio);
    canvas.height = Math.round(size.height * ratio);
    const context = canvas.getContext("2d");
    if (!context) throw new Error("The diagram could not be drawn.");
    context.fillStyle = background;
    context.fillRect(0, 0, canvas.width, canvas.height);
    context.drawImage(image, 0, 0, canvas.width, canvas.height);
    const png = await new Promise<Blob | null>((done) => canvas.toBlob(done, "image/png"));
    if (!png) throw new Error("The diagram could not be drawn.");
    return png;
  } finally {
    URL.revokeObjectURL(url);
  }
}

/** Saves a file the way a download does. */
export function saveBlob(blob: Blob, name: string): void {
  const url = URL.createObjectURL(blob);
  const link = document.createElement("a");
  link.href = url;
  link.download = name;
  link.rel = "noopener";
  document.body.append(link);
  link.click();
  link.remove();
  setTimeout(() => URL.revokeObjectURL(url), 30_000);
}
