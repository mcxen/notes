import type { DatabaseProperty, NotePage } from "../../core/types";
import { statusOf, statusOptions } from "./database-model";

/* What a chart of a database counts: the rows by the option each has in one
   status or select property, one slice for each option, in the order the
   property keeps them, and one for the rows that have none. */

/** The tones a select's options take in turn: a select keeps no colours of its
 *  own, a chart gives each option one so the slices tell apart. */
const TONES = ["blue", "green", "orange", "purple", "pink", "yellow", "red", "brown", "gray"] as const;

export interface Slice {
  /** The option's name; "" for rows with none. */
  name: string;
  count: number;
  /** A fraction of one: its share of the rows counted. */
  share: number;
  /** The tone the option is drawn in (`data-tone`): a status's own colour. */
  tone: string;
  rows: NotePage[];
}

/** The rows counted by `property`, a slice for each of its options (all of
 *  them, even those no row has) and one more, last, for rows with none or with
 *  a value that is no option any more. */
export function chartSlices(rows: readonly NotePage[], property: DatabaseProperty): Slice[] {
  const status = property.type === "status";
  // A status's options go To-do, In progress, Complete, as its writers keep them.
  const rank = (group: string) => ["todo", "progress", "complete"].indexOf(group);
  const options = status
    ? [...statusOptions(property)].sort((a, b) => rank(a.group) - rank(b.group)).map((option) => option.name)
    : (property.options ?? []);
  const byName = new Map<string, NotePage[]>(options.map((name) => [name, []]));
  const none: NotePage[] = [];
  for (const row of rows) {
    const stored = row.properties?.[property.id] ?? "";
    // A status always reads as one of its options: unset, its default.
    const name = status ? statusOf(property, stored).name : stored.trim();
    const bucket = byName.get(name);
    if (bucket) bucket.push(row);
    else none.push(row);
  }
  const total = rows.length || 1;
  const slices: Slice[] = options.map((name, index) => ({
    name,
    count: byName.get(name)!.length,
    share: byName.get(name)!.length / total,
    tone: status ? statusOf(property, name).color : TONES[index % TONES.length],
    rows: byName.get(name)!,
  }));
  if (none.length) slices.push({ name: "", count: none.length, share: none.length / total, tone: "default", rows: none });
  return slices;
}

/** One arc of a ring, from `from` to `to` (fractions of a turn, from the top,
 *  clockwise), as an SVG path between two radii. A whole turn is two halves,
 *  which a single arc cannot draw. */
export function arcPath(cx: number, cy: number, outer: number, inner: number, from: number, to: number): string {
  const turn = Math.PI * 2;
  const point = (radius: number, fraction: number) => {
    const angle = fraction * turn - Math.PI / 2;
    return [cx + radius * Math.cos(angle), cy + radius * Math.sin(angle)] as const;
  };
  const span = Math.min(Math.max(to - from, 0), 1);
  if (span >= 0.9999) {
    const [top] = [point(outer, 0)];
    const [bottom] = [point(outer, 0.5)];
    const [itop, ibottom] = [point(inner, 0), point(inner, 0.5)];
    return [
      `M ${top[0]} ${top[1]} A ${outer} ${outer} 0 1 1 ${bottom[0]} ${bottom[1]} A ${outer} ${outer} 0 1 1 ${top[0]} ${top[1]}`,
      `M ${itop[0]} ${itop[1]} A ${inner} ${inner} 0 1 0 ${ibottom[0]} ${ibottom[1]} A ${inner} ${inner} 0 1 0 ${itop[0]} ${itop[1]} Z`,
    ].join(" ");
  }
  const large = span > 0.5 ? 1 : 0;
  const [x1, y1] = point(outer, from);
  const [x2, y2] = point(outer, from + span);
  const [x3, y3] = point(inner, from + span);
  const [x4, y4] = point(inner, from);
  return `M ${x1} ${y1} A ${outer} ${outer} 0 ${large} 1 ${x2} ${y2} L ${x3} ${y3} A ${inner} ${inner} 0 ${large} 0 ${x4} ${y4} Z`;
}

/** How a chart is drawn: a ring or bars (the first two), or a ring made of
 *  pixels (square cells) or of dots, each on the same lattice. */
export type ChartStyle = "donut" | "bar" | "pixel" | "dots";

/** A cell of a ring drawn from squares or dots: its centre, and the slice it
 *  falls in (`index`, into the shares given). */
export interface RingCell {
  x: number;
  y: number;
  index: number;
}

/** The cells of a ring on a lattice of `cell`-sized squares centred on the
 *  ring's middle: every cell whose centre lies between `inner` and `outer`
 *  from it, and the slice it falls in, by the shares (fractions of a turn,
 *  from the top, clockwise). The lattice is symmetric about both axes, so
 *  two equal shares get equal cells. */
export function ringCells(shares: readonly number[], cx: number, cy: number, cell: number, outer: number, inner: number): RingCell[] {
  const cuts: number[] = [];
  let sum = 0;
  for (const share of shares) {
    sum += share;
    cuts.push(sum);
  }
  // The last cut takes whatever rounding leaves over, so no cell is lost.
  if (cuts.length) cuts[cuts.length - 1] = Infinity;
  const reach = Math.ceil(outer / cell) + 1;
  const cells: RingCell[] = [];
  for (let i = -reach; i < reach; i += 1) {
    for (let j = -reach; j < reach; j += 1) {
      const dx = (i + 0.5) * cell;
      const dy = (j + 0.5) * cell;
      const radius = Math.hypot(dx, dy);
      if (radius < inner || radius > outer) continue;
      // Clockwise from the top: nought at the top, a quarter at the right.
      const turn = (Math.atan2(dx, -dy) / (Math.PI * 2) + 1) % 1;
      const index = cuts.findIndex((cut) => turn < cut);
      if (index >= 0) cells.push({ x: cx + dx, y: cy + dy, index });
    }
  }
  return cells;
}
