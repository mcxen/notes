import { useState } from "react";
import type { DatabaseProperty, NotePage } from "../../core/types";
import { t } from "../../i18n";
import { arcPath, chartSlices, type Slice } from "./chart-data";

/** A database as a chart: its rows counted by the option each has in a status
 *  or a select. A ring shows each option's share of the rows (the total in its
 *  middle), bars their counts; the options are listed beside it with both, and
 *  one pressed — a slice, a bar or its line — shows the rows that have it,
 *  each of which opens. Drawn as SVG and CSS, in the colours the options have. */

const SIZE = 200;
const OUTER = 92;
const INNER = 60;

const label = (slice: Slice) => slice.name || t("No value");
const percent = (slice: Slice) => `${Math.round(slice.share * 100)}%`;

export function ChartLayout({
  rows,
  property,
  style,
  peekId,
  onOpen,
}: {
  rows: NotePage[];
  property: DatabaseProperty;
  style: "donut" | "bar";
  peekId: string | null;
  onOpen(pageId: string, full?: boolean): void;
}) {
  const [picked, setPicked] = useState<string | null>(null);
  const slices = chartSlices(rows, property);
  const total = rows.length;
  const most = Math.max(1, ...slices.map((slice) => slice.count));
  const chosen = slices.find((slice) => slice.name === picked);
  const pick = (slice: Slice) => setPicked((current) => (current === slice.name ? null : slice.name));
  const keys = (slice: Slice) => ({
    role: "button" as const,
    tabIndex: 0,
    "aria-pressed": picked === slice.name,
    "aria-label": `${label(slice)}: ${slice.count} (${percent(slice)})`,
    onClick: () => pick(slice),
    onKeyDown: (event: { key: string; preventDefault(): void }) => {
      if (event.key === "Enter" || event.key === " ") {
        event.preventDefault();
        pick(slice);
      }
    },
  });

  let from = 0;
  const arcs = slices.filter((slice) => slice.count > 0).map((slice) => {
    const arc = { slice, path: arcPath(SIZE / 2, SIZE / 2, OUTER, INNER, from, from + slice.share) };
    from += slice.share;
    return arc;
  });

  return (
    <div className="chart" data-chart={style}>
      <div className="chart__figure">
        {style === "donut" ? (
          <svg className="chart__ring" viewBox={`0 0 ${SIZE} ${SIZE}`} role="img" aria-label={t("Chart of {name}", { name: property.name })}>
            {total === 0 ? (
              <circle className="chart__empty" cx={SIZE / 2} cy={SIZE / 2} r={(OUTER + INNER) / 2} strokeWidth={OUTER - INNER} />
            ) : arcs.map(({ slice, path }) => (
              <path
                key={slice.name || "none"}
                className={`chart__slice${picked === slice.name ? " is-picked" : ""}${picked !== null && picked !== slice.name ? " is-dim" : ""}`}
                data-tone={slice.tone}
                d={path}
                fillRule="evenodd"
                {...keys(slice)}
              />
            ))}
            <text className="chart__total" x={SIZE / 2} y={SIZE / 2 - 2} textAnchor="middle">{chosen ? chosen.count : total}</text>
            <text className="chart__caption" x={SIZE / 2} y={SIZE / 2 + 18} textAnchor="middle">{chosen ? label(chosen) : t("Total")}</text>
          </svg>
        ) : (
          <div className="chart__bars" role="img" aria-label={t("Chart of {name}", { name: property.name })}>
            {slices.map((slice) => (
              <div key={slice.name || "none"} className={`chart__bar-row${picked === slice.name ? " is-picked" : ""}`} {...keys(slice)}>
                <span className="chart__bar-name">{label(slice)}</span>
                <span className="chart__bar-track">
                  <span className="chart__bar" data-tone={slice.tone} style={{ width: `${(slice.count / most) * 100}%` }} />
                </span>
                <span className="chart__bar-count">{slice.count}</span>
              </div>
            ))}
          </div>
        )}
      </div>
      {style === "donut" ? (
        <ul className="chart__legend">
          {slices.map((slice) => (
            <li key={slice.name || "none"} className={`chart__key${picked === slice.name ? " is-picked" : ""}`} {...keys(slice)}>
              <span className="chart__dot" data-tone={slice.tone} />
              <span className="chart__name">{label(slice)}</span>
              <span className="chart__count">{slice.count}</span>
              <span className="chart__share">{percent(slice)}</span>
            </li>
          ))}
        </ul>
      ) : null}
      {chosen ? (
        <div className="chart__rows">
          <div className="chart__rows-title">{label(chosen)} · {chosen.count}</div>
          {chosen.rows.map((row) => (
            <button key={row.id} type="button" className={`chart__row${peekId === row.id ? " is-peeked" : ""}`} data-page-id={row.id} onClick={(event) => onOpen(row.id, event.metaKey || event.ctrlKey)}>
              {row.title || t("Untitled")}
            </button>
          ))}
        </div>
      ) : null}
    </div>
  );
}
