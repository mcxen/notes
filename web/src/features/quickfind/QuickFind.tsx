import { Search } from "lucide-react";
import { useEffect, useMemo, useState, type ReactNode } from "react";
import { Modal } from "../../components/Modal";
import { imeComposing } from "../../core/keyboard";
import { pageName } from "../../core/block-tree";
import { useWorkspace } from "../../app/workspace-context";
import { markRanges, queryWords, quickFind, type Excerpt, type Hit } from "./search";
import { requestReveal } from "./reveal";
import { useUi } from "../../app/ui-context";
import { useWorkspaceState } from "../../app/workspace-store";
import { pageGlyph } from "../editor/block-registry";
import { t } from "../../i18n";

export { quickFind };
export type { Hit };

/** `text` with the ranges marked, as the page shows a match. */
function Marked({ text, marks }: { text: string; marks: ReadonlyArray<readonly [number, number]> }) {
  if (!marks.length) return <>{text}</>;
  const parts: ReactNode[] = [];
  let at = 0;
  marks.forEach(([from, to], index) => {
    if (from > at) parts.push(text.slice(at, from));
    parts.push(<mark key={index} className="quickfind__mark">{text.slice(from, to)}</mark>);
    at = to;
  });
  if (at < text.length) parts.push(text.slice(at));
  return <>{parts}</>;
}

function excerptMarks(excerpt: Excerpt) {
  return excerpt.marks;
}

/** ⌘K. Titles first, then the first line of body text that holds the words,
 *  cut around them and marked, so that two similarly named pages can be told
 *  apart; a press opens the page at that line. Several words must all be
 *  there. */
export function QuickFind() {
  const { selectPage } = useWorkspace();
  const { quickFindOpen, setQuickFindOpen } = useUi();
  // Closed, it reads nothing — and is not redrawn as pages change.
  const allPages = useWorkspaceState((state) => (quickFindOpen ? state.pages : null));
  const [query, setQuery] = useState("");
  const [active, setActive] = useState(0);

  useEffect(() => {
    if (quickFindOpen) {
      setQuery("");
      setActive(0);
    }
  }, [quickFindOpen]);

  const hits = useMemo<Hit[]>(() => (allPages ? quickFind(allPages, query) : []), [allPages, query]);

  const words = useMemo(() => queryWords(query), [query]);
  const open = (hit: Hit) => {
    if (hit.blockId) requestReveal(hit.page.id, hit.blockId);
    selectPage(hit.page.id);
    setQuickFindOpen(false);
  };

  return (
    <Modal open={quickFindOpen} label={t("Search pages")} className="quickfind" onClose={() => setQuickFindOpen(false)}>
      <div className="quickfind__input">
        <Search size={18} />
        <input
          autoFocus
          value={query}
          placeholder={t("Search pages…")}
          aria-label={t("Search pages")}
          onChange={(event) => { setQuery(event.target.value); setActive(0); }}
          onKeyDown={(event) => {
            if (imeComposing(event)) return;
            if (event.key === "ArrowDown" || event.key === "ArrowUp") {
              event.preventDefault();
              const delta = event.key === "ArrowDown" ? 1 : -1;
              setActive((index) => (index + delta + hits.length) % Math.max(1, hits.length));
              return;
            }
            if (event.key === "Enter" && hits[active]) {
              event.preventDefault();
              open(hits[active]);
            }
          }}
        />
      </div>

      <div className="menu scroller" role="listbox" aria-label={t("Results")}>
        {hits.length ? hits.map((hit, index) => (
          <button
            type="button"
            key={hit.page.id}
            role="option"
            aria-selected={index === active}
            className={`menu__item ${index === active ? "is-active" : ""}`}
            onPointerMove={() => setActive(index)}
            onClick={() => open(hit)}
          >
            <span className="menu__icon" aria-hidden="true">{pageGlyph(hit.page)}</span>
            <span className="menu__copy">
              <span className="menu__label"><Marked text={pageName(hit.page)} marks={markRanges(pageName(hit.page), words)} /></span>
              {hit.excerpt ? <span className="menu__description"><Marked text={hit.excerpt.text} marks={excerptMarks(hit.excerpt)} /></span> : null}
            </span>
            {hit.lines > 1 ? <span className="quickfind__crumb">{hit.lines} matches</span> : hit.crumb ? <span className="quickfind__crumb">{hit.crumb}</span> : null}
          </button>
        )) : (
          <div className="slash__empty">{query.trim() ? `No pages match “${query.trim()}”` : "No pages yet"}</div>
        )}
      </div>
    </Modal>
  );
}
