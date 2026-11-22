import { afterEach, describe, expect, it } from "vitest";
import type { BlockKind } from "../../core/types";
import type { ClipboardPreview } from "../../platform/mygo";
import {
  CLIPBOARD_TTL_MS,
  cycleClipboardItem,
  dismissClipboardItem,
  getClipboardSnapshot,
  isClipboardGhostEligible,
  resetClipboardStoreForTest,
  setClipboardState,
  type GhostEligibility,
} from "./clipboard-store";

const item = (n: number, ageMs = 0): ClipboardPreview => ({
  id: `clip-${n}`,
  kind: "text",
  preview: `snippet ${n}`,
  copiedAt: new Date(Date.now() - ageMs).toISOString(),
});

const baseEligibility: GhostEligibility = {
  enabled: true,
  hasItem: true,
  readOnly: false,
  embed: false,
  blockKind: "text",
  empty: true,
  focused: true,
  hasSlash: false,
  hasMention: false,
  hasTimeMenu: false,
  hasBlockSelection: false,
  composing: false,
};

afterEach(() => {
  resetClipboardStoreForTest();
});

describe("clipboard ghost eligibility", () => {
  it("shows only on focused empty plain paragraphs", () => {
    expect(isClipboardGhostEligible(baseEligibility)).toBe(true);
  });

  it("never shows on list items or any non-paragraph block kind", () => {
    const disallowed: BlockKind[] = [
      "bullet",
      "numbered",
      "todo",
      "toggle",
      "heading1",
      "heading2",
      "heading3",
      "quote",
      "callout",
      "code",
      "math",
      "table",
      "file",
      "embed",
      "divider",
      "page",
      "columns",
      "column",
    ];
    for (const blockKind of disallowed) {
      expect(isClipboardGhostEligible({ ...baseEligibility, blockKind }), blockKind).toBe(false);
    }
  });

  it("suppresses the ghost whenever editing overlays, selection, IME, or readOnly apply", () => {
    expect(isClipboardGhostEligible({ ...baseEligibility, enabled: false })).toBe(false);
    expect(isClipboardGhostEligible({ ...baseEligibility, hasItem: false })).toBe(false);
    expect(isClipboardGhostEligible({ ...baseEligibility, readOnly: true })).toBe(false);
    expect(isClipboardGhostEligible({ ...baseEligibility, embed: true })).toBe(false);
    expect(isClipboardGhostEligible({ ...baseEligibility, empty: false })).toBe(false);
    expect(isClipboardGhostEligible({ ...baseEligibility, focused: false })).toBe(false);
    expect(isClipboardGhostEligible({ ...baseEligibility, hasSlash: true })).toBe(false);
    expect(isClipboardGhostEligible({ ...baseEligibility, hasMention: true })).toBe(false);
    expect(isClipboardGhostEligible({ ...baseEligibility, hasTimeMenu: true })).toBe(false);
    expect(isClipboardGhostEligible({ ...baseEligibility, hasBlockSelection: true })).toBe(false);
    expect(isClipboardGhostEligible({ ...baseEligibility, composing: true })).toBe(false);
  });
});

describe("clipboard 5-item queue store", () => {
  it("cycles through up to 5 recent items and wraps around", () => {
    const five = [item(5), item(4), item(3), item(2), item(1)];
    setClipboardState({ enabled: true, items: five });

    expect(getClipboardSnapshot().activeIndex).toBe(0);
    expect(getClipboardSnapshot().activeItem?.id).toBe("clip-5");

    cycleClipboardItem(1);
    expect(getClipboardSnapshot().activeItem?.id).toBe("clip-4");

    cycleClipboardItem(-1);
    expect(getClipboardSnapshot().activeItem?.id).toBe("clip-5");

    cycleClipboardItem(-1);
    expect(getClipboardSnapshot().activeItem?.id).toBe("clip-1");
  });

  it("resets activeIndex when a new head arrives and preserves selection otherwise", () => {
    setClipboardState({ enabled: true, items: [item(3), item(2), item(1)] });
    cycleClipboardItem(1);
    expect(getClipboardSnapshot().activeItem?.id).toBe("clip-2");

    // Removing the oldest item keeps "clip-2" selected.
    setClipboardState({ enabled: true, items: [item(3), item(2)] });
    expect(getClipboardSnapshot().activeItem?.id).toBe("clip-2");

    // New head arrives -> switches immediately to the newest item.
    setClipboardState({ enabled: true, items: [item(4), item(3), item(2)] });
    expect(getClipboardSnapshot().activeIndex).toBe(0);
    expect(getClipboardSnapshot().activeItem?.id).toBe("clip-4");
  });

  it("filters expired entries and handles dismissal and disable", () => {
    setClipboardState({
      enabled: true,
      items: [item(2), item(1, CLIPBOARD_TTL_MS + 1000)],
    });
    expect(getClipboardSnapshot().items.map((x) => x.id)).toEqual(["clip-2"]);

    dismissClipboardItem("clip-2");
    expect(getClipboardSnapshot().items).toEqual([]);
    expect(getClipboardSnapshot().activeItem).toBeNull();

    setClipboardState({ enabled: true, items: [item(9)] });
    setClipboardState({ enabled: false, items: [item(9)] });
    expect(getClipboardSnapshot().enabled).toBe(false);
    expect(getClipboardSnapshot().items).toEqual([]);
  });
});
