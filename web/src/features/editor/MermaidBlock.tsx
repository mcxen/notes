import { useEffect, useRef, useState, type MouseEvent, type ReactNode } from "react";
import { t } from "../../i18n";
import { currentLook, DIAGRAM_SOURCE_LIMIT, renderDiagram, type Diagram } from "./mermaid-render";
import { focusAtEnd } from "./selection";

/** A code block of Mermaid, drawn as a diagram. The source is the block's
 *  words, edited in the monospace area (`children`) like any code; the area
 *  stays mounted — clipped, so the caret and the selection still count its
 *  characters — until the caret is in it, and the diagram is drawn under it,
 *  live as the source is typed. A source Mermaid cannot read stays on view with
 *  why under it, so the page is never a diagram that says nothing. */
export function MermaidBlock({
  source,
  readOnly,
  header,
  children,
}: {
  source: string;
  readOnly: boolean;
  header: ReactNode;
  children: ReactNode;
}) {
  const ref = useRef<HTMLDivElement | null>(null);
  const [diagram, setDiagram] = useState<Diagram | null>(null);
  /** Bumped when the page changes theme, so the diagram is drawn again in its colours. */
  const [look, setLook] = useState(0);
  const empty = !source.trim();

  useEffect(() => {
    const watch = new MutationObserver(() => setLook((n) => n + 1));
    watch.observe(document.documentElement, { attributes: true, attributeFilter: ["data-theme"] });
    return () => watch.disconnect();
  }, []);

  useEffect(() => {
    if (empty) {
      setDiagram(null);
      return;
    }
    let gone = false;
    // Not on every key: a diagram drawn for each letter of a long source is a lot of drawing.
    const wait = setTimeout(() => {
      void renderDiagram(source, currentLook()).then((drawn) => {
        if (!gone) setDiagram(drawn);
      });
    }, 250);
    return () => {
      gone = true;
      clearTimeout(wait);
    };
  }, [source, empty, look]);

  const ready = diagram !== null && "svg" in diagram;
  const state = empty ? "empty" : diagram === null ? "pending" : ready ? "ready" : "error";

  const edit = (event: MouseEvent<HTMLElement>) => {
    if (readOnly) return;
    const editor = ref.current?.querySelector(".block__editable");
    if (!(editor instanceof HTMLElement) || !editor.isContentEditable) return;
    event.preventDefault();
    focusAtEnd(editor);
  };

  return (
    <div ref={ref} className="block__code block__diagram" data-state={state}>
      {header}
      {children}
      {diagram && "svg" in diagram ? (
        <div className="block__diagram-view" contentEditable={false} aria-label={t("Diagram")} onMouseDown={edit} dangerouslySetInnerHTML={{ __html: diagram.svg }} />
      ) : null}
      {diagram && "error" in diagram ? (
        <div className="block__diagram-error" contentEditable={false} role="alert">
          {diagram.error === "too long" ? t("This diagram is over {n} characters, so it is not drawn.", { n: DIAGRAM_SOURCE_LIMIT }) : diagram.error}
        </div>
      ) : null}
    </div>
  );
}
