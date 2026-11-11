import { emptyPlane, type NotesCollection, type PlaneRecords, type RecordRef, type RecordValue, type RecordWrite } from './records';
import { mergePatch } from './storage';
import { recordStore, type RecordStore, type RowChange } from './store';

const NOTES_COLLECTIONS = new Set<string>(['pages', 'blocks', 'versions', 'workspace_settings']);

/** One notebook plane: its saved records, with this window's pending writes
 *  laid over them. A write is saved by the store and the record it resolves to
 *  is taken as saved at once; changes from elsewhere are applied as they come,
 *  or the whole plane is read again when the store cannot say which changed. */
export class Plane {
  private current: PlaneRecords = emptyPlane();
  private stored: PlaneRecords = emptyPlane();
  private pending: { token: symbol; write: RecordWrite }[] = [];
  private writes: Promise<void> = Promise.resolve();
  private listeners = new Set<(records: PlaneRecords) => void>();
  private stop: () => void = () => {};
  private refreshing = Promise.resolve();
  private closed = false;
  /** The pages whose history has been read, for a store that keeps history out
   *  of its snapshot (the desktop app's: it is most of a vault's bytes). */
  private historyOf = new Map<string, Promise<void>>();

  private constructor(private readonly scope: string, private readonly store: RecordStore) {}

  static async open(spaceId?: string, store: RecordStore = recordStore()): Promise<Plane> {
    const plane = new Plane(spaceId ?? 'personal', store);
    plane.stop = store.onChange(changes => {
      if (changes === null) void plane.refresh().catch(console.error);
      else plane.apply(changes);
    });
    try {
      await plane.refresh();
      return plane;
    } catch (error) {
      plane.stop();
      throw error;
    }
  }

  /** Whether the page's history is here: always, for a store that sends it all
   *  in its snapshot, else once `loadHistory` has read it. */
  hasHistory(pageId: string): boolean {
    return !this.store.versions || this.historyOf.has(pageId);
  }

  /** Reads a page's history from the store, once. Reading again after the
   *  page's history is here changes nothing. */
  loadHistory(pageId: string): Promise<void> {
    const read = this.store.versions;
    if (!read || this.closed) return Promise.resolve();
    let loading = this.historyOf.get(pageId);
    if (!loading) {
      loading = read.call(this.store, this.scope, pageId).then(rows => {
        if (this.closed) return;
        const versions = this.stored.versions as Map<string, RecordValue>;
        for (const row of rows) {
          // A version heard of meanwhile, or written by this window, is newer.
          if (!versions.has(row.id)) versions.set(row.id, row.value as RecordValue);
        }
        this.project();
      }, error => {
        // Asked for again the next time: the page's history is not known yet.
        this.historyOf.delete(pageId);
        throw error;
      });
      this.historyOf.set(pageId, loading);
    }
    return loading;
  }

  watch(listener: (records: PlaneRecords) => void): () => void {
    this.listeners.add(listener);
    listener(this.current);
    return () => { this.listeners.delete(listener); };
  }

  get(ref: RecordRef): RecordValue | undefined {
    return this.current[ref.collection].get(ref.id);
  }

  write(write: RecordWrite): Promise<void> {
    if (this.closed) return Promise.reject(new Error('Notebook is closed.'));
    const token = Symbol();
    this.pending.push({ token, write: structuredClone(write) });
    this.project();
    const run = async () => {
      try {
        const value = await this.store.write(this.scope, write);
        // The record as the store saved it is what the plane now holds, so the
        // pending write can go without a re-read.
        this.commit(write, value);
      } finally {
        this.pending = this.pending.filter(item => item.token !== token);
        this.project();
      }
    };
    const result = this.writes.then(run, run);
    this.writes = result.catch(() => {});
    return result;
  }

  async close(): Promise<void> {
    this.closed = true;
    this.stop();
    this.listeners.clear();
  }

  private project(): void {
    const next = Object.fromEntries(Object.entries(this.stored).map(([name, values]) => [name, new Map(values)])) as unknown as PlaneRecords;
    for (const { write } of this.pending) {
      const records = next[write.collection] as Map<string, RecordValue>;
      if (write.action === 'delete') records.delete(write.id);
      else if (write.action === 'put') records.set(write.id, write.value);
      else records.set(write.id, mergePatch(records.get(write.id) ?? {}, write.patch) as RecordValue);
    }
    this.current = next;
    if (!this.closed) this.listeners.forEach(listener => listener(next));
  }

  private commit(write: RecordWrite, value: Record<string, unknown> | null): void {
    const records = this.stored[write.collection] as Map<string, RecordValue>;
    if (value === null) records.delete(write.id);
    else records.set(write.id, value as RecordValue);
  }

  private apply(changes: RowChange[]): void {
    if (this.closed) return;
    for (const change of changes) {
      if (!NOTES_COLLECTIONS.has(change.collection)) continue;
      const records = this.stored[change.collection as NotesCollection] as Map<string, RecordValue>;
      if (change.value === null) records.delete(change.id);
      else records.set(change.id, change.value as RecordValue);
    }
    this.project();
  }

  private refresh(): Promise<void> {
    const run = async () => {
      if (this.closed) return;
      const next = emptyPlane();
      for (const row of await this.store.snapshot(this.scope)) {
        if (row.collection in next) {
          (next[row.collection as keyof PlaneRecords] as Map<string, RecordValue>).set(row.id, row.value as RecordValue);
        }
      }
      this.stored = next;
      this.historyOf.clear();
      this.project();
    };
    this.refreshing = this.refreshing.then(run, run);
    return this.refreshing;
  }
}
