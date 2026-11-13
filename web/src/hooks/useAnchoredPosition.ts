import { useLayoutEffect, useState, type CSSProperties, type RefObject } from "react";

export interface AnchorRect {
  top: number;
  left: number;
  bottom: number;
  right: number;
  width: number;
  height: number;
}

export type Placement =
  | "bottom-start"
  | "bottom-end"
  | "top-start"
  | "top-end"
  | "right-start"
  | "left-start";

const VIEWPORT_MARGIN = 8;

export function rectOf(element: Element): AnchorRect {
  const { top, left, bottom, right, width, height } = element.getBoundingClientRect();
  return { top, left, bottom, right, width, height };
}

export function pointRect(x: number, y: number): AnchorRect {
  return { top: y, left: x, bottom: y, right: x, width: 0, height: 0 };
}

/** A caret as an anchor: no wider than a point, but as tall as its line — so
 *  a menu hung from it that flips above starts over the line, not on it. */
export function caretAnchor(rect: { left: number; top: number; bottom: number } | null): AnchorRect {
  if (!rect) return pointRect(0, 0);
  return { top: rect.top, left: rect.left, bottom: rect.bottom, right: rect.left, width: 0, height: rect.bottom - rect.top };
}

/** `anchor` moved with what it hangs from. */
export function shiftedRect(anchor: AnchorRect, dx: number, dy: number): AnchorRect {
  return { ...anchor, top: anchor.top + dy, bottom: anchor.bottom + dy, left: anchor.left + dx, right: anchor.right + dx };
}

/** The least a panel is ever squeezed to, however little room there is. */
const MIN_ROOM = 96;

function resolve(
  anchor: AnchorRect,
  size: { width: number; height: number },
  placement: Placement,
  gap: number,
): { left: number; top: number; placement: Placement; room: number | null } {
  const vertical = placement.startsWith("top") || placement.startsWith("bottom");
  let side = placement.split("-")[0];
  // Above or below, a panel is never taller than the roomier side: one that
  // fits neither side would otherwise be pushed back over its own anchor.
  let room: number | null = null;
  if (vertical) {
    const wantsTop = side === "top";
    const roomBelow = window.innerHeight - anchor.bottom - gap - VIEWPORT_MARGIN;
    const roomAbove = anchor.top - gap - VIEWPORT_MARGIN;
    // Flip only when the preferred side cannot hold the panel and the other can.
    if (wantsTop && roomAbove < size.height && roomBelow > roomAbove) side = "bottom";
    if (!wantsTop && roomBelow < size.height && roomAbove > roomBelow) side = "top";
    room = Math.max(MIN_ROOM, Math.floor(Math.max(roomBelow, roomAbove)));
  } else {
    const wantsLeft = side === "left";
    const roomRight = window.innerWidth - anchor.right - gap - VIEWPORT_MARGIN;
    const roomLeft = anchor.left - gap - VIEWPORT_MARGIN;
    if (wantsLeft && roomLeft < size.width && roomRight > roomLeft) side = "right";
    if (!wantsLeft && roomRight < size.width && roomLeft > roomRight) side = "left";
  }

  const alignEnd = placement.endsWith("-end");
  let left = vertical
    ? (alignEnd ? anchor.right - size.width : anchor.left)
    : (side === "left" ? anchor.left - size.width - gap : anchor.right + gap);
  let top = vertical
    ? (side === "top" ? anchor.top - size.height - gap : anchor.bottom + gap)
    : anchor.top;

  left = Math.min(Math.max(VIEWPORT_MARGIN, left), Math.max(VIEWPORT_MARGIN, window.innerWidth - size.width - VIEWPORT_MARGIN));
  top = Math.min(Math.max(VIEWPORT_MARGIN, top), Math.max(VIEWPORT_MARGIN, window.innerHeight - size.height - VIEWPORT_MARGIN));
  return { left, top, placement: `${side}-${alignEnd ? "end" : "start"}` as Placement, room };
}

/** Pins a fixed-position floating element to an anchor rectangle, flipping to
 *  the opposite side when the preferred one would run off screen. Re-measures
 *  whenever the panel resizes, which is what keeps a filtering slash menu from
 *  drifting as its result list shrinks. */
export function useAnchoredPosition(
  ref: RefObject<HTMLElement | null>,
  anchor: AnchorRect | null,
  placement: Placement = "bottom-start",
  gap = 6,
): { style: CSSProperties; placement: Placement } {
  const [state, setState] = useState<{ left: number; top: number; placement: Placement; room: number | null } | null>(null);

  useLayoutEffect(() => {
    const element = ref.current;
    if (!element || !anchor) return;
    const measure = () => {
      const size = { width: element.offsetWidth, height: element.offsetHeight };
      if (!size.width || !size.height) return;
      setState((current) => {
        const next = resolve(anchor, size, placement, gap);
        if (current && current.left === next.left && current.top === next.top && current.placement === next.placement && current.room === next.room) {
          return current;
        }
        return next;
      });
    };
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(element);
    window.addEventListener("resize", measure);
    return () => {
      observer.disconnect();
      window.removeEventListener("resize", measure);
    };
  }, [anchor, gap, placement, ref]);

  return {
    style: state
      ? {
        position: "fixed",
        left: `${state.left}px`,
        top: `${state.top}px`,
        // What a menu inside may grow to (`.menu`'s max-height reads it).
        ...(state.room === null ? {} : { "--pop-room": `${state.room}px` }),
      } as CSSProperties
      : { position: "fixed", left: 0, top: 0, opacity: 0, pointerEvents: "none" },
    placement: state?.placement ?? placement,
  };
}
