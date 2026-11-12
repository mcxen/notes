import { describe, expect, it } from "vitest";
import { blockShortcut } from "./block-registry";
import { kindForKeys } from "./block-shortcuts";

const keys = (code: string, mods: Partial<{ metaKey: boolean; ctrlKey: boolean; shiftKey: boolean; altKey: boolean }> = {}) =>
  ({ code, metaKey: false, ctrlKey: false, shiftKey: false, altKey: false, ...mods });

describe("keys that set a block's kind", () => {
  it("⌘1, ⌘2, ⌘3 are headings", () => {
    expect(kindForKeys(keys("Digit1", { metaKey: true }))).toBe("heading1");
    expect(kindForKeys(keys("Digit2", { ctrlKey: true }))).toBe("heading2");
    expect(kindForKeys(keys("Digit3", { metaKey: true }))).toBe("heading3");
    expect(kindForKeys(keys("Digit4", { metaKey: true }))).toBeNull();
  });

  it("⌘⌥0–8 are Notion's", () => {
    const wanted = ["text", "heading1", "heading2", "heading3", "todo", "bullet", "numbered", "toggle", "code"];
    wanted.forEach((kind, digit) => expect(kindForKeys(keys(`Digit${digit}`, { metaKey: true, altKey: true }))).toBe(kind));
    expect(kindForKeys(keys("Digit9", { metaKey: true, altKey: true }))).toBeNull();
  });

  it("⌘⇧7, 8, 9 are the lists and the to-do", () => {
    expect(kindForKeys(keys("Digit7", { metaKey: true, shiftKey: true }))).toBe("numbered");
    expect(kindForKeys(keys("Digit8", { metaKey: true, shiftKey: true }))).toBe("bullet");
    expect(kindForKeys(keys("Digit9", { metaKey: true, shiftKey: true }))).toBe("todo");
  });

  it("need ⌘ or Ctrl, and no other mix", () => {
    expect(kindForKeys(keys("Digit1"))).toBeNull();
    expect(kindForKeys(keys("Digit1", { altKey: true }))).toBeNull();
    expect(kindForKeys(keys("Digit1", { metaKey: true, altKey: true, shiftKey: true }))).toBeNull();
    expect(kindForKeys(keys("KeyA", { metaKey: true }))).toBeNull();
  });
});

describe("what typed Markdown turns into", () => {
  it("is a toggle for >>, and a quote for >", () => {
    expect(blockShortcut(">>")).toEqual({ kind: "toggle" });
    expect(blockShortcut(">")).toEqual({ kind: "quote" });
    expect(blockShortcut("-")).toEqual({ kind: "bullet" });
  });
});
