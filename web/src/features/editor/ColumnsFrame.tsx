import { Columns2, MoreHorizontal, Rows3, Trash2 } from "lucide-react";
import { useEffect, useRef, useState, type CSSProperties, type PointerEvent, type ReactNode } from "react";
import { MenuItem, MenuList } from "../../components/Menu";
import { Popover } from "../../components/Popover";
import { rectOf, type AnchorRect } from "../../hooks/useAnchoredPosition";
import { t } from "../../i18n";

/** Columns side by side, as Notion draws them: cells with room between them
 *  for each line's "+" and handle, and between two cells a divider that is
 *  dragged to give one more room and the other less. Below `STACK_BELOW` px
 *  the cells stand one above another, which a narrow window needs more than
 *  the columns. The frame has a small menu: put the lines back in one
 *  column, or delete them with the frame. */

/** The room between two cells, where the "+" and the handle of the second
 *  cell's lines stand (they hang 37px before a line's words). */
export const COLUMN_GAP = 46;
/** Narrower than this, the columns are stacked. */
export const STACK_BELOW = 520;

export interface ColumnCell {
  id: string;
  /** Its share of the width the frame has, a fraction of one. */
  share: number;
  children: ReactNode;
}

export function ColumnsFrame({
  frameId,
  depth,
  level,
  cells,
  readOnly,
  onResize,
  onUnwrap,
  onDelete,
}: {
  frameId: string;
  depth: number;
  level: number;
  cells: ColumnCell[];
  readOnly: boolean;
  /** The divider after cell `leftId` let go: that cell has `share` of what it
   *  and the cell after it have between them. */
  onResize(leftId: string, share: number): void;
  onUnwrap(): void;
  onDelete(): void;
}) {
  const ref = useRef<HTMLDivElement | null>(null);
  const [stacked, setStacked] = useState(false);
  /** A divider being dragged: the pair's new split, drawn before it is kept. */
  const [drag, setDrag] = useState<{ leftId: string; share: number } | null>(null);
  const [menu, setMenu] = useState<AnchorRect | null>(null);

  useEffect(() => {
    const element = ref.current;
    if (!element || typeof ResizeObserver === "undefined") return;
    const watch = new ResizeObserver(() => setStacked(element.getBoundingClientRect().width < STACK_BELOW));
    watch.observe(element);
    return () => watch.disconnect();
  }, []);

  const shares = cells.map((cell, index) => {
    if (!drag) return cell.share;
    const pair = cell.share + (cells[index + 1]?.share ?? 0);
    if (cell.id === drag.leftId) return pair * drag.share;
    if (cells[index - 1]?.id === drag.leftId) return (cells[index - 1].share + cell.share) * (1 - drag.share);
    return cell.share;
  });
  const tracks = cells.map((_, index) => (
    `minmax(0, ${Math.max(shares[index], 0.01)}fr)${index < cells.length - 1 ? ` ${COLUMN_GAP}px` : ""}`
  )).join(" ");

  const start = (event: PointerEvent<HTMLDivElement>, index: number) => {
    if (readOnly || stacked || event.button !== 0) return;
    // The page's own pointer handling (selecting, moving lines) is not for this.
    event.preventDefault();
    event.stopPropagation();
    const element = event.currentTarget;
    try {
      element.setPointerCapture(event.pointerId);
    } catch {
      // A pointer that is not active (a synthetic event) is followed on the element alone.
    }
    const left = ref.current?.querySelector<HTMLElement>(`[data-column-cell="${cells[index].id}"]`)?.getBoundingClientRect();
    const right = ref.current?.querySelector<HTMLElement>(`[data-column-cell="${cells[index + 1].id}"]`)?.getBoundingClientRect();
    if (!left || !right) return;
    const pair = right.right - left.left - COLUMN_GAP;
    // The divider's edge is where the left cell ends: the pointer is that edge.
    const shareAt = (x: number) => (x - 4 - left.left) / pair;
    const move = (e: globalThis.PointerEvent) => setDrag({ leftId: cells[index].id, share: shareAt(e.clientX) });
    const finish = (e: globalThis.PointerEvent) => {
      element.removeEventListener("pointermove", move);
      element.removeEventListener("pointerup", finish);
      element.removeEventListener("pointercancel", finish);
      setDrag(null);
      if (e.type === "pointerup") onResize(cells[index].id, shareAt(e.clientX));
    };
    element.addEventListener("pointermove", move);
    element.addEventListener("pointerup", finish);
    element.addEventListener("pointercancel", finish);
  };

  return (
    <div
      ref={ref}
      className={["columns", stacked ? "is-stacked" : "", drag ? "is-resizing" : ""].filter(Boolean).join(" ")}
      data-columns-of={frameId}
      style={{ "--depth-base": depth, "--frame-level": level, gridTemplateColumns: stacked ? undefined : tracks } as CSSProperties}
    >
      {cells.flatMap((cell, index) => [
        <div key={cell.id} className="columns__cell" data-column-cell={cell.id}>{cell.children}</div>,
        index < cells.length - 1 && !stacked ? (
          <div
            key={`${cell.id}-divider`}
            className="columns__divider"
            role="separator"
            aria-orientation="vertical"
            aria-label={t("Resize columns")}
            contentEditable={false}
            onPointerDown={(event) => start(event, index)}
          />
        ) : null,
      ])}
      {readOnly ? null : (
        <button
          type="button"
          className="columns__menu icon-btn"
          contentEditable={false}
          aria-label={t("Columns options")}
          aria-haspopup="menu"
          onClick={(event) => setMenu(rectOf(event.currentTarget))}
        >
          <MoreHorizontal size={14} />
        </button>
      )}
      <Popover open={menu !== null} anchor={menu} placement="bottom-end" label={t("Columns")} onDismiss={() => setMenu(null)}>
        <MenuList label={t("Columns")} autoFocus>
          <MenuItem icon={<Rows3 size={16} />} label={t("Turn into one column")} onSelect={() => { setMenu(null); onUnwrap(); }} />
          <MenuItem icon={<Trash2 size={16} />} label={t("Delete columns")} tone="danger" onSelect={() => { setMenu(null); onDelete(); }} />
        </MenuList>
      </Popover>
    </div>
  );
}

export { Columns2 as ColumnsIcon };
