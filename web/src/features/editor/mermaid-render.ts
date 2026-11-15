import type { MermaidConfig } from "mermaid";

/* Mermaid diagrams, drawn in the page's own colours and type. The library is
   large, so it loads the first time a page draws a diagram, and not before. The
   source is anyone's (pasted, synced, imported): it is read in Mermaid's strict
   mode, which sanitizes what a label holds and takes no click handlers, and a
   source past the limit is not given to it. */

/** Past this, the source is shown as text. */
export const DIAGRAM_SOURCE_LIMIT = 20_000;

/** What the diagram's colours are read from: names of the page's CSS colours,
 *  each as one solid colour (the page blends the translucent ones over its
 *  paper), and whether the page is dark. */
export interface PageLook {
  dark: boolean;
  /** A colour of the page by its custom property's name, as `#rrggbb`. */
  color(name: string): string;
  /** The page's body type, as a CSS font-family. */
  font: string;
}

/** Mermaid's `base` theme with every colour it uses given by the page: nodes in
 *  the soft blue of a status pill, notes in yellow, lines in the page's quiet
 *  ink — so a diagram reads as part of the page, in either theme. */
export function diagramConfig(look: PageLook): MermaidConfig {
  const c = look.color;
  const ink = c("--ink");
  const quiet = c("--ink-3");
  const paper = c("--paper");
  // The diagram sits on a block's own fill, so what is behind a label is that.
  const card = c("--fill");
  const node = c("--tag-blue-bg");
  const border = look.dark ? c("--text-blue") : c("--tag-blue-ink");
  const soft = c("--fill-hover");
  const edge = c("--hairline-strong");
  const note = c("--tag-yellow-bg");
  const tones = ["blue", "green", "orange", "purple", "pink", "yellow", "red", "brown", "gray"].map((tone) => c(`--tag-${tone}-bg`));
  const dots = ["blue", "green", "orange", "purple", "pink", "yellow", "red", "brown", "gray"].map((tone) => c(`--text-${tone}`));
  return {
    startOnLoad: false,
    securityLevel: "strict",
    suppressErrorRendering: true,
    theme: "base",
    fontFamily: look.font,
    themeVariables: {
      darkMode: look.dark,
      fontFamily: look.font,
      fontSize: "14px",
      dropShadow: "none",
      background: paper,
      primaryColor: node,
      primaryTextColor: ink,
      primaryBorderColor: border,
      secondaryColor: c("--tag-green-bg"),
      secondaryTextColor: ink,
      secondaryBorderColor: c("--text-green"),
      tertiaryColor: soft,
      tertiaryTextColor: ink,
      tertiaryBorderColor: edge,
      lineColor: quiet,
      textColor: ink,
      mainBkg: node,
      nodeBorder: border,
      nodeTextColor: ink,
      clusterBkg: soft,
      clusterBorder: edge,
      titleColor: ink,
      edgeLabelBackground: card,
      noteBkgColor: note,
      noteTextColor: ink,
      noteBorderColor: c("--text-yellow"),
      actorBkg: node,
      actorBorder: border,
      actorTextColor: ink,
      actorLineColor: edge,
      signalColor: quiet,
      signalTextColor: ink,
      labelBoxBkgColor: soft,
      labelBoxBorderColor: edge,
      labelTextColor: ink,
      loopTextColor: ink,
      activationBkgColor: soft,
      activationBorderColor: border,
      sequenceNumberColor: paper,
      classText: ink,
      altBackground: soft,
      stateBkg: node,
      stateLabelColor: ink,
      labelColor: ink,
      transitionColor: quiet,
      transitionLabelColor: ink,
      specialStateColor: quiet,
      compositeBackground: soft,
      compositeBorder: edge,
      compositeTitleBackground: node,
      attributeBackgroundColorOdd: card,
      attributeBackgroundColorEven: soft,
      pie1: dots[0], pie2: dots[1], pie3: dots[2], pie4: dots[3], pie5: dots[4], pie6: dots[5], pie7: dots[6], pie8: dots[7], pie9: dots[8],
      pieTitleTextColor: ink,
      pieSectionTextColor: look.dark ? ink : paper,
      pieLegendTextColor: ink,
      pieStrokeColor: card,
      pieOuterStrokeColor: edge,
      cScale0: tones[0], cScale1: tones[1], cScale2: tones[2], cScale3: tones[3], cScale4: tones[4], cScale5: tones[5],
      cScaleLabel0: ink, cScaleLabel1: ink, cScaleLabel2: ink, cScaleLabel3: ink, cScaleLabel4: ink, cScaleLabel5: ink,
    },
    themeCSS: DIAGRAM_CSS,
    // Labels are SVG text, not HTML in a foreignObject: a diagram saved as a
    // picture (diagram-export.ts) is then drawn the same by any web view.
    htmlLabels: false,
    flowchart: { curve: "basis", padding: 12, htmlLabels: false },
    sequence: { mirrorActors: false, useMaxWidth: true },
    // A chart is drawn to the width it needs: a Gantt shrunk to the block would have unreadable type.
    gantt: { useMaxWidth: false, barHeight: 22, fontSize: 12, leftPadding: 70, axisFormat: "%m-%d" },
    maxTextSize: DIAGRAM_SOURCE_LIMIT,
    maxEdges: 400,
  };
}

/** What Mermaid's own look is changed to, for the page's: rounded nodes, thinner
 *  lines, the page's weight of type. Kept small: Mermaid renames its classes
 *  now and then, and the theme variables above do the rest. */
const DIAGRAM_CSS = `
  .node rect, .node polygon, .node path, .actor, .classBox rect, .statediagram-state rect { rx: 6px; ry: 6px; stroke-width: 1px; }
  .cluster rect { rx: 8px; ry: 8px; stroke-width: 1px; stroke-dasharray: 4 3; }
  .edgePath .path, .flowchart-link, .messageLine0, .messageLine1, .relation, .transition { stroke-width: 1.4px; }
  .label, .nodeLabel, .edgeLabel, text { font-weight: 400; }
  .edgeLabel { padding: 2px 4px; }
  .note { stroke-width: 1px; }
  .node rect, .node polygon, .node path, .node circle, .actor, .note, .cluster rect, .label-container { filter: none !important; }
`;

type Mermaid = (typeof import("mermaid"))["default"];

let loading: Promise<Mermaid> | null = null;

/** The library, once. A failed fetch is forgotten, so the next diagram tries again. */
export function loadMermaid(): Promise<Mermaid> {
  loading ??= import("mermaid")
    .then((module) => module.default)
    .catch((error: unknown) => {
      loading = null;
      throw error;
    });
  return loading;
}

export type Diagram = { svg: string } | { error: string };

/* Mermaid draws one diagram at a time (its configuration is global), so draws
   wait in line. */
let line: Promise<unknown> = Promise.resolve();
let counter = 0;

/** Draws `source`, or says why it cannot be: a source too long, or one Mermaid
 *  cannot read. Never throws. */
export function renderDiagram(source: string, look: PageLook, load: () => Promise<Pick<Mermaid, "initialize" | "render">> = loadMermaid): Promise<Diagram> {
  if (source.length > DIAGRAM_SOURCE_LIMIT) return Promise.resolve({ error: "too long" });
  const draw = async (): Promise<Diagram> => {
    const id = `mermaid-${(counter += 1)}`;
    try {
      const mermaid = await load();
      mermaid.initialize(diagramConfig(look));
      const { svg } = await mermaid.render(id, source);
      return { svg };
    } catch (error) {
      // Mermaid may leave the element it was drawing in behind.
      if (typeof document !== "undefined") document.getElementById(`d${id}`)?.remove();
      const message = error instanceof Error ? error.message : String(error);
      return { error: message.split("\n").slice(0, 4).join("\n") };
    }
  };
  const result = line.then(draw, draw);
  line = result;
  return result;
}

/** One solid colour of the page: the custom property resolved by the browser
 *  and, when it is translucent, laid over the page's paper. */
export function pageColor(name: string, paper: string): string {
  const probe = document.createElement("span");
  probe.style.color = `var(${name})`;
  probe.style.display = "none";
  document.documentElement.appendChild(probe);
  const resolved = getComputedStyle(probe).color;
  probe.remove();
  return solid(resolved, paper);
}

/** `rgb(…)` or `rgba(…)` as `#rrggbb`, over `under` (a `#rrggbb`) when it is translucent. */
export function solid(color: string, under: string): string {
  const parts = /rgba?\(\s*([\d.]+)[ ,]+([\d.]+)[ ,]+([\d.]+)(?:[ ,/]+([\d.]+%?))?\s*\)/.exec(color);
  if (!parts) return under;
  const alpha = parts[4] === undefined ? 1 : parts[4].endsWith("%") ? Number.parseFloat(parts[4]) / 100 : Number.parseFloat(parts[4]);
  const base = [1, 3, 5].map((at) => Number.parseInt(under.slice(at, at + 2), 16));
  const mixed = [1, 2, 3].map((at, i) => Math.round(Number.parseFloat(parts[at]) * alpha + (Number.isFinite(base[i]) ? base[i] : 255) * (1 - alpha)));
  return `#${mixed.map((value) => Math.min(255, Math.max(0, value)).toString(16).padStart(2, "0")).join("")}`;
}

/** The page's look now: its colours in the theme it has, its body type. */
export function currentLook(): PageLook {
  const dark = document.documentElement.getAttribute("data-theme") === "dark";
  const paperProbe = document.createElement("span");
  paperProbe.style.color = "var(--paper)";
  paperProbe.style.display = "none";
  document.documentElement.appendChild(paperProbe);
  const paper = solid(getComputedStyle(paperProbe).color, "#ffffff");
  paperProbe.remove();
  const font = getComputedStyle(document.body).fontFamily || "sans-serif";
  return { dark, font, color: (name) => (name === "--paper" ? paper : pageColor(name, paper)) };
}
