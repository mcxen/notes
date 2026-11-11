import { Copy, ImageDown, Trash2, X } from "lucide-react";
import { Tooltip } from "../../components/Tooltip";
import { t, tp } from "../../i18n";

/** What can be done with paragraphs selected as blocks — several of them,
 *  since one has its handle's menu. It sits at the foot of the page, out of
 *  the way of the text, and keeps the selection when it is pressed. */
export function SelectionBar({
  count,
  readOnly,
  onCopy,
  onCopyImage,
  onDelete,
  onClear,
}: {
  count: number;
  readOnly: boolean;
  onCopy(): void;
  onCopyImage(): void;
  onDelete(): void;
  onClear(): void;
}) {
  return (
    <div className="selection-bar" role="toolbar" aria-label={t("Selected paragraphs")} onMouseDown={(event) => event.preventDefault()}>
      <span className="selection-bar__count">{tp(count, "{n} paragraph", "{n} paragraphs")}</span>
      <Tooltip label={t("Copy")} hint="⌘C" placement="top-start">
        <button type="button" className="selection-bar__btn" aria-label={t("Copy")} onClick={onCopy}>
          <Copy size={15} /><span>{t("Copy")}</span>
        </button>
      </Tooltip>
      <Tooltip label={t("Copy as image")} hint="⌘⇧C" placement="top-start">
        <button type="button" className="selection-bar__btn" aria-label={t("Copy as image")} onClick={onCopyImage}>
          <ImageDown size={15} /><span>{t("Copy as image")}</span>
        </button>
      </Tooltip>
      {readOnly ? null : (
        <Tooltip label={t("Delete")} hint="Del" placement="top-start">
          <button type="button" className="selection-bar__btn is-danger" aria-label={t("Delete")} onClick={onDelete}>
            <Trash2 size={15} />
          </button>
        </Tooltip>
      )}
      <Tooltip label={t("Clear selection")} hint="Esc" placement="top-start">
        <button type="button" className="selection-bar__btn selection-bar__btn--icon" aria-label={t("Clear selection")} onClick={onClear}>
          <X size={15} />
        </button>
      </Tooltip>
    </div>
  );
}
