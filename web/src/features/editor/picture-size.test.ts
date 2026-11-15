import { describe, expect, it } from "vitest";
import { edgeOf, toolsPlace } from "./pictures";

const box = (left: number, top: number, width: number, height: number) =>
  ({ left, top, width, height, right: left + width, bottom: top + height });
const tools = { width: 150, height: 30 };
const view = { width: 1000, height: 800 };

describe("a picture's side bars", () => {
  it("stand inside each edge, with the middle left to carry it by", () => {
    const picture = box(100, 0, 240, 180);
    expect(edgeOf(picture, 105)).toBe("left");
    expect(edgeOf(picture, 335)).toBe("right");
    expect(edgeOf(picture, 220)).toBeNull();
  });

  it("are still there on a picture made as small as it goes", () => {
    const picture = box(100, 0, 32, 24);
    expect(edgeOf(picture, 104)).toBe("left");
    expect(edgeOf(picture, 128)).toBe("right");
    expect(edgeOf(picture, 116)).toBeNull();
  });

  it("are the whole of a picture narrower than the two of them", () => {
    const picture = box(100, 0, 10, 10);
    expect(edgeOf(picture, 102)).toBe("left");
    expect(edgeOf(picture, 108)).toBe("right");
  });
});

describe("a picture's tools", () => {
  it("stand in its top right corner when it holds them", () => {
    expect(toolsPlace(box(100, 200, 400, 300), tools, undefined, view)).toEqual({ left: 344, top: 206 });
  });

  it("stand above a small picture, not over it", () => {
    const picture = box(100, 200, 32, 24);
    const place = toolsPlace(picture, tools, undefined, view);
    expect(place).toEqual({ left: 100, top: 166 });
    expect(place.top + tools.height).toBeLessThanOrEqual(picture.top);
  });

  it("stand clear of the bars of a picture wide enough but not tall", () => {
    const picture = box(100, 200, 400, 80);
    expect(toolsPlace(picture, tools, undefined, view).top + tools.height).toBeLessThanOrEqual(picture.top);
  });

  it("follow the side the line sets the picture to, and stay in the window", () => {
    expect(toolsPlace(box(900, 200, 40, 30), tools, "right", view).left).toBe(790);
    expect(toolsPlace(box(500, 200, 40, 30), tools, "center", view).left).toBe(445);
    expect(toolsPlace(box(960, 200, 32, 24), tools, undefined, view).left).toBe(842);
    expect(toolsPlace(box(4, 200, 32, 24), tools, "right", view).left).toBe(8);
  });

  it("stand below a small picture at the top of the window", () => {
    expect(toolsPlace(box(100, 10, 32, 24), tools, undefined, view).top).toBe(38);
  });
});
