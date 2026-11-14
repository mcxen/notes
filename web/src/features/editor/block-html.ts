import { escapeHtml, inlinePlainText, inlineToHtml, type DescribeMention } from "../../core/inline-content";
import type { Block } from "../../core/types";
import { blockDefinition } from "./block-registry";
import { codeHtml } from "./code-highlight";

/** What a block's `contenteditable` holds. Every writer of a block's DOM goes
 *  through here, so a code block is always drawn coloured, every block's line
 *  breaks are real newlines, and a block's text in the DOM is always its text
 *  in the model. */
export function editableHtml(block: Block, describe?: DescribeMention): string {
  if (!blockDefinition(block.kind).editable) return "";
  if (block.kind === "code") return codeHtml(inlinePlainText(block.content), block.language);
  // An equation's source is plain TeX, not coloured and not marked. A final
  // newline gets a `<br>` the same way code does, so the empty line can take
  // the caret.
  if (block.kind === "math") {
    const text = inlinePlainText(block.content);
    if (!text) return "";
    const html = escapeHtml(text);
    return text.endsWith("\n") ? `${html}<br>` : html;
  }
  // As in code: a final newline gets a `<br>` after it, which no text offset
  // sees and reading the block back drops, so the empty line it starts is
  // drawn and can take the caret.
  const html = inlineToHtml(block.content, describe, true);
  return inlinePlainText(block.content).endsWith("\n") ? `${html}<br>` : html;
}
