import { Download, X } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { Modal } from "../../components/Modal";
import { useUi } from "../../app/ui-context";
import { useWorkspace } from "../../app/workspace-context";
import { pageName } from "../../core/block-tree";
import type { NotePage, StoredFile } from "../../core/types";
import { useWorkspaceState } from "../../app/workspace-store";
import { t, tp } from "../../i18n";
import { bytesOf } from "../editor/file-store";
import { buildExport, planExport, type ExportFormat, type ExportOptions } from "./build";
import { saveBytes } from "./save";

const STORED = "notes-export-options";

function stored(): ExportOptions {
  const fallback: ExportOptions = { format: "markdown", subpages: true, files: true };
  try {
    const value = JSON.parse(localStorage.getItem(STORED) ?? "null") as Partial<ExportOptions> | null;
    if (!value) return fallback;
    return {
      format: value.format === "html" ? "html" : "markdown",
      subpages: value.subpages !== false,
      files: value.files !== false,
    };
  } catch {
    return fallback;
  }
}

function Switch({ on, onChange, label, hint }: { on: boolean; onChange(on: boolean): void; label: string; hint?: string }) {
  return (
    <button type="button" className="export__row" role="switch" aria-checked={on} onClick={() => onChange(!on)}>
      <span className="export__copy">
        <span className="export__label">{label}</span>
        {hint ? <span className="export__hint">{hint}</span> : null}
      </span>
      <span className={`switch ${on ? "is-on" : ""}`}><span className="switch__knob" /></span>
    </button>
  );
}

/** Export a page: its format, whether the pages under it and its pictures and files
 *  go with it, as Notion's export asks. One page without pictures is one file; any
 *  more is a zip, the pages in folders and the files in `assets/`. */
export function ExportDialog({ page, onClose }: { page: NotePage; onClose(): void }) {
  const { platform } = useWorkspace();
  const { toast } = useUi();
  const pages = useWorkspaceState((state) => state.pages);
  const [options, setOptions] = useState<ExportOptions>(stored);
  const [busy, setBusy] = useState(false);
  const titleOf = useMemo(() => (id: string) => (pages[id] ? pageName(pages[id]) : ""), [pages]);

  // What there is to take along, for the dialog to offer only what exists.
  const everything = useMemo(() => planExport(pages, page.id, { subpages: true }, titleOf), [page.id, pages, titleOf]);
  const chosen = useMemo(() => planExport(pages, page.id, { subpages: options.subpages }, titleOf), [options.subpages, page.id, pages, titleOf]);
  const hasSubpages = everything.pageIds.length > 1;
  const hasFiles = everything.files.length > 0;
  const set = (patch: Partial<ExportOptions>) => setOptions((current) => ({ ...current, ...patch }));
  useEffect(() => { try { localStorage.setItem(STORED, JSON.stringify(options)); } catch { /* not kept */ } }, [options]);

  const zipped = (options.subpages && chosen.pageIds.length > 1) || (options.files && chosen.files.length > 0);
  const summary = zipped
    ? t("A .zip of {pages}{files}.", {
      pages: tp(options.subpages ? chosen.pageIds.length : 1, "{n} page", "{n} pages"),
      files: options.files && chosen.files.length ? `, ${tp(chosen.files.length, "{n} file", "{n} files")}` : "",
    })
    : t("One {format} file.", { format: options.format === "markdown" ? "Markdown" : "HTML" });

  const run = async () => {
    if (!platform || busy) return;
    setBusy(true);
    try {
      const result = await buildExport({
        pages,
        rootId: page.id,
        options: { ...options, subpages: options.subpages && hasSubpages, files: options.files && hasFiles },
        titleOf,
        lang: document.documentElement.lang || "en",
        readFile: async (file: StoredFile) => {
          try { return new Uint8Array(await (await bytesOf(platform.files, file, file.path)).arrayBuffer()); } catch { return null; }
        },
      });
      saveBytes(result.name, result.bytes, result.mime);
      toast(t("Exported {name}", { name: result.name }), "success");
      onClose();
    } catch {
      toast(t("The export failed."), "error");
      setBusy(false);
    }
  };

  const FORMATS: Array<{ value: ExportFormat; label: string }> = [{ value: "markdown", label: "Markdown" }, { value: "html", label: "HTML" }];
  return (
    <Modal open label={t("Export")} className="export" onClose={onClose}>
      <div className="share__head">
        <h2 className="share__title">{t("Export")}</h2>
        <button type="button" className="icon-btn" aria-label={t("Close")} onClick={onClose}><X size={16} /></button>
      </div>
      <div className="export__body">
        <div className="export__row export__row--static">
          <span className="export__copy"><span className="export__label">{t("Format")}</span></span>
          <span className="export__formats" role="radiogroup" aria-label={t("Format")}>
            {FORMATS.map((format) => (
              <button
                key={format.value}
                type="button"
                role="radio"
                aria-checked={options.format === format.value}
                className={`export__format ${options.format === format.value ? "is-on" : ""}`}
                onClick={() => set({ format: format.value })}
              >
                {format.label}
              </button>
            ))}
          </span>
        </div>
        {hasSubpages ? (
          <Switch
            on={options.subpages}
            onChange={(subpages) => set({ subpages })}
            label={t("Include sub-pages")}
            hint={tp(everything.pageIds.length - 1, "{n} sub-page, kept in a folder", "{n} sub-pages, kept in folders")}
          />
        ) : null}
        {hasFiles ? (
          <Switch
            on={options.files}
            onChange={(files) => set({ files })}
            label={t("Include images and files")}
            hint={t("Kept in an assets folder and linked from the pages")}
          />
        ) : null}
        <p className="export__summary">{summary}</p>
      </div>
      <div className="export__foot">
        <button type="button" className="btn btn--outline" onClick={onClose}>{t("Cancel")}</button>
        <button type="button" className="btn btn--primary" disabled={busy || !platform} onClick={() => void run()}>
          <Download size={14} />{busy ? t("Exporting…") : t("Export")}
        </button>
      </div>
    </Modal>
  );
}
