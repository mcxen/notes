import { loadKatex, renderMath } from "../editor/math-render";
import { currentLook, renderDiagram } from "../editor/mermaid-render";
import type { Drawn } from "./publisher";

/** Typesets the equations and draws the diagrams a publish found, in the look the
 *  page has now: a diagram sits on the page's own paper, so a theme's colours stay
 *  together. A source that cannot be drawn is left to the plain code. */
export async function drawForPublish(sources: { math: string[]; diagram: string[] }): Promise<Drawn> {
  const math = new Map<string, string>();
  const diagram = new Map<string, string>();
  if (sources.math.length) {
    const katex = await loadKatex();
    for (const source of sources.math) {
      const typeset = renderMath(katex, source);
      if (!typeset.error) math.set(source, typeset.html);
    }
  }
  if (sources.diagram.length) {
    const look = currentLook();
    const paper = look.color("--paper");
    for (const source of sources.diagram) {
      const drawn = await renderDiagram(source, look);
      if ("svg" in drawn) diagram.set(source, `<div class="diagram" style="background:${paper}">${drawn.svg}</div>`);
    }
  }
  return { math: (source) => math.get(source) ?? null, diagram: (source) => diagram.get(source) ?? null };
}
