import { useState } from "react";
import type { DatabaseProperty, NotePage } from "../../core/types";
import { t } from "../../i18n";
import { arcPath, chartSlices, type ChartStyle, type Slice, ringCells } from "./chart-data";

/** A database as a chart: its rows counted by the option each has in a status
 *  or a select. A ring shows each option's share of the rows (the total in its
 *  middle), bars their counts; the ring can also be drawn of pixels or of dots,
 *  on one lattice. The options are listed beside it with both, and one pressed
 *  — a slice, a bar or its line — shows the rows that have it, each of which
 *  opens. Drawn as SVG and CSS, in the colours the options have. */

const SIZE = 200;
const OUTER = 92;
const INNER = 60;
/** The pixels and dots of a ring: the lattice's cell, in drawn units. */
const PIXEL_CELL = 6;
const DOT_CELL = 7;

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
  style: ChartStyle;
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
  const classes = (slice: Slice, base: string) =>
    `${base}${picked === slice.name ? " is-picked" : ""}${picked !== null && picked !== slice.name ? " is-dim" : ""}`;
  const ring = style !== "bar";

  // The slices as arcs (a ring), or as the cells of a ring of pixels or dots.
  let from = 0;
  const arcs = slices.filter((slice) => slice.count > 0).map((slice) => {
    const arc = { slice, path: arcPath(SIZE / 2, SIZE / 2, OUTER, INNER, from, from + slice.share) };
    from += slice.share;
    return arc;
  });
  const cell = style === "pixel" ? PIXEL_CELL : DOT_CELL;
  const cells = style === "pixel" || style === "dots"
    ? ringCells(slices.map((slice) => slice.share), SIZE / 2, SIZE / 2, cell, OUTER, INNER)
    : [];

  return (
    <div className="chart" data-chart={style}>
      <div className="chart__figure">
        {ring ? (
          <svg className={`chart__ring chart__ring--${style}`} viewBox={`0 0 ${SIZE} ${SIZE}`} role="img" aria-label={t("Chart of {name}", { name: property.name })}>
            {total === 0 ? (
              <circle className="chart__empty" cx={SIZE / 2} cy={SIZE / 2} r={(OUTER + INNER) / 2} strokeWidth={OUTER - INNER} />
            ) : style === "donut" ? arcs.map(({ slice, path }) => (
              <path
                key={slice.name || "none"}
                className={classes(slice, "chart__slice")}
                data-tone={slice.tone}
                d={path}
                fillRule="evenodd"
                {...keys(slice)}
              />
            )) : slices.map((slice, index) => slice.count > 0 ? (
              <g
                key={slice.name || "none"}
                className={classes(slice, `chart__slice chart__slice--grid`)}
                data-tone={slice.tone}
                {...keys(slice)}
              >
                {cells.filter((mark) => mark.index === index).map((mark, n) => (style === "pixel" ? (
                  <rect key={n} x={mark.x - (cell - 1.4) / 2} y={mark.y - (cell - 1.4) / 2} width={cell - 1.4} height={cell - 1.4} />
                ) : (
                  <circle key={n} cx={mark.x} cy={mark.y} r={cell * 0.38} />
                )))}
              </g>
            ) : null)}
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
      {ring ? (
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
