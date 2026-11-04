import { emptyPlane, type PlaneRecords, type RecordRef, type RecordValue, type RecordWrite } from './records';
import { changed, committed, database, mergePatch, onChange, readRecords, requested } from './storage';

/** One personal notebook, persisted locally; changes in other tabs reload it. */
export class Plane {
  private current: PlaneRecords = emptyPlane();
  private stored: PlaneRecords = emptyPlane();
  private pending: { token: symbol; write: RecordWrite }[] = [];
  private writes: Promise<void> = Promise.resolve();
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
  private listeners = new Set<(records: PlaneRecords) => void>();
  private stop: () => void = () => {};
  private refreshing = Promise.resolve();
  private closed = false;
  private constructor(private readonly scope: string) {}
  static async open(spaceId?: string): Promise<Plane> {
    const plane = new Plane(spaceId ?? 'personal');
    plane.stop = onChange(() => { void plane.refresh().catch(console.error); });
    try { await plane.refresh(); return plane; } catch (error) { plane.stop(); throw error; }
  }
  private refresh(): Promise<void> {
    const run = async () => {
      if (this.closed) return;
      const next = emptyPlane();
      for (const row of await readRecords()) {
        if (row.scope === this.scope && row.collection in next) {
          (next[row.collection as keyof PlaneRecords] as Map<string, RecordValue>).set(row.id, row.value as RecordValue);
        }
      }
      this.stored = next;
      this.project();
    };
    this.refreshing = this.refreshing.then(run, run);
    return this.refreshing;
  }
  watch(listener: (records: PlaneRecords) => void): () => void {
    this.listeners.add(listener); listener(this.current);
    return () => { this.listeners.delete(listener); };
  }
  get(ref: RecordRef): RecordValue | undefined { return this.current[ref.collection].get(ref.id); }
  write(write: RecordWrite): Promise<void> {
    if (this.closed) return Promise.reject(new Error('Notebook is closed.'));
    const token = Symbol();
    this.pending.push({ token, write: structuredClone(write) });
    this.project();
    const run = async () => {
      try {
        await this.persist(write);
        // Keep every pending edit visible until a fresh committed snapshot includes this write.
        await this.refresh();
      } finally {
        this.pending = this.pending.filter(item => item.token !== token);
        this.project();
      }
    };
    const result = this.writes.then(run, run);
    this.writes = result.catch(() => {});
    return result;
  }
  private async persist(write: RecordWrite): Promise<void> {
    const db = await database();
    const transaction = db.transaction('records', 'readwrite');
    const done = committed(transaction);
    const store = transaction.objectStore('records');
    const key = [this.scope, write.collection, write.id];
    if (write.action === 'delete') store.delete(key);
    else if (write.action === 'put') store.put({ scope: this.scope, collection: write.collection, id: write.id, value: write.value });
    else {
      const before = await requested(store.get(key));
      store.put({ scope: this.scope, collection: write.collection, id: write.id, value: mergePatch(before?.value ?? {}, write.patch) });
    }
    await done;
    changed();
  }
  async close(): Promise<void> { this.closed = true; this.stop(); this.listeners.clear(); }
}
