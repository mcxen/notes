import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { t } from "../../i18n";
import type { BlockTree } from "../../core/types";
import { currentHeading, headingsOf } from "./headings";

/** A page needs this many headings before it gets an outline. */
const MIN_HEADINGS = 2;
/** How far below the top of the page a heading is when the outline calls it current. */
const LINE_OFFSET = 96;

/** The page's outline, as Notion has it: a column of short lines at the page's
 *  right edge, one for each heading, the one the page is at darker. Pointing at
 *  it opens the headings by name, and a press takes the page to one. It sits in
 *  the page's scroller, which it finds itself, and is hidden by the stylesheet
 *  when the page is too narrow for it or the app's settings turn it off. */
export function PageOutline({ tree }: { tree: BlockTree }) {
  const anchor = useRef<HTMLDivElement | null>(null);
  const headings = useMemo(() => headingsOf(tree), [tree]);
  const [active, setActive] = useState(0);

  const scroller = useCallback((): HTMLElement | null => anchor.current?.closest<HTMLElement>(".page-surface") ?? null, []);
  const rowOf = useCallback((id: string): HTMLElement | null => (
    scroller()?.querySelector<HTMLElement>(`.blocks [data-block-id="${CSS.escape(id)}"]`) ?? null
  ), [scroller]);

  // Which heading the page is at, as it scrolls and as headings come and go.
  useEffect(() => {
    const surface = scroller();
    if (!surface || headings.length < MIN_HEADINGS) return;
    let frame = 0;
    const measure = () => {
      frame = 0;
      const top = surface.getBoundingClientRect().top;
      const tops = headings.map((heading) => (rowOf(heading.id)?.getBoundingClientRect().top ?? Number.POSITIVE_INFINITY) - top);
      setActive(currentHeading(tops, LINE_OFFSET));
    };
    const schedule = () => { if (!frame) frame = requestAnimationFrame(measure); };
    schedule();
    surface.addEventListener("scroll", schedule, { passive: true });
    window.addEventListener("resize", schedule);
    return () => {
      if (frame) cancelAnimationFrame(frame);
      surface.removeEventListener("scroll", schedule);
      window.removeEventListener("resize", schedule);
    };
  }, [headings, rowOf, scroller]);

  const go = (id: string) => {
    const row = rowOf(id);
    if (!row) return;
    const calm = window.matchMedia?.("(prefers-reduced-motion: reduce)").matches;
    row.scrollIntoView({ block: "start", behavior: calm ? "auto" : "smooth" });
    // The same light the search lights the line it found with.
    row.classList.add("is-found");
    window.setTimeout(() => row.classList.remove("is-found"), 1800);
  };

  if (headings.length < MIN_HEADINGS) return <div ref={anchor} hidden />;
  const current = Math.min(active, headings.length - 1);
  return (
    <div ref={anchor} className="outline-anchor">
      <nav className="outline" aria-label={t("Page outline")} style={{ "--outline-n": headings.length } as React.CSSProperties}>
        <div className="outline__ticks" aria-hidden="true">
          {headings.map((heading, index) => (
            <span key={heading.id} className={`outline__tick outline__tick--${heading.level} ${index === current ? "is-active" : ""}`} />
          ))}
        </div>
        <ul className="outline__panel">
          {headings.map((heading, index) => (
            <li key={heading.id}>
              <button
                type="button"
                className={`outline__item outline__item--${heading.level} ${index === current ? "is-active" : ""}`}
                aria-current={index === current ? "location" : undefined}
                onClick={() => go(heading.id)}
              >
                {heading.text}
              </button>
            </li>
          ))}
        </ul>
      </nav>
    </div>
  );
}
