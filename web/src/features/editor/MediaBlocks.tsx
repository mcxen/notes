import { ChevronLeft, ChevronRight, Download, Maximize, Paperclip, Replace, RotateCw, X, ZoomIn, ZoomOut } from "lucide-react";
import { useCallback, useEffect, useRef, useState, type PointerEvent as ReactPointerEvent } from "react";
import { formatFileSize, normalizeStoredFile } from "../../core/files";
import type { Block, StoredFile } from "../../core/types";
import { Modal } from "../../components/Modal";
import { peekFileUrl, type ShownFile } from "./file-store";
import { actualSize, FIT, limitsFor, percentOf, wheelFactor, zoomAbout, type Zoom } from "./viewer-zoom";
import { t } from "../../i18n";

export type UploadState = "uploading" | "failed";

/** What a file block asks of its editor. */
export interface MediaActions {
  /** The file's bytes on screen, held for the view until it lets go. */
  showFile(file: StoredFile, which: "page" | "original"): ShownFile;
  pickFile(block: Block): void;
  downloadFile(block: Block): void;
  retryUpload(block: Block): void;
}

/** The picture's bytes as a URL: at once when this session has them, else
 *  once they are read. `failed` after the reads have given up. */
export function useFileUrl(file: StoredFile | undefined, which: "page" | "original", actions: Pick<MediaActions, "showFile">) {
  const [state, setState] = useState<{ key: string; url: string | null; failed: boolean }>({ key: "", url: null, failed: false });
  const [attempt, setAttempt] = useState(0);
  const key = file ? `${file.space ?? ""}:${file.owner}:${file.path}:${which === "page" ? file.preview ?? "" : ""}:${attempt}` : "";
  const peeked = file ? peekFileUrl(file, which) : null;
  useEffect(() => {
    if (!file) return;
    const shown = actions.showFile(file, which);
    let live = true;
    shown.ready
      .then((url) => { if (live) setState({ key, url, failed: false }); })
      .catch(() => { if (live) setState({ key, url: null, failed: true }); });
    return () => {
      live = false;
      shown.release();
    };
    // `key` names everything about the file that decides what is read.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key]);
  const current = state.key === key ? state : { url: null, failed: false };
  return {
    url: current.url ?? peeked,
    failed: current.failed && !peeked,
    retry: () => setAttempt((count) => count + 1),
  };
}

/** Notion's two edge handles, for a picture or a card: a drag on either one
 *  sizes `figure` — about its centre when its column centres it, else from
 *  the edge it stands against — drawn as the pointer moves, written
 *  once, when it is let go — between `least` and `most` (and never past its
 *  column, the figure's parent). `done` gets the width, or `undefined` at
 *  the size the figure has with none (`natural`, from the column's width),
 *  so a figure dragged back to its own size keeps no number. The pointer is
 *  the handle's for the whole drag, and a frame inside the figure (a
 *  player) takes none of it while `is-resizing`. */
export function dragEdge(
  event: ReactPointerEvent<HTMLElement>,
  side: "left" | "right",
  figure: HTMLElement | null,
  { least, most = Infinity, natural, done }: { least: number; most?: number; natural(column: number): number; done(width: number | undefined): void },
) {
  const column = figure?.parentElement;
  if (!figure || !column || event.button !== 0) return;
  event.preventDefault();
  event.stopPropagation();
  const handle = event.currentTarget;
  const pointer = event.pointerId;
  try {
    handle.setPointerCapture(pointer);
  } catch {
    // A pointer the browser no longer has: the window's listeners serve.
  }
  const startX = event.clientX;
  const startWidth = figure.getBoundingClientRect().width;
  const columnWidth = column.getBoundingClientRect().width;
  const widest = Math.min(most, columnWidth);
  const narrowest = Math.min(least, widest);
  const own = Math.min(natural(columnWidth), widest);
  const centred = getComputedStyle(column).alignItems === "center";
  let next = startWidth;
  figure.classList.add("is-resizing");
  const move = (moveEvent: PointerEvent) => {
    const delta = (moveEvent.clientX - startX) * (side === "right" ? 1 : -1) * (centred ? 2 : 1);
    next = Math.round(Math.min(widest, Math.max(narrowest, startWidth + delta)));
    figure.style.width = `${next}px`;
  };
  const finish = () => {
    window.removeEventListener("pointermove", move);
    window.removeEventListener("pointerup", finish);
    window.removeEventListener("pointercancel", finish);
    try {
      handle.releasePointerCapture(pointer);
    } catch {
      // Already let go.
    }
    figure.classList.remove("is-resizing");
    if (Math.abs(next - startWidth) >= 1) done(Math.abs(next - own) <= 2 ? undefined : next);
  };
  window.addEventListener("pointermove", move);
  window.addEventListener("pointerup", finish);
  window.addEventListener("pointercancel", finish);
}

/** Notion's empty file block: a grey bar that asks for the file. */
function EmptyMedia({ block, readOnly, actions }: { block: Block; readOnly: boolean; actions: MediaActions }) {
  return (
    <button type="button" className="block__media-empty" disabled={readOnly} onClick={() => actions.pickFile(block)}>
      <Paperclip size={18} />
      <span>{t("Add a file")}</span>
    </button>
  );
}

/** A file block: its name and size on one quiet row; clicking saves it. */
export function FileBlock({
  block,
  readOnly,
  upload,
  actions,
}: {
  block: Block;
  readOnly: boolean;
  upload?: UploadState;
  actions: MediaActions;
}) {
  const file = normalizeStoredFile(block.file);
  if (!file) return <EmptyMedia block={block} readOnly={readOnly} actions={actions} />;
  return (
    <div className="block__file" contentEditable={false}>
      <button type="button" className="block__file-open" title={`Download ${file.name}`} onClick={() => actions.downloadFile(block)}>
        <Paperclip size={16} className="block__file-icon" />
        <span className="block__file-name">{file.name}</span>
        <span className="block__file-size">{formatFileSize(file.size)}</span>
      </button>
      {readOnly || upload ? null : (
        <button type="button" className="icon-btn block__file-replace" aria-label={t("Replace file")} onClick={() => actions.pickFile(block)}>
          <Replace size={14} />
        </button>
      )}
      {upload === "uploading" ? <span className="block__file-status" role="status"><span className="spinner" aria-hidden="true" />{t("Uploading…")}</span> : null}
      {upload === "failed" ? (
        <span className="block__file-status is-failed" role="alert">
          {t("Upload failed")}
          <button type="button" className="btn" onClick={() => actions.retryUpload(block)}><RotateCw size={13} />{t("Try again")}</button>
        </span>
      ) : null}
    </div>
  );
}

/** A picture at full size over the page — the original, not the page copy. */
/** Pictures looked through in turn — a cell's: where this one stands among
 *  them, and a step to the one before or after. */
export interface ViewerBrowse {
  index: number;
  count: number;
  onStep(delta: -1 | 1): void;
}

export function ImageViewer({
  file,
  actions,
  browse,
  onDownload,
  onClose,
}: {
  file: StoredFile;
  actions: Pick<MediaActions, "showFile">;
  browse?: ViewerBrowse;
  onDownload(): void;
  onClose(): void;
}) {
  const { url, failed } = useFileUrl(file, "original", actions);
  const stage = useRef<HTMLDivElement | null>(null);
  const image = useRef<HTMLImageElement | null>(null);
  const [zoom, setZoom] = useState<Zoom>(FIT);
  // The picture fitted in the window, and its own size: what 100% is measured against.
  const [size, setSize] = useState({ fit: 0, natural: 0 });
  const dragged = useRef(false);
  const measure = useCallback(() => {
    const element = image.current;
    if (!element || !element.naturalWidth) return;
    // The fitted width is the layout's, whatever the zoom transform is now doing.
    setSize({ fit: element.offsetWidth, natural: element.naturalWidth });
  }, []);
  // Another picture (← →) starts fitted.
  useEffect(() => { setZoom(FIT); }, [file.path]);
  useEffect(() => {
    window.addEventListener("resize", measure);
    return () => window.removeEventListener("resize", measure);
  }, [measure]);
  const limits = limitsFor(size.fit, size.natural);
  const centred = (clientX: number, clientY: number) => {
    const box = stage.current?.getBoundingClientRect();
    return box ? { x: clientX - (box.left + box.width / 2), y: clientY - (box.top + box.height / 2) } : { x: 0, y: 0 };
  };
  const zoomBy = (factor: number, point = { x: 0, y: 0 }) => setZoom((current) => zoomAbout(current, factor, point, limits));
  const toActual = () => {
    const target = actualSize(size.fit, size.natural);
    setZoom((current) => (Math.abs(current.scale - target) < 0.01 ? FIT : zoomAbout(current, target / current.scale, { x: 0, y: 0 }, limits)));
  };
  // The wheel and a trackpad's pinch zoom about the pointer; it needs a listener
  // that may stop the page scrolling, which React's onWheel may not.
  useEffect(() => {
    const element = stage.current;
    if (!element) return;
    const onWheel = (event: WheelEvent) => {
      event.preventDefault();
      zoomBy(wheelFactor(event.deltaY, event.ctrlKey), centred(event.clientX, event.clientY));
    };
    element.addEventListener("wheel", onWheel, { passive: false });
    return () => element.removeEventListener("wheel", onWheel);
  });
  // A press and a drag pan a picture that is zoomed.
  const startPan = (event: ReactPointerEvent<HTMLImageElement>) => {
    dragged.current = false;
    if (zoom.scale <= 1 || event.button !== 0) return;
    event.preventDefault();
    const start = { x: event.clientX, y: event.clientY, zoom };
    const onMove = (move: PointerEvent) => {
      const dx = move.clientX - start.x;
      const dy = move.clientY - start.y;
      if (Math.abs(dx) + Math.abs(dy) > 3) dragged.current = true;
      setZoom({ ...start.zoom, x: start.zoom.x + dx, y: start.zoom.y + dy });
    };
    const onUp = () => {
      window.removeEventListener("pointermove", onMove);
      window.removeEventListener("pointerup", onUp);
    };
    window.addEventListener("pointermove", onMove);
    window.addEventListener("pointerup", onUp);
  };
  const first = !browse || browse.index <= 0;
  const last = !browse || browse.index >= browse.count - 1;
  /* ← and → step through them, as Notion's viewer does; at either end there
     is nowhere further to go. */
  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      // + and − zoom, 0 fits, 1 shows the picture's own pixels.
      if (!event.metaKey && !event.ctrlKey && !event.altKey) {
        if (event.key === "+" || event.key === "=") { event.preventDefault(); event.stopPropagation(); zoomBy(1.25); return; }
        if (event.key === "-" || event.key === "_") { event.preventDefault(); event.stopPropagation(); zoomBy(0.8); return; }
        if (event.key === "0") { event.preventDefault(); event.stopPropagation(); setZoom(FIT); return; }
        if (event.key === "1") { event.preventDefault(); event.stopPropagation(); toActual(); return; }
      }
      if (!browse) return;
      if (event.key !== "ArrowLeft" && event.key !== "ArrowRight") return;
      event.preventDefault();
      event.stopPropagation();
      const back = event.key === "ArrowLeft";
      if (back ? browse.index > 0 : browse.index < browse.count - 1) browse.onStep(back ? -1 : 1);
    };
    window.addEventListener("keydown", onKeyDown, true);
    return () => window.removeEventListener("keydown", onKeyDown, true);
  });
  return (
    <Modal open label={file.name} className="viewer" onClose={onClose}>
      <div className="viewer__bar">
        <span className="viewer__name">{file.name}</span>
        {browse && browse.count > 1 ? <span className="viewer__count">{browse.index + 1} of {browse.count}</span> : null}
        <button type="button" className="icon-btn" aria-label={t("Download")} onClick={onDownload}><Download size={16} /></button>
        <button type="button" className="icon-btn" aria-label={t("Close")} onClick={onClose}><X size={16} /></button>
      </div>
      <div className="viewer__stage" ref={stage} onClick={() => { if (!dragged.current) onClose(); dragged.current = false; }}>
        {url ? (
          <img
            ref={image}
            src={url}
            alt={file.name}
            draggable={false}
            className={zoom.scale > 1 ? "is-zoomed" : undefined}
            style={{ transform: `translate(${zoom.x}px, ${zoom.y}px) scale(${zoom.scale})` }}
            onLoad={measure}
            onPointerDown={startPan}
            onDoubleClick={(event) => { event.stopPropagation(); toActual(); }}
            onClick={(event) => event.stopPropagation()}
          />
        ) : failed ? (
          <p className="viewer__missing">{t("This image could not be loaded.")}</p>
        ) : (
          <span className="spinner" aria-label={t("Loading")} />
        )}
        {browse && browse.count > 1 ? (
          <>
            <button
              type="button"
              className="viewer__step viewer__step--back"
              aria-label={t("Previous image")}
              disabled={first}
              onClick={(event) => { event.stopPropagation(); browse.onStep(-1); }}
            >
              <ChevronLeft size={20} />
            </button>
            <button
              type="button"
              className="viewer__step viewer__step--next"
              aria-label={t("Next image")}
              disabled={last}
              onClick={(event) => { event.stopPropagation(); browse.onStep(1); }}
            >
              <ChevronRight size={20} />
            </button>
          </>
        ) : null}
        {url ? (
          <div className="viewer__tools" role="toolbar" aria-label={t("Zoom")} onClick={(event) => event.stopPropagation()}>
            <button type="button" className="viewer__tool" aria-label={t("Zoom out")} title={`${t("Zoom out")}  −`} onClick={() => zoomBy(0.8)}><ZoomOut size={16} /></button>
            <span className="viewer__zoom" aria-live="polite">{percentOf(zoom.scale, size.fit, size.natural)}%</span>
            <button type="button" className="viewer__tool" aria-label={t("Zoom in")} title={`${t("Zoom in")}  +`} onClick={() => zoomBy(1.25)}><ZoomIn size={16} /></button>
            <span className="viewer__rule" aria-hidden="true" />
            <button type="button" className="viewer__tool" aria-label={t("Fit to window")} title={`${t("Fit to window")}  0`} onClick={() => setZoom(FIT)}><Maximize size={16} /></button>
            <button type="button" className="viewer__tool viewer__tool--text" aria-label={t("Actual size")} title={`${t("Actual size")}  1`} onClick={toActual}>1:1</button>
          </div>
        ) : null}
      </div>
    </Modal>
  );
}
