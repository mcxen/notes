/** The picture viewer's zoom: a scale (1 is the picture fitted in the window) and
 *  where its middle stands, in pixels from the middle of the stage. */
export interface Zoom {
  scale: number;
  x: number;
  y: number;
}

export const FIT: Zoom = { scale: 1, x: 0, y: 0 };

/** The least a picture is shrunk to, and the most it is enlarged: at least eight
 *  times its fitted size, and always up to four times its own pixels. */
export function limitsFor(fitWidth: number, naturalWidth: number): { min: number; max: number } {
  const actual = fitWidth > 0 && naturalWidth > 0 ? naturalWidth / fitWidth : 1;
  return { min: 0.25, max: Math.max(8, actual * 4) };
}

/** `zoom` scaled by `factor` about `point` (pixels from the middle of the stage,
 *  where the pointer is): the spot under the pointer stays under it. */
export function zoomAbout(zoom: Zoom, factor: number, point: { x: number; y: number }, limits: { min: number; max: number }): Zoom {
  const scale = Math.min(limits.max, Math.max(limits.min, zoom.scale * factor));
  const ratio = scale / zoom.scale;
  if (scale === 1) return FIT;
  return { scale, x: point.x - (point.x - zoom.x) * ratio, y: point.y - (point.y - zoom.y) * ratio };
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
