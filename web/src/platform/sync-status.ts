import { isMyGo } from 'mygo-runtime';
import { events } from './mygo';
import type { SyncStatus } from './workspace-document';

/** The desktop app's sync state, as the save indicator shows it. Calls
 *  `listener` with the status, a message and the number of conflicts for each
 *  change; a browser has no remote folder, so it never calls it. */
export function onRemoteSync(listener: (status: SyncStatus, message: string, conflicts: number) => void): () => void {
  if (!isMyGo()) return () => {};
  return events.syncState.on(state => {
    const status = STATUS[state.status];
    if (status) listener(status, state.message ?? '', state.conflicts ?? 0);
  });
}

const STATUS: Record<string, SyncStatus> = {
  saving: 'saving',
  saved: 'saved',
  offline: 'offline',
  refused: 'refused',
};
