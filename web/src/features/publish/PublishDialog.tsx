import { Check, Copy, ExternalLink, Globe, X } from "lucide-react";
import { useMemo, useState } from "react";
import { Modal } from "../../components/Modal";
import { useUi } from "../../app/ui-context";
import { useWorkspace } from "../../app/workspace-context";
import { pageName } from "../../core/block-tree";
import type { NotePage, StoredFile } from "../../core/types";
import { useWorkspaceState } from "../../app/workspace-store";
import { t, tp } from "../../i18n";
import { bytesOf } from "../editor/file-store";
import { planExport } from "../export/build";
import type { ShareStatus } from "../../platform/mygo";
import { drawForPublish } from "./publish-render";
import { publishPages, unpublishPages, type PublishReport } from "./publisher";
import { goTransport } from "./share-transport";

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

/** Publish a page to the web, on the worker the user set up in Settings › Cloud: the page,
 *  and the pages under it (a database's rows) when asked. A page already published is
 *  updated at the same address, or taken off. */
export function PublishDialog({ page, status, onChanged, onClose }: { page: NotePage; status: ShareStatus; onChanged(): Promise<void>; onClose(): void }) {
  const { platform } = useWorkspace();
  const { toast } = useUi();
  const pages = useWorkspaceState((state) => state.pages);
  const titleOf = useMemo(() => (id: string) => (pages[id] ? pageName(pages[id]) : ""), [pages]);
  const [subpages, setSubpages] = useState(true);
  const [files, setFiles] = useState(true);
  const [busy, setBusy] = useState<string | null>(null);
  const [report, setReport] = useState<PublishReport | null>(null);
  const [problem, setProblem] = useState("");

  const everything = useMemo(() => planExport(pages, page.id, { subpages: true }, titleOf), [page.id, pages, titleOf]);
  const chosen = useMemo(() => planExport(pages, page.id, { subpages }, titleOf), [subpages, page.id, pages, titleOf]);
  const hasSubpages = everything.pageIds.length > 1;
  const hasFiles = everything.files.length > 0;
  const here = status.publications[page.id];
  const ready = status.enabled && status.configured;

  const run = async () => {
    if (!platform || busy) return;
    setBusy(t("Publishing…"));
    setProblem("");
    setReport(null);
    try {
      const result = await publishPages({
        pages, rootId: page.id, titleOf,
        subpages: subpages && hasSubpages,
        files: files && hasFiles,
        existing: status.publications,
        maxFileBytes: status.maxFileMB * 1024 * 1024,
        readFile: async (file: StoredFile) => {
          try { return new Uint8Array(await (await bytesOf(platform.files, file, file.path)).arrayBuffer()); } catch { return null; }
        },
        draw: drawForPublish,
        transport: goTransport,
        onProgress: (done, total, title) => setBusy(title ? t("Publishing {done} of {total}: {title}", { done: done + 1, total, title }) : t("Publishing…")),
      });
      setReport(result);
      await onChanged();
    } catch (error) {
      setProblem(error instanceof Error ? error.message : String(error));
      await onChanged();
    } finally {
      setBusy(null);
    }
  };

  const takeOff = async () => {
    if (busy) return;
    setBusy(t("Taking off the web…"));
    setProblem("");
    try {
      await unpublishPages({ pages, rootId: page.id, titleOf, subpages: subpages && hasSubpages, existing: status.publications, transport: goTransport });
      setReport(null);
      toast(t("Taken off the web"), "success");
      await onChanged();
    } catch (error) {
      setProblem(error instanceof Error ? error.message : String(error));
      await onChanged();
    } finally {
      setBusy(null);
    }
  };

  const address = report?.pages[0]?.url ?? here?.url;
  const copy = () => {
    if (!address) return;
    void navigator.clipboard?.writeText(address).then(() => toast(t("Link copied"), "success")).catch(() => toast(t("Could not copy that link"), "error"));
  };
  const publishedUnder = chosen.pageIds.filter((id) => status.publications[id]).length;

  return (
    <Modal open label={t("Publish to the web")} className="export" onClose={onClose}>
      <div className="share__head">
        <h2 className="share__title">{t("Publish to the web")}</h2>
        <button type="button" className="icon-btn" aria-label={t("Close")} onClick={onClose}><X size={16} /></button>
      </div>
      <div className="export__body">
        {!ready ? (
          <p className="export__summary">{t("Add your worker’s address and token in Settings › Cloud first.")}</p>
        ) : (
          <>
            {address ? (
              <div className="publish__link">
                <Globe size={15} aria-hidden="true" />
                <a href={address} target="_blank" rel="noopener noreferrer" title={address}>{address}</a>
                <button type="button" className="icon-btn" aria-label={t("Copy link")} onClick={copy}><Copy size={14} /></button>
                <a className="icon-btn" href={address} target="_blank" rel="noopener noreferrer" aria-label={t("Open in the browser")}><ExternalLink size={14} /></a>
              </div>
            ) : null}
            {hasSubpages ? (
              <Switch
                on={subpages}
                onChange={setSubpages}
                label={t("Include sub-pages")}
                hint={tp(everything.pageIds.length - 1, "{n} sub-page, with its own address", "{n} sub-pages, each with its own address")}
              />
            ) : null}
            {hasFiles ? (
              <Switch
                on={files}
                onChange={setFiles}
                label={t("Include images and files")}
                hint={t("Files over {n} MB are left out", { n: status.maxFileMB })}
              />
            ) : null}
            <p className="export__summary">
              {t("Anyone with the link can read it. Links to pages that are not published show as their names.")}
              {" "}
              {here || publishedUnder ? t("Publishing again updates it at the same address.") : ""}
            </p>
            {report ? (
              <p className="publish__report" role="status">
                <Check size={14} aria-hidden="true" />
                {t("Published {pages}.", { pages: tp(report.pages.length, "{n} page", "{n} pages") })}
                {report.linksAsText ? ` ${tp(report.linksAsText, "{n} link to a page that is not published is its name.", "{n} links to pages that are not published are their names.")}` : ""}
                {report.filesSkipped.length ? ` ${t("Left out: {names}.", { names: report.filesSkipped.slice(0, 4).join(", ") })}` : ""}
              </p>
            ) : null}
            {problem ? <p className="publish__problem" role="alert">{problem}</p> : null}
            {busy ? <p className="export__summary" role="status">{busy}</p> : null}
          </>
        )}
      </div>
      <div className="export__foot">
        {ready && (here || publishedUnder) ? (
          <button type="button" className="btn btn--outline" disabled={Boolean(busy)} onClick={() => void takeOff()}>{t("Take off the web")}</button>
        ) : null}
        <button type="button" className="btn btn--outline" onClick={onClose}>{t("Close")}</button>
        {ready ? (
          <button type="button" className="btn btn--primary" disabled={Boolean(busy) || !platform} onClick={() => void run()}>
            <Globe size={14} />{here ? t("Update") : t("Publish")}
          </button>
        ) : null}
      </div>
    </Modal>
  );
}
