import type { CSSProperties } from "react";
import type { SkinLayerView } from "../../platform/mygo";
import { layerActive, skinImageUrl, skinShape, useSkin } from "./skin-store";

/* The wallpaper under the panes: each side's art with a veil of paper over it,
   and a seam that blurs only the art. Decorative, so it takes no clicks. */

function artStyle(layer: SkinLayerView): CSSProperties {
  const style: Record<string, string> = {
    "--skin-blur": `${layer.blur}px`,
    "--skin-bright": String(1 + layer.brightness / 100),
    "--skin-zoom": String(1 + layer.zoom / 100),
    "--skin-x": `${50 + layer.offsetX / 2}%`,
    "--skin-y": `${50 + layer.offsetY / 2}%`,
  };
  if (layer.kind === "image") {
    style.backgroundImage = `url("${skinImageUrl(layer.image)}")`;
  } else if (layer.kind === "gradient") {
    const stops = [layer.from, layer.via, layer.to].filter(Boolean).join(", ");
    style.backgroundImage = `linear-gradient(${layer.angle}deg, ${stops})`;
  }
  return style as CSSProperties;
}

function Art({ layer, className }: { layer: SkinLayerView; className?: string }) {
  return <div className={`skin__art skin__art--${layer.kind} ${className ?? ""}`} style={artStyle(layer)} />;
}

export function SkinLayers() {
  const skin = useSkin();
  const shape = skinShape(skin);
  if (!shape.side && !shape.page) return null;
  const veil = (layer: SkinLayerView): CSSProperties => ({ opacity: layerActive(layer) ? layer.veil / 100 : 0 });
  /* Across the window the page has no layer to carry a veil: it has its own. */
  const pageVeil: CSSProperties = shape.span ? { opacity: skin.spanVeil / 100 } : veil(skin.page);
  return (
    <div
      className="skin"
      aria-hidden="true"
      style={{ "--skin-seam": `${skin.seam}px`, "--skin-seam-blur": `${skin.seamBlur}px` } as CSSProperties}
    >
      {shape.span ? <div className="skin__span"><Art layer={skin.sidebar} /></div> : null}
      <div className="skin__side">
        {shape.side && !shape.span ? <Art layer={skin.sidebar} /> : null}
        {shape.side ? <div className="skin__veil skin__veil--side" style={veil(skin.sidebar)} /> : null}
      </div>
      <div className="skin__page">
        {shape.page && !shape.span ? <Art layer={skin.page} /> : null}
        {shape.page ? <div className="skin__veil skin__veil--page" style={pageVeil} /> : null}
      </div>
      {shape.seam ? <div className="skin__split"><div className="skin__seam" /></div> : null}
    </div>
  );
}
