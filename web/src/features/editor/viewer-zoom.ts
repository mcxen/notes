/** The picture viewer's zoom: a scale (1 is the picture fitted in the window) and
 *  where its middle stands, in pixels from the middle of the stage. */
export interface Zoom {
  scale: number;
  x: number;
  y: number;
}

export const FIT: Zoom = { scale: 1, x: 0, y: 0 };

/** A size in CSS pixels: the stage the picture is shown in, or the picture fitted in it. */
export interface Box {
  width: number;
  height: number;
}

/** How far the picture may be moved from the middle of the stage, each way: half
 *  of what its zoomed size overflows the stage by, and nothing on an axis where it
 *  fits. This is what lets a very tall picture be dragged to its top and bottom
 *  however far it is zoomed, and keeps it from being dragged out of sight. */
export function panLimits(scale: number, stage: Box, fit: Box): { x: number; y: number } {
  return {
    x: Math.max(0, (fit.width * scale - stage.width) / 2),
    y: Math.max(0, (fit.height * scale - stage.height) / 2),
  };
}

/** `zoom` moved back to where the picture can be. */
export function clampPan(zoom: Zoom, stage: Box, fit: Box): Zoom {
  const limit = panLimits(zoom.scale, stage, fit);
  // `|| 0` keeps a held-still axis at 0, not -0.
  const x = Math.min(limit.x, Math.max(-limit.x, zoom.x)) || 0;
  const y = Math.min(limit.y, Math.max(-limit.y, zoom.y)) || 0;
  return x === zoom.x && y === zoom.y ? zoom : { ...zoom, x, y };
}

/** The least a picture is shrunk to, and the most it is enlarged: at least eight
 *  times its fitted size, and always up to four times its own pixels. */
export function limitsFor(fitWidth: number, naturalWidth: number): { min: number; max: number } {
  const actual = fitWidth > 0 && naturalWidth > 0 ? naturalWidth / fitWidth : 1;
  return { min: 0.25, max: Math.max(8, actual * 4) };
}

/** `zoom` scaled by `factor` about `point` (pixels from the middle of the stage,
 *  where the pointer is): the spot under the pointer stays under it. */
export function zoomAbout(
  zoom: Zoom,
  factor: number,
  point: { x: number; y: number },
  limits: { min: number; max: number },
  area?: { stage: Box; fit: Box },
): Zoom {
  const scale = Math.min(limits.max, Math.max(limits.min, zoom.scale * factor));
  const ratio = scale / zoom.scale;
  if (scale === 1) return FIT;
  const next = { scale, x: point.x - (point.x - zoom.x) * ratio, y: point.y - (point.y - zoom.y) * ratio };
  return area ? clampPan(next, area.stage, area.fit) : next;
}

/** `zoom` moved by a wheel's or a trackpad's scroll (`dx`, `dy` as the event gives
 *  them: positive scrolls toward the picture's right and bottom). */
export function scrollBy(zoom: Zoom, dx: number, dy: number, stage: Box, fit: Box): Zoom {
  return clampPan({ ...zoom, x: zoom.x - dx, y: zoom.y - dy }, stage, fit);
}

/** Whether a scroll by `dx`, `dy` has anywhere to go: the picture overflows the
 *  stage on an axis it moves along. If not, a wheel zooms instead. */
export function canScroll(zoom: Zoom, dx: number, dy: number, stage: Box, fit: Box): boolean {
  const limit = panLimits(zoom.scale, stage, fit);
  return (dx !== 0 && limit.x > 0) || (dy !== 0 && limit.y > 0);
}

/** The scale at which the picture shows one of its pixels to a CSS pixel. */
export function actualSize(fitWidth: number, naturalWidth: number): number {
  return fitWidth > 0 && naturalWidth > 0 ? naturalWidth / fitWidth : 1;
}

/** What the readout says: the share of the picture's own size it is drawn at. */
export function percentOf(scale: number, fitWidth: number, naturalWidth: number): number {
  return fitWidth > 0 && naturalWidth > 0 ? Math.round(scale * fitWidth / naturalWidth * 100) : Math.round(scale * 100);
}

/** A wheel's turn as a factor: a trackpad's pinch (a wheel with Ctrl held) turns
 *  more finely than a wheel does. */
export function wheelFactor(deltaY: number, pinch: boolean): number {
  return Math.exp(-deltaY * (pinch ? 0.01 : 0.0015));
}
