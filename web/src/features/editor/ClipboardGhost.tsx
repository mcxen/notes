import { ChevronLeft, ChevronRight, X } from "lucide-react";
import { memo, useMemo } from "react";
import { t, tp } from "../../i18n";
import type { ClipboardPreview } from "../../platform/mygo";
import { codeLanguage } from "./code-highlight";
import { detectCodeLanguage } from "./detect-language";

function badgeFor(item: ClipboardPreview): string {
  if (item.kind === "image") return item.preview;
  if (item.kind === "url") return "URL";
  if (item.kind === "html") {
    return item.lines && item.lines > 1
      ? tp(item.lines, "HTML · {n} line", "HTML · {n} lines")
      : "HTML";
  }
  if (item.lines && item.lines > 1) {
    const lang = item.sample && !/^\s*```/.test(item.sample) ? detectCodeLanguage(item.sample) : null;
    const lineLabel = tp(item.lines, "{n} line", "{n} lines");
    return lang ? `${codeLanguage(lang).label} · ${lineLabel}` : lineLabel;
  }
  return "";
}

export const ClipboardGhost = memo(function ClipboardGhost({
  item,
  index,
  count,
  onAdopt,
  onDismiss,
  onCycle,
}: {
  item: ClipboardPreview;
  index: number;
  count: number;
  onAdopt: () => void;
  onDismiss: () => void;
  onCycle: (delta: -1 | 1) => void;
}) {
  const badge = useMemo(() => badgeFor(item), [item]);
  const text = item.kind === "image" ? t("Image") : item.preview;

  return (
    <div className="clip-ghost" contentEditable={false} aria-hidden="true">
      <div className="clip-ghost__preview">
        {item.kind === "image" && item.imagePreview ? (
          <img className="clip-ghost__thumb" src={item.imagePreview} alt="" draggable={false} />
        ) : null}
        <span className="clip-ghost__text">{text}</span>
      </div>
      <div className="clip-ghost__meta">
        {badge ? <span className="clip-ghost__badge">{badge}</span> : null}
        {count > 1 ? (
          <span className="clip-ghost__nav">
            <button
              type="button"
              className="clip-ghost__btn"
              tabIndex={-1}
              title={t("Previous clipboard item (⌥[)")}
              onMouseDown={(event) => event.preventDefault()}
              onClick={() => onCycle(-1)}
            >
              <ChevronLeft size={12} />
            </button>
            <span className="clip-ghost__count">{`${index + 1}/${count}`}</span>
            <button
              type="button"
              className="clip-ghost__btn"
              tabIndex={-1}
              title={t("Next clipboard item (⌥])")}
              onMouseDown={(event) => event.preventDefault()}
              onClick={() => onCycle(1)}
            >
              <ChevronRight size={12} />
            </button>
          </span>
        ) : null}
        <button
          type="button"
          className="clip-ghost__kbd"
          tabIndex={-1}
          title={t("Press Tab to paste")}
          onMouseDown={(event) => event.preventDefault()}
          onClick={onAdopt}
        >
          Tab
        </button>
        <button
          type="button"
          className="clip-ghost__dismiss"
          tabIndex={-1}
          title={t("Dismiss (Esc)")}
          onMouseDown={(event) => event.preventDefault()}
          onClick={onDismiss}
        >
          <X size={12} />
        </button>
      </div>
    </div>
  );
});
