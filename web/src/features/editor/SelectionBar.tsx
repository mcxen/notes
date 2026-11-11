import { Copy, ImageDown, Trash2, X } from "lucide-react";
import { Tooltip } from "../../components/Tooltip";

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
    <div className="selection-bar" role="toolbar" aria-label="Selected paragraphs" onMouseDown={(event) => event.preventDefault()}>
      <span className="selection-bar__count">{count} paragraphs</span>
      <Tooltip label="Copy" hint="⌘C" placement="top-start">
        <button type="button" className="selection-bar__btn" aria-label="Copy" onClick={onCopy}>
          <Copy size={15} /><span>Copy</span>
        </button>
      </Tooltip>
      <Tooltip label="Copy as image" hint="⌘⇧C" placement="top-start">
        <button type="button" className="selection-bar__btn" aria-label="Copy as image" onClick={onCopyImage}>
          <ImageDown size={15} /><span>Copy as image</span>
        </button>
      </Tooltip>
      {readOnly ? null : (
        <Tooltip label="Delete" hint="Del" placement="top-start">
          <button type="button" className="selection-bar__btn is-danger" aria-label="Delete" onClick={onDelete}>
            <Trash2 size={15} />
          </button>
        </Tooltip>
      )}
      <Tooltip label="Clear selection" hint="Esc" placement="top-start">
        <button type="button" className="selection-bar__btn selection-bar__btn--icon" aria-label="Clear selection" onClick={onClear}>
          <X size={15} />
        </button>
      </Tooltip>
    </div>
  );
}
