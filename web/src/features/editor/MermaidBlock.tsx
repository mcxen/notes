import { Copy, Download, FileCode, Maximize2, Pencil } from "lucide-react";
import { useEffect, useMemo, useRef, useState, type KeyboardEvent, type ReactNode } from "react";
import { useUi } from "../../app/ui-context";
import { t } from "../../i18n";
import { saveBlob, standaloneSvg, svgBlob, svgToPng } from "./diagram-export";
import { ZoomViewer } from "./MediaBlocks";
import { currentLook, DIAGRAM_SOURCE_LIMIT, renderDiagram, type Diagram } from "./mermaid-render";
import { writePngToClipboard } from "./copy-image";
import { focusAtEnd } from "./selection";

/** A code block of Mermaid, drawn as a diagram. The source is the block's
 *  words, edited in the monospace area (`children`) like any code; the area
 *  stays mounted — clipped, so the caret and the selection still count its
 *  characters — until the caret is in it, and the diagram is drawn under it,
 *  live as the source is typed. A source Mermaid cannot read stays on view with
 *  why under it, so the page is never a diagram that says nothing.
 *
 *  Pressing the diagram shows it larger, to zoom and move about in; the buttons
 *  on it edit the source, copy the diagram as a picture, or save it as a PNG
 *  or SVG. */
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
  const { toast } = useUi();
  const [diagram, setDiagram] = useState<Diagram | null>(null);
  /** Bumped when the page changes theme, so the diagram is drawn again in its colours. */
  const [look, setLook] = useState(0);
  const [larger, setLarger] = useState(false);
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

  const svg = diagram && "svg" in diagram ? diagram.svg : null;
  const state = empty ? "empty" : diagram === null ? "pending" : svg ? "ready" : "error";

  const edit = () => {
    if (readOnly) return;
    const editor = ref.current?.querySelector(".block__editable");
    if (editor instanceof HTMLElement && editor.isContentEditable) focusAtEnd(editor);
  };

  /** The diagram on the page's paper, at twice its size so zooming in stays sharp. */
  const shown = useMemo(() => {
    if (!svg || !larger) return null;
    return URL.createObjectURL(svgBlob(standaloneSvg(svg, { scale: 2, background: currentLook().color("--paper") })));
  }, [svg, larger]);
  useEffect(() => () => { if (shown) URL.revokeObjectURL(shown); }, [shown]);

  const paper = () => currentLook().color("--paper");
  const failed = () => toast(t("Could not save the diagram"), "error");
  const saveSvg = () => {
    if (svg) saveBlob(svgBlob(standaloneSvg(svg, { background: paper() })), "diagram.svg");
  };
  const savePng = () => {
    if (svg) void svgToPng(svg, paper()).then((png) => saveBlob(png, "diagram.png")).catch(failed);
  };
  const copyPng = () => {
    if (!svg) return;
    void svgToPng(svg, paper())
      .then(writePngToClipboard)
      .then(() => toast(t("Diagram copied"), "success"))
      .catch(() => toast(t("Could not copy the diagram"), "error"));
  };

  const keyOpen = (event: KeyboardEvent) => {
    if (event.key === "Enter" || event.key === " ") {
      event.preventDefault();
      setLarger(true);
    }
  };

  return (
    <div ref={ref} className="block__code block__diagram" data-state={state}>
      {header}
      {children}
      {svg ? (
        <div className="block__diagram-stage" contentEditable={false}>
          <div
            className="block__diagram-view"
            role="button"
            tabIndex={0}
            aria-label={t("View diagram larger")}
            title={t("View larger")}
            // A press is not a click into the text: the caret would move into the
            // source, which opens up and moves the diagram from under the pointer.
            onMouseDown={(event) => event.preventDefault()}
            onClick={() => setLarger(true)}
            onKeyDown={keyOpen}
            dangerouslySetInnerHTML={{ __html: svg }}
          />
          <div className="block__diagram-tools" role="toolbar" aria-label={t("Diagram")}>
            <button type="button" className="icon-btn" aria-label={t("View larger")} title={t("View larger")} onClick={() => setLarger(true)}><Maximize2 size={14} /></button>
            {readOnly ? null : <button type="button" className="icon-btn" aria-label={t("Edit diagram")} title={t("Edit diagram")} onClick={edit}><Pencil size={14} /></button>}
            <button type="button" className="icon-btn" aria-label={t("Copy as image")} title={t("Copy as image")} onClick={copyPng}><Copy size={14} /></button>
            <button type="button" className="icon-btn" aria-label={t("Save as PNG")} title={t("Save as PNG")} onClick={savePng}><Download size={14} /></button>
            <button type="button" className="icon-btn" aria-label={t("Save as SVG")} title={t("Save as SVG")} onClick={saveSvg}><FileCode size={14} /></button>
          </div>
        </div>
      ) : null}
      {diagram && "error" in diagram ? (
        <div className="block__diagram-error" contentEditable={false} role="alert">
          {diagram.error === "too long" ? t("This diagram is over {n} characters, so it is not drawn.", { n: DIAGRAM_SOURCE_LIMIT }) : diagram.error}
        </div>
      ) : null}
      {larger && shown ? (
        <ZoomViewer
          name={t("Diagram")}
          url={shown}
          onClose={() => setLarger(false)}
          tools={
            <>
              <button type="button" className="icon-btn" aria-label={t("Copy as image")} title={t("Copy as image")} onClick={copyPng}><Copy size={16} /></button>
              <button type="button" className="icon-btn" aria-label={t("Save as SVG")} title={t("Save as SVG")} onClick={saveSvg}><FileCode size={16} /></button>
            </>
          }
          onDownload={savePng}
        />
      ) : null}
    </div>
  );
}
