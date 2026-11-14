import { useEffect, useState, type MouseEvent, type ReactNode } from "react";
import { escapeHtml } from "../../core/inline-content";
import { t } from "../../i18n";
import { loadKatex, MATH_SOURCE_LIMIT, renderMath } from "./math-render";
import { focusAtEnd } from "./selection";

/** A display equation. The TeX source is the block's words, edited in the
 *  monospace area (`children`) the way a code block is. Until that area has
 *  the caret the source is typeset; the area stays mounted so the caret,
 *  selection and shortcuts can reach it, and CSS shows only one of the two. */
export function MathBlock({
  source,
  readOnly,
  children,
}: {
  source: string;
  readOnly: boolean;
  children: ReactNode;
}) {
  const placeholder = t("Add a TeX equation");
  const label = t("Equation");
  const empty = !source.trim();
  const over = source.length > MATH_SOURCE_LIMIT;
  const [html, setHtml] = useState<string | null>(null);

  useEffect(() => {
    if (empty || over) return;
    let gone = false;
    setHtml(null);
    loadKatex()
      .then((katex) => {
        if (!gone) setHtml(renderMath(katex, source).html);
      })
      .catch(() => {
        // The chunk did not arrive: the source stays, the page does not go blank.
        if (!gone) setHtml(`<span class="katex-error">${escapeHtml(source)}</span>`);
      });
    return () => { gone = true; };
  }, [source, empty, over]);

  const edit = (event: MouseEvent<HTMLElement>) => {
    if (readOnly) return;
    const editor = event.currentTarget.parentElement?.querySelector(".block__editable");
    if (!(editor instanceof HTMLElement) || !editor.isContentEditable) return;
    // The preview is not the source: the caret goes to the end of the TeX.
    event.preventDefault();
    focusAtEnd(editor);
  };

  let view: ReactNode;
  if (empty) {
    view = (
      <div className="block__math-view block__math-placeholder" contentEditable={false} aria-label={label} onMouseDown={edit}>
        {placeholder}
      </div>
    );
  } else if (over || html === null) {
    view = (
      <div
        className={`block__math-view ${over ? "block__math-error" : "block__math-pending"}`}
        contentEditable={false}
        aria-label={label}
        onMouseDown={edit}
      >
        {source}
      </div>
    );
  } else {
    view = (
      <div
        className="block__math-view"
        contentEditable={false}
        aria-label={label}
        onMouseDown={edit}
        dangerouslySetInnerHTML={{ __html: html }}
      />
    );
  }

  return (
    <div className="block__math">
      {view}
      {readOnly ? null : children}
    </div>
  );
}
