import { describe, expect, it, vi } from "vitest";
import { DIAGRAM_SOURCE_LIMIT, diagramConfig, renderDiagram, solid, type PageLook } from "./mermaid-render";

const look = (dark = false): PageLook => ({
  dark,
  font: "Test Sans",
  color: (name) => (name === "--paper" ? (dark ? "#191919" : "#ffffff") : `#${(name.length * 1234567 % 0xffffff).toString(16).padStart(6, "0")}`),
});

describe("the colours of a diagram", () => {
  it("are the page's, in Mermaid's base theme, and strict", () => {
    const config = diagramConfig(look());
    expect(config.theme).toBe("base");
    expect(config.securityLevel).toBe("strict");
    expect(config.startOnLoad).toBe(false);
    expect(config.fontFamily).toBe("Test Sans");
    const variables = config.themeVariables as Record<string, string>;
    expect(variables.background).toBe("#ffffff");
    // Every colour is one solid `#rrggbb`: Mermaid derives others from them.
    for (const [key, value] of Object.entries(variables)) {
      if (typeof value === "string" && /color|Bkg|bkg|pie\d|cScale\d|background|Border|border/i.test(key) && key !== "fontFamily" && key !== "fontSize") {
        expect(value, key).toMatch(/^#[0-9a-f]{6}$/);
      }
    }
  });

  it("follow the theme: dark mode is said to Mermaid, and the page's own paper is the background", () => {
    const dark = diagramConfig(look(true));
    expect((dark.themeVariables as Record<string, unknown>).darkMode).toBe(true);
    expect((dark.themeVariables as Record<string, string>).background).toBe("#191919");
  });

  it("are blended: a translucent colour over the paper is one solid colour", () => {
    expect(solid("rgba(0, 0, 0, 0.5)", "#ffffff")).toBe("#808080");
    expect(solid("rgb(10, 20, 30)", "#ffffff")).toBe("#0a141e");
    expect(solid("rgba(255, 255, 255, 0.1)", "#000000")).toBe("#1a1a1a");
    expect(solid("not a colour", "#123456")).toBe("#123456");
  });
});

describe("drawing a diagram", () => {
  it("gives Mermaid the page's configuration and returns its drawing", async () => {
    const initialize = vi.fn();
    const render = vi.fn(async (id: string, text: string) => ({ svg: `<svg id="${id}">${text}</svg>`, diagramType: "flowchart", bindFunctions: undefined }));
    const result = await renderDiagram("flowchart LR\n A-->B", look(), async () => ({ initialize, render } as never));
    expect(result).toEqual({ svg: expect.stringContaining("flowchart LR") });
    expect(initialize).toHaveBeenCalledWith(expect.objectContaining({ securityLevel: "strict", theme: "base" }));
  });

  it("says why when Mermaid cannot read the source, and goes on to the next", async () => {
    const render = vi.fn()
      .mockRejectedValueOnce(new Error("Parse error on line 2:\nA--\n^\nExpecting 'ARROW'\nand more\nlines"))
      .mockResolvedValueOnce({ svg: "<svg/>" });
    const load = async () => ({ initialize: vi.fn(), render } as never);
    const bad = await renderDiagram("flowchart\nA--", look(), load);
    expect("error" in bad && bad.error.split("\n")).toHaveLength(4);
    expect(await renderDiagram("flowchart\nA-->B", look(), load)).toEqual({ svg: "<svg/>" });
  });

  it("does not give a source past the limit to Mermaid at all", async () => {
    const load = vi.fn();
    expect(await renderDiagram("x".repeat(DIAGRAM_SOURCE_LIMIT + 1), look(), load)).toEqual({ error: "too long" });
    expect(load).not.toHaveBeenCalled();
  });

  it("is a failed load that is said, not thrown", async () => {
    expect(await renderDiagram("graph TD\nA-->B", look(), async () => { throw new Error("chunk failed"); })).toEqual({ error: "chunk failed" });
  });

  it("draws one at a time, in the order asked", async () => {
    const order: string[] = [];
    let running = 0;
    let most = 0;
    const render = async (_id: string, text: string) => {
      running += 1;
      most = Math.max(most, running);
      await new Promise((resolve) => setTimeout(resolve, 5));
      order.push(text);
      running -= 1;
      return { svg: text };
    };
    const load = async () => ({ initialize: vi.fn(), render } as never);
    await Promise.all(["a", "b", "c"].map((text) => renderDiagram(text, look(), load)));
    expect(order).toEqual(["a", "b", "c"]);
    expect(most).toBe(1);
  });
});
