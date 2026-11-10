import { useRef, useState } from 'react';
import { Download, Upload } from 'lucide-react';
import { isMyGo } from 'mygo-runtime';
import { MenuItem } from './Menu';
import { useUi } from '../app/ui-context';
import { exportBackup, importBackupIntoVault, parseBackup, restoreBackup } from '../platform/backup';

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
      toast('Backup downloaded, including attachments.', 'success');
    } catch (error) { toast(error instanceof Error ? error.message : 'Backup failed.', 'error'); }
    finally { setBusy(false); }
  };

  const importIntoVault = async (file: File) => {
    const report = await importBackupIntoVault(await file.text());
    toast(`Imported ${report.pages} pages, ${report.blocks} paragraphs, ${report.attachments} files.`, 'success');
    window.setTimeout(() => window.location.reload(), 600);
  };

  const restore = async (file: File) => {
    const backup = parseBackup(await file.text());
    if (!window.confirm(`Replace this browser's notebook with this backup (${backup.records.filter(row => row.collection === 'pages').length} pages)? Download a backup first if you want to keep the current notebook.`)) return;
    await restoreBackup(backup);
    window.location.assign(window.location.pathname);
  };

  return <>
    {!desktop && <MenuItem icon={<Download size={16}/>} label={busy ? 'Working…' : 'Download backup'} onSelect={() => { if (!busy) void save(); }}/>}
    <MenuItem icon={<Upload size={16}/>} label={desktop ? 'Import browser backup…' : 'Restore backup…'} onSelect={() => { if (!busy) input.current?.click(); }}/>
    <input ref={input} type="file" accept=".json,application/json" hidden aria-label="Restore notebook backup" onChange={async event => {
      const file = event.currentTarget.files?.[0]; event.currentTarget.value = '';
      if (!file) return;
      setBusy(true);
      try {
        if (file.size > 200 * 1024 * 1024) throw new Error('Choose a backup smaller than 200 MB.');
        if (desktop) await importIntoVault(file);
        else await restore(file);
      } catch (error) { toast(error instanceof Error ? error.message : 'Restore failed.', 'error'); }
      finally { setBusy(false); }
    }}/>
  </>;
}
