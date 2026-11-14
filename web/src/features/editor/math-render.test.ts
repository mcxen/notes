import { renderToString } from "katex";
import { describe, expect, it } from "vitest";
import { MATH_SOURCE_LIMIT, renderMath, type MathRenderer } from "./math-render";

const katex: MathRenderer = { renderToString };

describe("renderMath", () => {
  it("typesets a formula", () => {
    const rendered = renderMath(katex, "E=mc^2");
    expect(rendered.error).toBeUndefined();
    expect(rendered.html).toContain("katex");
    expect(rendered.html).not.toContain("katex-error");
  });

  it("shows a formula KaTeX cannot parse", () => {
    const rendered = renderMath(katex, "\\frac{");
    expect(rendered.error).toBe("parse");
    expect(rendered.html).toContain("katex-error");
  });

  it("shows a source past the limit as text", () => {
    const source = "a".repeat(MATH_SOURCE_LIMIT + 1);
    const rendered = renderMath(katex, source);
    expect(rendered.error).toBe("too long");
    expect(rendered.html).not.toContain("katex-html");
    expect(rendered.html).toContain(source);
  });

  it("returns the source when the renderer throws", () => {
    const broken: MathRenderer = {
      renderToString() {
        throw new Error("boom");
      },
    };
    const rendered = renderMath(broken, "a<b");
    expect(rendered.error).toBe("boom");
    expect(rendered.html).toContain("a&lt;b");
    expect(rendered.html).not.toContain("a<b");
  });

  it("stops a macro that expands without end", () => {
    const rendered = renderMath(katex, "\\def\\x{\\x}\\x");
    expect(rendered.html.length).toBeGreaterThan(0);
    expect(rendered.error).toBeDefined();
  });

  it("does not follow a link or an image in the source", () => {
    const href = renderMath(katex, "\\href{javascript:alert(1)}{x}");
    expect(href.html).not.toMatch(/href\s*=\s*["']javascript:/i);
    const image = renderMath(katex, "\\includegraphics{https://example.com/a.png}");
    expect(image.html).not.toContain("<img");
  });
});
