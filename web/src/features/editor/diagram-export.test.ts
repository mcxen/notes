import { describe, expect, it } from "vitest";
import { diagramSize, standaloneSvg } from "./diagram-export";

const drawn = '<svg id="mermaid-1" width="100%" xmlns="http://www.w3.org/2000/svg" viewBox="0 0 320.5 140" style="max-width: 320.5px; background-color: white;" role="graphics-document"><g/></svg>';

describe("a diagram as a file", () => {
  it("is as large as its viewBox says", () => {
    expect(diagramSize(drawn)).toEqual({ width: 320.5, height: 140 });
    expect(diagramSize("<svg></svg>")).toBeNull();
    expect(diagramSize('<svg viewBox="0 0 0 10"></svg>')).toBeNull();
  });

  it("gets a size of its own and loses the block's max-width", () => {
    const svg = standaloneSvg(drawn, { scale: 2 });
    expect(svg).toContain('width="641" height="280"');
    expect(svg).not.toContain("max-width");
    expect(svg).not.toContain('width="100%"');
    expect(svg).toContain('viewBox="0 0 320.5 140"');
  });

  it("is painted on the page's paper when it is given one", () => {
    expect(standaloneSvg(drawn, { background: "#191919" })).toContain('<rect x="0" y="0" width="320.5" height="140" fill="#191919"/>');
    expect(standaloneSvg(drawn)).not.toContain("<rect");
  });

  it("says what it is even when Mermaid left the namespace out", () => {
    expect(standaloneSvg('<svg viewBox="0 0 10 10"><g/></svg>')).toContain('xmlns="http://www.w3.org/2000/svg"');
  });

  it("leaves a drawing it cannot size as it is", () => {
    expect(standaloneSvg("<svg></svg>")).toBe("<svg></svg>");
  });
});
