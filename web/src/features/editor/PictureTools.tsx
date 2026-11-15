import { useLayoutEffect, useRef, useState } from "react";
import { AlignCenter, AlignLeft, AlignRight, Download, Maximize2 } from "lucide-react";
import type { Block } from "../../core/types";
import { t } from "../../i18n";
import { toolsPlace } from "./pictures";

const ALIGNMENTS = [
  { value: undefined, label: "Align left", Icon: AlignLeft },
  { value: "center", label: "Align center", Icon: AlignCenter },
  { value: "right", label: "Align right", Icon: AlignRight },
] as const;

/** A picture's tools, at its top right corner while the pointer is on it —
 *  or just outside a picture too small to hold them (`toolsPlace`), so they
 *  never lie over the bars that size it: where its line stands across the
 *  page — the picture with its words — and its bytes, saved or looked at
 *  whole. */
export function PictureTools({
  rect,
  align,
  readOnly,
  onAlign,
  onDownload,
  onView,
  onEnter,
  onLeave,
}: {
  rect: DOMRect;
  align: Block["align"];
  readOnly: boolean;
  onAlign(align: Block["align"]): void;
  onDownload(): void;
  onView(): void;
  onEnter(): void;
  onLeave(): void;
}) {
  // Placed once their own size is known: out of sight for the one frame before.
  const bar = useRef<HTMLDivElement | null>(null);
  const [size, setSize] = useState<{ width: number; height: number } | null>(null);
  useLayoutEffect(() => {
    const element = bar.current;
    if (!element) return;
    const next = { width: element.offsetWidth, height: element.offsetHeight };
    setSize((current) => (current && current.width === next.width && current.height === next.height ? current : next));
  }, [readOnly]);
  const place = size
    ? toolsPlace(rect, size, align, { width: window.innerWidth, height: window.innerHeight })
    : { left: 0, top: 0 };
  return (
    <div
      ref={bar}
      className="picture-tools"
      style={{ ...place, visibility: size ? undefined : "hidden" }}
      onPointerEnter={onEnter}
      onPointerLeave={onLeave}
      // The caret stays where it was.
      onMouseDown={(event) => event.preventDefault()}
    >
      {readOnly ? null : (
        <span className="picture-tools__group" role="group" aria-label={t("Align")}>
          {ALIGNMENTS.map(({ value, label, Icon }) => (
            <button
              key={label}
              type="button"
              className={`block__image-tool ${align === value ? "is-active" : ""}`}
              aria-label={t(label)}
              aria-pressed={align === value}
              onClick={() => onAlign(value)}
            >
              <Icon size={14} />
            </button>
          ))}
        </span>
      )}
      <button type="button" className="block__image-tool" aria-label={t("Download")} onClick={onDownload}>
        <Download size={14} />
      </button>
      <button type="button" className="block__image-tool" aria-label={t("View full size")} onClick={onView}>
        <Maximize2 size={14} />
      </button>
    </div>
  );
}
