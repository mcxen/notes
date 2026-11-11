import { Baseline, ChevronDown, Copy, ImageDown, Trash2, X } from "lucide-react";
import { useRef, useState } from "react";
import { Tooltip } from "../../components/Tooltip";
import { MenuItem, MenuList } from "../../components/Menu";
import { Popover } from "../../components/Popover";
import { rectOf } from "../../hooks/useAnchoredPosition";
import type { ToggleableMark } from "../../core/inline-content";
import type { BlockKind, ContentColor } from "../../core/types";
import { t, tp } from "../../i18n";
import { blockDefinition } from "./block-registry";
import { ColorMenu } from "./ColorMenu";
import { MARKS } from "./marks";

/** What a line can be turned into, in the order of the block menu's. */
const TURN_INTO: BlockKind[] = [
  "text", "heading1", "heading2", "heading3", "todo", "bullet", "numbered", "toggle",
  "quote", "callout", "code", "divider",
];

/** What can be done with paragraphs selected as blocks — several of them,
 *  since one has its handle's menu. It sits at the foot of the page, out of
 *  the way of the text, and keeps the selection when it is pressed. Besides
 *  copying and deleting, it styles them all at once: what they are (a heading,
 *  a list…), their colour, and bold, italic and the rest across all their
 *  words. */
export function SelectionBar({
  count,
  readOnly,
  marked,
  styled,
  onCopy,
  onCopyImage,
  onDelete,
  onClear,
  onMark,
  onTurnInto,
  onColor,
}: {
  count: number;
  readOnly: boolean;
  /** The marks that are on in every word of the selection. */
  marked: ReadonlySet<ToggleableMark>;
  /** What the lines are, when they are all the one kind, and their colour. */
  styled: { kind?: BlockKind; color?: ContentColor };
  onCopy(): void;
  onCopyImage(): void;
  onDelete(): void;
  onClear(): void;
  onMark(mark: ToggleableMark): void;
  onTurnInto(kind: BlockKind): void;
  onColor(color: ContentColor | undefined): void;
}) {
  const turnRef = useRef<HTMLButtonElement | null>(null);
  const colorRef = useRef<HTMLButtonElement | null>(null);
  const [menu, setMenu] = useState<"turn" | "color" | null>(null);
  const toggleMenu = (which: "turn" | "color") => setMenu((current) => (current === which ? null : which));
  return (
    <div className="selection-bar" role="toolbar" aria-label={t("Selected paragraphs")} onMouseDown={(event) => event.preventDefault()}>
      <span className="selection-bar__count">{tp(count, "{n} paragraph", "{n} paragraphs")}</span>
      {readOnly ? null : (
        <>
          <button
            type="button"
            ref={turnRef}
            className={`selection-bar__btn ${menu === "turn" ? "is-on" : ""}`}
            aria-label={t("Turn into")}
            aria-haspopup="menu"
            aria-expanded={menu === "turn"}
            onClick={() => toggleMenu("turn")}
          >
            <span>{styled.kind ? t(blockDefinition(styled.kind).label) : t("Turn into")}</span>
            <ChevronDown size={13} />
          </button>
          {MARKS.map(({ mark, label, icon: Icon, key, shift }) => (
            <Tooltip key={mark} label={t(label)} hint={`⌘${shift ? "⇧" : ""}${key.toUpperCase()}`} placement="top-start">
              <button
                type="button"
                className={`selection-bar__btn selection-bar__btn--icon ${marked.has(mark) ? "is-on" : ""}`}
                aria-label={t(label)}
                aria-pressed={marked.has(mark)}
                onClick={() => onMark(mark)}
              >
                <Icon size={15} />
              </button>
            </Tooltip>
          ))}
          <Tooltip label={t("Color")} placement="top-start">
            <button
              type="button"
              ref={colorRef}
              className={`selection-bar__btn selection-bar__btn--icon format-bar__color ${styled.color || menu === "color" ? "is-on" : ""}`}
              data-color={styled.color}
              aria-label={t("Color")}
              aria-haspopup="menu"
              aria-expanded={menu === "color"}
              onClick={() => toggleMenu("color")}
            >
              <Baseline size={15} />
            </button>
          </Tooltip>
          <span className="selection-bar__rule" aria-hidden="true" />
        </>
      )}
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

      <Popover
        open={menu === "turn"}
        anchor={turnRef.current ? rectOf(turnRef.current) : null}
        placement="top-start"
        label={t("Turn into")}
        group="selection-bar"
        onDismiss={() => setMenu(null)}
      >
        <MenuList label={t("Turn into")} autoFocus>
          {TURN_INTO.map((kind) => {
            const definition = blockDefinition(kind);
            const Icon = definition.icon;
            return (
              <MenuItem
                key={kind}
                icon={<Icon size={16} />}
                label={t(definition.label)}
                selected={kind === styled.kind}
                onSelect={() => { setMenu(null); onTurnInto(kind); }}
              />
            );
          })}
        </MenuList>
      </Popover>

      <Popover
        open={menu === "color"}
        anchor={colorRef.current ? rectOf(colorRef.current) : null}
        placement="top-start"
        label={t("Color")}
        group="selection-bar"
        onDismiss={() => setMenu(null)}
      >
        <ColorMenu current={styled.color} onPick={(color) => { setMenu(null); onColor(color); }} />
      </Popover>
    </div>
  );
}
