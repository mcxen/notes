import { useSyncExternalStore } from "react";
import { isMyGo, runtime } from "mygo-runtime";
import type { SkinLayerView, SkinView } from "../../platform/mygo";

/* The wallpaper arrives with the look and again, whole, on every change. */

const NONE: SkinLayerView = {
  kind: "", image: "", from: "", via: "", to: "", angle: 0, blur: 0, veil: 0, brightness: 0, zoom: 0, offsetX: 0, offsetY: 0,
};

export const NO_SKIN: SkinView = { sidebar: NONE, page: NONE, span: false, spanVeil: 0, seam: 0, seamBlur: 0 };

let current: SkinView = NO_SKIN;
const listeners = new Set<() => void>();

export function setSkin(next: SkinView | undefined): void {
  current = next ?? NO_SKIN;
  for (const listener of listeners) listener();
}

function subscribe(listener: () => void): () => void {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

export function useSkin(): SkinView {
  return useSyncExternalStore(subscribe, () => current);
}

export const layerActive = (layer: SkinLayerView): boolean => layer.kind === "image" || layer.kind === "gradient";


export function skinShape(skin: SkinView) {
  const span = skin.span && skin.sidebar.kind === "image";
  return {
    span,
    side: layerActive(skin.sidebar),
    page: span || layerActive(skin.page),
    seam: skin.seam > 0 && (layerActive(skin.sidebar) || layerActive(skin.page) || span),
  };
}

/** skin:// is served at http://skin.localhost/ by WebView2 on Windows. */
export function skinImageUrl(name: string): string {
  if (!isMyGo()) return "";
  const origin = runtime().platform === "win32" ? "http://skin.localhost" : "skin://localhost";
  return `${origin}/${encodeURIComponent(name)}`;
}
