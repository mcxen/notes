import { useRef, useState } from 'react';
import { Download, Upload } from 'lucide-react';
import { isMyGo } from 'mygo-runtime';
import { MenuItem } from './Menu';
import { useUi } from '../app/ui-context';
import { exportBackup, importBackupIntoVault, parseBackup, restoreBackup } from '../platform/backup';
import { t } from "../i18n";

/* In the desktop app the notebook is the vault folder, so the only backup to
   take in is a browser backup, which is merged into the open vault. In a
   browser the notebook is IndexedDB, which downloads and restores as before. */
export function BackupControls() {
  const input = useRef<HTMLInputElement>(null);
  const [busy, setBusy] = useState(false);
  const { toast } = useUi();
  const desktop = isMyGo();

  const save = async () => {
    setBusy(true);
    try {
      const backup = await exportBackup();
      const url = URL.createObjectURL(new Blob([JSON.stringify(backup)], { type: 'application/json' }));
      const link = document.createElement('a');
      link.href = url; link.download = `open-notes-${new Date().toISOString().slice(0,10)}.json`; link.click();
      setTimeout(() => URL.revokeObjectURL(url), 1000);
      toast(t('Backup downloaded, including attachments.'), 'success');
    } catch (error) { toast(error instanceof Error ? error.message : t('Backup failed.'), 'error'); }
    finally { setBusy(false); }
  };

  const importIntoVault = async (file: File) => {
    const report = await importBackupIntoVault(await file.text());
    toast(t('Imported {pages} pages, {blocks} paragraphs, {files} files.', { pages: report.pages, blocks: report.blocks, files: report.attachments }), 'success');
    window.setTimeout(() => window.location.reload(), 600);
  };

  const restore = async (file: File) => {
    const backup = parseBackup(await file.text());
    if (!window.confirm(t("Replace this browser's notebook with this backup ({n} pages)? Download a backup first if you want to keep the current notebook.", { n: backup.records.filter(row => row.collection === 'pages').length }))) return;
    await restoreBackup(backup);
    window.location.assign(window.location.pathname);
  };

  return <>
    {!desktop && <MenuItem icon={<Download size={16}/>} label={busy ? t('Working…') : t('Download backup')} onSelect={() => { if (!busy) void save(); }}/>}
    <MenuItem icon={<Upload size={16}/>} label={desktop ? t('Import browser backup…') : t('Restore backup…')} onSelect={() => { if (!busy) input.current?.click(); }}/>
    <input ref={input} type="file" accept=".json,application/json" hidden aria-label={t("Restore notebook backup")} onChange={async event => {
      const file = event.currentTarget.files?.[0]; event.currentTarget.value = '';
      if (!file) return;
      setBusy(true);
      try {
        if (file.size > 200 * 1024 * 1024) throw new Error(t('Choose a backup smaller than 200 MB.'));
        if (desktop) await importIntoVault(file);
        else await restore(file);
      } catch (error) { toast(error instanceof Error ? error.message : t('Restore failed.'), 'error'); }
      finally { setBusy(false); }
    }}/>
  </>;
}
