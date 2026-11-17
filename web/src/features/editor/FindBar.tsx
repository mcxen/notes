import { ArrowDown, ArrowUp, CaseSensitive, ChevronRight, Replace, ReplaceAll, WholeWord, X } from "lucide-react";
import { useEffect, useLayoutEffect, useMemo, useRef, useState, type KeyboardEvent, type RefObject } from "react";
import { Tooltip } from "../../components/Tooltip";
import { imeComposing } from "../../core/keyboard";
import type { BlockTree } from "../../core/types";
import { t } from "../../i18n";
import { applyEditorTransaction, type EditorTransaction } from "./editor-commands";
import { FIND_DEFAULTS, findInText, findInTree, replaceMatches, type FindMatch, type FindOptions } from "./find";
import { domRangeIn, editableWithin } from "./selection";

/** What opens the bar: the words selected in the page, and whether the
 *  replace row is asked for. `n` changes each time, so that asking again
 *  while the bar is open puts the caret back in it. */
export interface FindRequest {
  n: number;
  query: string;
  replace: boolean;
}

const MATCH = "find-match";
const CURRENT = "find-current";

/** The browser can paint over words without touching the page (CSS Custom
 *  Highlight API): the page's own markup, which the editor keeps, is left alone. */
function paintable(): boolean {
  return typeof CSS !== "undefined" && "highlights" in CSS && typeof Highlight !== "undefined";
}

function clearPaint(): void {
  if (!paintable()) return;
  CSS.highlights.delete(MATCH);
  CSS.highlights.delete(CURRENT);
}

/** Find and replace within one page — Notion's ⌘F, with a replace row.
 *  The words found are painted over the page, the current one apart, and the
 *  page is scrolled to it; replacing writes through the page's own
 *  transactions, so each replace — and Replace all, as one — is a step to
 *  undo. Nothing here is saved. */
export function FindBar({
  root,
  tree,
  readOnly,
  request,
  onReplace,
  onShow,
  onClose,
}: {
  /** The page's blocks, for the words' places on screen. */
  root: RefObject<HTMLElement | null>;
  tree: BlockTree;
  readOnly: boolean;
  request: FindRequest;
  onReplace(transaction: EditorTransaction): void;
  /** A match with no line on screen — under a folded toggle: open what hides it. */
  onShow(match: FindMatch): void;
  /** Closed; `current` is the match the page should put its selection on. */
  onClose(current: FindMatch | null): void;
}) {
  const [query, setQuery] = useState(request.query);
  const [replacement, setReplacement] = useState("");
  const [options, setOptions] = useState<FindOptions>(FIND_DEFAULTS);
  const [replacing, setReplacing] = useState(request.replace && !readOnly);
  const [index, setIndex] = useState(0);
  const findInput = useRef<HTMLInputElement | null>(null);
  /** Set when the person moves — a new word, a step — and cleared once the
   *  page has scrolled there: typing in the page must not scroll it back. */
  const wantsScroll = useRef(true);

  const matches = useMemo(() => findInTree(tree, query, options), [options, query, tree]);
  const current = matches.length ? Math.min(index, matches.length - 1) : -1;
  const match = current >= 0 ? matches[current] : null;

  // Asked for again — a new word selected, or the replace row wanted.
  useEffect(() => {
    if (request.query) {
      setQuery(request.query);
      setIndex(0);
      wantsScroll.current = true;
    }
    if (request.replace && !readOnly) setReplacing(true);
    findInput.current?.focus();
    findInput.current?.select();
  }, [readOnly, request]);

  useLayoutEffect(() => {
    const lines = new Map<string, HTMLElement | null>();
    const lineOf = (blockId: string) => {
      if (!lines.has(blockId)) lines.set(blockId, editableWithin(root.current, blockId));
      return lines.get(blockId) ?? null;
    };
    if (paintable()) {
      const others: Range[] = [];
      let now: Range | null = null;
      matches.forEach((found, at) => {
        const line = lineOf(found.blockId);
        if (!line) return;
        const range = domRangeIn(line, found.start, found.end);
        if (at === current) now = range;
        else others.push(range);
      });
      CSS.highlights.set(MATCH, new Highlight(...others));
      CSS.highlights.set(CURRENT, new Highlight(...(now ? [now] : [])));
    }
    if (!match || !wantsScroll.current) return;
    const line = lineOf(match.blockId);
    if (!line) {
      onShow(match);
      return;
    }
    wantsScroll.current = false;
    const rect = domRangeIn(line, match.start, match.end).getBoundingClientRect();
    // Only when it is out of sight, or under the bar: a word in view stays put.
    if (rect.top < 110 || rect.bottom > window.innerHeight - 40) {
      (line.parentElement ?? line).scrollIntoView({ block: "center" });
    }
  }, [current, match, matches, onShow, root, tree]);

  useEffect(() => clearPaint, []);

  const step = (by: 1 | -1) => {
    if (!matches.length) return;
    wantsScroll.current = true;
    setIndex((current + by + matches.length) % matches.length);
  };

  const replaceOne = () => {
    if (!match || readOnly) return;
    const change = replaceMatches(tree, [match], replacement);
    if (!change) return;
    // The next one: the replacement may hold the words itself, and is stepped over.
    const inside = findInText(replacement, query, options).length;
    const next = findInTree(applyEditorTransaction(tree, change), query, options);
    wantsScroll.current = true;
    setIndex(next.length ? (current + inside) % next.length : 0);
    onReplace(change);
  };

  const replaceAll = () => {
    if (!matches.length || readOnly) return;
    const change = replaceMatches(tree, matches, replacement);
    if (change) onReplace(change);
    setIndex(0);
  };

  const toggle = (name: keyof FindOptions) => {
    wantsScroll.current = true;
    setIndex(0);
    setOptions((now) => ({ ...now, [name]: !now[name] }));
  };

  const onKeys = (event: KeyboardEvent<HTMLInputElement>, field: "find" | "replace") => {
    if (imeComposing(event)) return;
    const mod = event.metaKey || event.ctrlKey;
    if (event.key === "Escape") {
      event.preventDefault();
      event.stopPropagation();
      onClose(match);
    } else if (mod && event.code === "KeyF") {
      event.preventDefault();
      findInput.current?.focus();
      findInput.current?.select();
    } else if (mod && event.key.toLowerCase() === "g") {
      event.preventDefault();
      step(event.shiftKey ? -1 : 1);
    } else if (event.key === "Enter") {
      event.preventDefault();
      if (field === "replace") replaceOne();
      else step(event.shiftKey ? -1 : 1);
    }
  };

  const status = !query ? "" : matches.length ? t("{n} of {total}", { n: current + 1, total: matches.length }) : t("No results");

  return (
    <div className="findbar" role="search" aria-label={t("Find in page")} onKeyDown={(event) => event.stopPropagation()}>
      <div className="findbar__row">
        {readOnly ? <span className="findbar__twist-space" /> : (
          <Tooltip label={replacing ? t("Hide replace") : t("Show replace")}>
            <button
              type="button"
              className="icon-btn findbar__twist"
              aria-label={replacing ? t("Hide replace") : t("Show replace")}
              aria-expanded={replacing}
              onClick={() => setReplacing((open) => !open)}
            >
              <ChevronRight size={15} />
            </button>
          </Tooltip>
        )}
        <input
          ref={findInput}
          className="findbar__input"
          type="text"
          value={query}
          placeholder={t("Find in page")}
          aria-label={t("Find in page")}
          spellCheck={false}
          autoComplete="off"
          autoFocus
          onChange={(event) => { wantsScroll.current = true; setIndex(0); setQuery(event.target.value); }}
          onKeyDown={(event) => onKeys(event, "find")}
        />
        <span className={`findbar__status${query && !matches.length ? " is-none" : ""}`} aria-live="polite">{status}</span>
        <Tooltip label={t("Match case")}>
          <button type="button" className="icon-btn" aria-label={t("Match case")} aria-pressed={options.caseSensitive} onClick={() => toggle("caseSensitive")}>
            <CaseSensitive size={16} />
          </button>
        </Tooltip>
        <Tooltip label={t("Whole word")}>
          <button type="button" className="icon-btn" aria-label={t("Whole word")} aria-pressed={options.wholeWord} onClick={() => toggle("wholeWord")}>
            <WholeWord size={16} />
          </button>
        </Tooltip>
        <Tooltip label={t("Previous match")} hint="⇧↵">
          <button type="button" className="icon-btn" aria-label={t("Previous match")} disabled={!matches.length} onClick={() => step(-1)}>
            <ArrowUp size={15} />
          </button>
        </Tooltip>
        <Tooltip label={t("Next match")} hint="↵">
          <button type="button" className="icon-btn" aria-label={t("Next match")} disabled={!matches.length} onClick={() => step(1)}>
            <ArrowDown size={15} />
          </button>
        </Tooltip>
        <Tooltip label={t("Close")} hint="Esc">
          <button type="button" className="icon-btn" aria-label={t("Close")} onClick={() => onClose(match)}>
            <X size={15} />
          </button>
        </Tooltip>
      </div>
      {replacing && !readOnly ? (
        <div className="findbar__row">
          <span className="findbar__twist-space" />
          <input
            className="findbar__input"
            type="text"
            value={replacement}
            placeholder={t("Replace with")}
            aria-label={t("Replace with")}
            spellCheck={false}
            autoComplete="off"
            onChange={(event) => setReplacement(event.target.value)}
            onKeyDown={(event) => onKeys(event, "replace")}
          />
          <Tooltip label={t("Replace")} hint="↵">
            <button type="button" className="icon-btn" aria-label={t("Replace")} disabled={!match} onClick={replaceOne}>
              <Replace size={15} />
            </button>
          </Tooltip>
          <Tooltip label={t("Replace all")}>
            <button type="button" className="icon-btn" aria-label={t("Replace all")} disabled={!matches.length} onClick={replaceAll}>
              <ReplaceAll size={15} />
            </button>
          </Tooltip>
        </div>
      ) : null}
    </div>
  );
}
