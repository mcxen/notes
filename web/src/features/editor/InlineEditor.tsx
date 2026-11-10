import {
  memo,
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
  type ClipboardEvent,
  type FormEvent,
  type KeyboardEvent,
} from "react";
import { inlineFromText, inlinePlainText, inlineSignature, sameInlineContent, type DescribeMention } from "../../core/inline-content";
import type { Block, InlineContent } from "../../core/types";
import { editableHtml } from "./block-html";
import { blockDefinition } from "./block-registry";
import { highlighterReady, whenHighlighterReady } from "./code-highlight";
import { inlineFromNodes, keptOffset } from "./rich-text";
import { focusRange, selectionOffsets } from "./selection";

/** One `contenteditable` per block.
 *
 * A block without the caret is simply drawn from the model. A block WITH the
 * caret is left alone while its words and the model's agree — typing there is
 * what the model came from, and rewriting under a live caret would collapse
 * the selection on every keystroke. Words that differ came from elsewhere
 * (another person, another window) and are written in at once, the caret kept
 * where it was among the words around it; left out, the next keystroke would
 * have written the old words back over theirs. Nothing is rewritten while an
 * input method is mid-composition; once it lets go, marks the editor changed
 * meanwhile are drawn too (typing on after inline Markdown). When the editor
 * itself changes a focused block — applying a mark, collapsing inline
 * Markdown — it also re-writes the DOM and places the caret itself, after this.
 *
 * A code block is the exception to leaving a focused block alone: its colours
 * depend on the words around each one, so it is redrawn after every change
 * (and once an input method lets go), the caret put back at the same offset. */
export const InlineEditor = memo(function InlineEditor({
  block,
  readOnly,
  describe,
  onChange,
  onKeyDown,
  onPaste,
}: {
  block: Block;
  readOnly: boolean;
  /** How a mention reads now. Stable for the editor's life; a title that
   *  changes later is redrawn by the editor, attribute by attribute. */
  describe?: DescribeMention;
  /** `composing` while an input method has the text: the DOM is not drawn
   *  again until it lets go. */
  onChange(content: InlineContent, text: string, element: HTMLElement, composing: boolean): void;
  onKeyDown(event: KeyboardEvent<HTMLElement>): void;
  onPaste(event: ClipboardEvent<HTMLElement>): void;
}) {
  const ref = useRef<HTMLElement | null>(null);
  const composing = useRef(false);
  /** The HTML last written into a focused code block, to tell a redraw that
   *  is needed from one that is not. */
  const drawn = useRef("");
  const sync = useRef<(marks?: boolean) => void>(() => {});
  const definition = blockDefinition(block.kind);
  const Tag = definition.tag ?? "div";
  const code = block.kind === "code";
  const [coloured, setColoured] = useState(highlighterReady);

  useEffect(() => {
    if (!code || coloured) return;
    return whenHighlighterReady(() => setColoured(true));
  }, [code, coloured]);

  useLayoutEffect(() => {
    /* `marks`: the marks must agree as well as the words and mentions. */
    sync.current = (marks = false) => {
      const element = ref.current;
      if (!element) return;
      const html = editableHtml(block, describe);
      if (document.activeElement !== element) {
        if (element.innerHTML !== html) element.innerHTML = html;
        drawn.current = html;
        return;
      }
      if (composing.current) return;
      const showing = code ? null : inlineFromNodes(element);
      const shown = showing ? inlinePlainText(showing) : element.textContent ?? "";
      const wanted = inlinePlainText(block.content);
      // Words and mentions both: a mention swapped for another is a change
      // even though the text around it is the same.
      const agree = showing
        ? marks ? sameInlineContent(showing, block.content) : inlineSignature(showing) === inlineSignature(block.content)
        : html === drawn.current && shown === wanted;
      if (agree) return;
      const selection = selectionOffsets(element);
      element.innerHTML = html;
      drawn.current = html;
      if (selection) focusRange(element, keptOffset(shown, wanted, selection.start), keptOffset(shown, wanted, selection.end));
    };
    sync.current();
  }, [block, code, coloured, describe]);

  return (
    <Tag
      ref={(node: HTMLElement | null) => { ref.current = node; }}
      className="block__editable"
      contentEditable={!readOnly && definition.editable}
      suppressContentEditableWarning
      dir="ltr"
      role="textbox"
      aria-multiline="true"
      aria-label={definition.label}
      data-placeholder={definition.placeholder}
      spellCheck={!code}
      onInput={(event: FormEvent<HTMLElement>) => {
        const element = event.currentTarget;
        const text = element.textContent ?? "";
        // Code is its characters and nothing else: whatever markup a browser
        // leaves behind while typing, the colours are drawn again from these.
        onChange(code ? inlineFromText(text) : inlineFromNodes(element), text, element, composing.current);
      }}
      onKeyDown={onKeyDown}
      onPaste={onPaste}
      onCompositionStart={() => { composing.current = true; }}
      onCompositionEnd={() => {
        composing.current = false;
        if (code) {
          sync.current();
          return;
        }
        // Safari writes the committed text only after this event, so the
        // block is compared once the event is over.
        window.setTimeout(() => sync.current(true));
      }}
    />
  );
});
