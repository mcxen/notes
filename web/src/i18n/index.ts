/* The words of the pages, in English or Chinese.

   The English text is the key: `t("Copy link")` says it as written, or in
   Chinese when the page shows Chinese and `zh.ts` has it. A text with
   `{name}` places takes its values from the second argument; `tp` picks the
   singular or plural form in English (Chinese has one form). The language
   is the app's (MyGo gives it to the page, and tells of changes), else the
   one kept in the browser, else the browser's. Changing it redraws the
   whole page: `useLang` re-renders what reads it, and `main.tsx` mounts the
   page again under the new language. */

import { isMyGo, locale, onLocaleChange } from "mygo-runtime";
import { useSyncExternalStore } from "react";
import { zh } from "./zh";

export type Lang = "en" | "zh";

const STORAGE_KEY = "notes.language";
const listeners = new Set<() => void>();

function fromTag(tag: string | null | undefined): Lang | null {
  if (!tag) return null;
  const lower = tag.toLowerCase();
  if (lower.startsWith("zh")) return "zh";
  if (lower.startsWith("en")) return "en";
  return null;
}

function detect(): Lang {
  if (isMyGo()) return fromTag(locale()) ?? "en";
  try {
    const kept = fromTag(window.localStorage.getItem(STORAGE_KEY));
    if (kept) return kept;
  } catch { /* storage may be blocked */ }
  return fromTag(navigator.language) ?? "en";
}

let current: Lang = detect();

function announce(): void {
  document.documentElement.lang = current === "zh" ? "zh-Hans" : "en";
  listeners.forEach((listener) => listener());
}

/** The language the page shows. */
export function language(): Lang {
  return current;
}

/** Shows another language in a browser, and keeps it. In the desktop app the
 *  language is the app's: it is set there (App.SetLocale), and heard here. */
export function setLanguage(next: Lang): void {
  if (next === current) return;
  current = next;
  try { window.localStorage.setItem(STORAGE_KEY, next); } catch { /* not kept */ }
  announce();
}

if (typeof document !== "undefined") {
  document.documentElement.lang = current === "zh" ? "zh-Hans" : "en";
  if (isMyGo()) {
    onLocaleChange((tag) => {
      const next = fromTag(tag) ?? "en";
      if (next === current) return;
      current = next;
      announce();
    });
  }
}

function fill(text: string, values?: Record<string, string | number>): string {
  return values ? text.replace(/\{(\w+)\}/g, (whole, name: string) => (name in values ? String(values[name]) : whole)) : text;
}

/** `text` in the page's language. */
export function t(text: string, values?: Record<string, string | number>): string {
  return fill(current === "zh" ? zh[text] ?? text : text, values);
}

/** A count with its noun: `one` when `n` is 1 in English; `other` otherwise
 *  and in Chinese. Both hold `{n}`. The Chinese is looked up by `other`. */
export function tp(n: number, one: string, other: string): string {
  return fill(current === "zh" ? zh[other] ?? other : n === 1 ? one : other, { n });
}

/** Re-renders a component when the language changes, and says which it is. */
export function useLang(): Lang {
  return useSyncExternalStore(
    (listener) => { listeners.add(listener); return () => { listeners.delete(listener); }; },
    () => current,
    () => current,
  );
}
