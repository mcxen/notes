import { escapeHtml } from "../../core/inline-content";
import type { KatexOptions } from "katex";

/** Past this, the source is shown as text. A page someone else wrote must
 *  not be able to make typesetting run away. */
export const MATH_SOURCE_LIMIT = 20_000;

/** Display math, never trusted, never thrown. `maxSize` caps a `\rule` in
 *  ems; `maxExpand` caps macro expansion (KaTeX's own default). HTML only:
 *  the MathML copy would ride along in the chunk for no reader here. */
const OPTIONS: KatexOptions = {
  displayMode: true,
  throwOnError: false,
  trust: false,
  strict: "ignore",
  maxSize: 50,
  maxExpand: 1000,
  output: "html",
};

export interface MathRenderer {
  renderToString(tex: string, options?: KatexOptions): string;
}

export interface RenderedMath {
  html: string;
  /** Set when the source was not typeset: too long, or KaTeX could not. */
  error?: string;
}

let loading: Promise<MathRenderer> | null = null;

/** The formula chunk, once, the first time a page draws an equation. A
 *  failed fetch is forgotten so the next equation tries again. */
export function loadKatex(): Promise<MathRenderer> {
  loading ??= Promise.all([import("katex"), import("katex/dist/katex.min.css")])
    .then(([katex]) => katex)
    .catch((error: unknown) => {
      loading = null;
      throw error;
    });
  return loading;
}

/** Typeset `source`, or the source itself when it cannot be. Never throws. */
export function renderMath(katex: MathRenderer, source: string): RenderedMath {
  if (source.length > MATH_SOURCE_LIMIT) {
    return { html: `<span class="katex-error">${escapeHtml(source)}</span>`, error: "too long" };
  }
  try {
    const html = katex.renderToString(source, OPTIONS);
    // throwOnError is off: a bad formula comes back as HTML, marked, not thrown.
    if (html.includes("katex-error")) return { html, error: "parse" };
    return { html };
  } catch (error) {
    const message = error instanceof Error ? error.message : "render";
    return { html: `<span class="katex-error">${escapeHtml(source)}</span>`, error: message };
  }
}
