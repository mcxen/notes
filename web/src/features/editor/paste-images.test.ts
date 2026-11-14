import { describe, expect, it } from "vitest";
import type { ImageSlot } from "./html-paste";
import { settleImages } from "./paste-images";

const slot = (n: number): ImageSlot => ({ blockId: `b${n}`, src: `https://example.com/${n}.png`, label: `n${n}` });
const file = (name: string) => new File(["x"], name, { type: "image/png" });

describe("the pictures of a pasted page", () => {
  it("are fetched a few at a time and each is placed as it comes", async () => {
    let running = 0;
    let most = 0;
    const placed: string[] = [];
    const missed = await settleImages(
      [1, 2, 3, 4, 5, 6, 7].map(slot),
      () => true,
      async (f, image) => { placed.push(`${image.blockId}:${f.name}`); },
      async (src) => {
        running += 1;
        most = Math.max(most, running);
        await new Promise((resolve) => setTimeout(resolve, 2));
        running -= 1;
        return src.endsWith("4.png") ? null : file(src.split("/").pop() as string);
      },
      3,
    );
    expect(most).toBe(3);
    expect(missed).toBe(1);
    expect(placed.sort()).toEqual(["b1:1.png", "b2:2.png", "b3:3.png", "b5:5.png", "b6:6.png", "b7:7.png"]);
  });

  it("are not placed on a line that was changed while they came", async () => {
    const placed: string[] = [];
    const missed = await settleImages(
      [slot(1), slot(2)],
      (image) => image.blockId !== "b1",
      async (_f, image) => { placed.push(image.blockId); },
      async () => file("a.png"),
    );
    expect(placed).toEqual(["b2"]);
    expect(missed).toBe(0);
  });

  it("need nothing when a paste names none", async () => {
    expect(await settleImages([], () => true, async () => {}, async () => null)).toBe(0);
  });
});
