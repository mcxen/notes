import { useSyncExternalStore } from "react";
import { isMyGo } from "mygo-runtime";
import type { BlockKind } from "../../core/types";
import {
  events,
  Notebooks,
  type ClipboardPayload,
  type ClipboardPreview,
  type ClipboardState,
} from "../../platform/mygo";

export const CLIPBOARD_TTL_MS = 10 * 60_000;

export interface ClipboardSnapshot {
  enabled: boolean;
  items: ClipboardPreview[];
  activeIndex: number;
  activeItem: ClipboardPreview | null;
}

const listeners = new Set<() => void>();
let enabled = false;
let items: ClipboardPreview[] = [];
let activeIndex = 0;
let snapshot: ClipboardSnapshot = { enabled: false, items: [], activeIndex: 0, activeItem: null };
let pruneTimer: number | undefined;
let initialized = false;

function isExpired(item: ClipboardPreview, now = Date.now()): boolean {
  const parsed = Date.parse(item.copiedAt);
  if (!Number.isFinite(parsed)) return false;
  return now - parsed >= CLIPBOARD_TTL_MS;
}

function clearPruneTimer(): void {
  if (pruneTimer !== undefined) {
    window.clearTimeout(pruneTimer);
    pruneTimer = undefined;
  }
}

function rebuildSnapshot(): void {
  const clamped = items.length ? Math.min(Math.max(0, activeIndex), items.length - 1) : 0;
  activeIndex = clamped;
  snapshot = {
    enabled,
    items,
    activeIndex: clamped,
    activeItem: items[clamped] ?? null,
  };
}

function schedulePrune(): void {
  clearPruneTimer();
  if (!enabled || !items.length || typeof window === "undefined") return;
  const now = Date.now();
  let earliest = Infinity;
  for (const item of items) {
    const at = Date.parse(item.copiedAt);
    if (!Number.isFinite(at)) continue;
    const left = at + CLIPBOARD_TTL_MS - now;
    if (left < earliest) earliest = left;
  }
  if (!Number.isFinite(earliest)) return;
  pruneTimer = window.setTimeout(() => {
    pruneTimer = undefined;
    pruneExpired();
  }, Math.max(50, earliest));
}

function emit(): void {
  rebuildSnapshot();
  schedulePrune();
  for (const listener of listeners) listener();
}

function pruneExpired(): void {
  const now = Date.now();
  const kept = items.filter((item) => !isExpired(item, now));
  if (kept.length === items.length) {
    schedulePrune();
    return;
  }
  items = kept;
  emit();
}

export function setClipboardState(next: ClipboardState | undefined): void {
  const nextEnabled = Boolean(next?.enabled);
  const now = Date.now();
  const nextItems = nextEnabled
    ? (next?.items ?? []).filter((item) => !isExpired(item, now))
    : [];

  const prevHeadId = items[0]?.id;
  const nextHeadId = nextItems[0]?.id;
  const prevActiveId = items[activeIndex]?.id;

  enabled = nextEnabled;
  items = nextItems;

  if (!enabled || !items.length) {
    activeIndex = 0;
  } else if (nextHeadId && nextHeadId !== prevHeadId) {
    // A brand-new item arrived at index 0: show it immediately.
    activeIndex = 0;
  } else if (prevActiveId) {
    const keptAt = items.findIndex((item) => item.id === prevActiveId);
    activeIndex = keptAt >= 0 ? keptAt : 0;
  } else {
    activeIndex = 0;
  }
  emit();
}

export function initClipboardStore(): void {
  if (initialized || !isMyGo()) return;
  initialized = true;
  events.clipboardChanged.on(setClipboardState);
  void Notebooks.clipboardState().then(setClipboardState, () => undefined);
}

function subscribe(listener: () => void): () => void {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

export function useClipboardQueue(): ClipboardSnapshot {
  return useSyncExternalStore(subscribe, () => snapshot);
}

export function getClipboardSnapshot(): ClipboardSnapshot {
  return snapshot;
}

export function cycleClipboardItem(delta: -1 | 1): void {
  if (items.length <= 1) return;
  activeIndex = (activeIndex + delta + items.length) % items.length;
  emit();
}

function removeLocal(id: string): void {
  const index = items.findIndex((item) => item.id === id);
  if (index < 0) return;
  items = items.filter((item) => item.id !== id);
  if (activeIndex >= items.length) activeIndex = Math.max(0, items.length - 1);
  emit();
}

export function dismissClipboardItem(id: string): void {
  removeLocal(id);
  if (isMyGo()) void Notebooks.dismissClipboardItem(id).catch(() => undefined);
}

export async function takeClipboardItem(id: string): Promise<ClipboardPayload | null> {
  removeLocal(id);
  if (!isMyGo()) return null;
  try {
    const payload = await Notebooks.takeClipboardItem(id);
    return payload?.id ? payload : null;
  } catch {
    return null;
  }
}

export function consumeClipboardMatch(text: string): void {
  if (!isMyGo()) return;
  void Notebooks.consumeClipboardMatch(text).catch(() => undefined);
}

export function resetClipboardStoreForTest(): void {
  clearPruneTimer();
  enabled = false;
  items = [];
  activeIndex = 0;
  rebuildSnapshot();
  for (const listener of listeners) listener();
}

export interface GhostEligibility {
  enabled: boolean;
  hasItem: boolean;
  readOnly: boolean;
  embed: boolean;
  blockKind: BlockKind;
  empty: boolean;
  focused: boolean;
  hasSlash: boolean;
  hasMention: boolean;
  hasTimeMenu: boolean;
  hasBlockSelection: boolean;
  composing: boolean;
}

/** Ghost preview appears strictly on a focused, editable, empty plain
 *  paragraph (`block.kind === "text"`), never on lists or other block kinds. */
export function isClipboardGhostEligible(p: GhostEligibility): boolean {
  return p.enabled
    && p.hasItem
    && !p.readOnly
    && !p.embed
    && p.blockKind === "text"
    && p.empty
    && p.focused
    && !p.hasSlash
    && !p.hasMention
    && !p.hasTimeMenu
    && !p.hasBlockSelection
    && !p.composing;
}
