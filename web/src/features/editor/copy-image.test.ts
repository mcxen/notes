import { describe, expect, it } from "vitest";
import { backgroundUrl, isWebKit } from "./copy-image";

describe("the address of a background", () => {
  it("is read from url() with or without quotes", () => {
    expect(backgroundUrl('url("vault://localhost/v_1/files/p/a.png")')).toBe("vault://localhost/v_1/files/p/a.png");
    expect(backgroundUrl("url(blob:http://x/1)")).toBe("blob:http://x/1");
    expect(backgroundUrl("url('a b.png')")).toBe("a b.png");
  });

  it("is null when there is none", () => {
    expect(backgroundUrl("")).toBeNull();
    expect(backgroundUrl("none")).toBeNull();
    expect(backgroundUrl("linear-gradient(red, blue)")).toBeNull();
  });
});

describe("telling WebKit from Chromium", () => {
  it("knows a macOS web view, which has no Safari in its agent", () => {
    expect(isWebKit("Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko)")).toBe(true);
    expect(isWebKit("Mozilla/5.0 (Macintosh) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Safari/605.1.15")).toBe(true);
  });

  it("knows Chromium and Edge", () => {
    expect(isWebKit("Mozilla/5.0 (Windows NT 10.0) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0 Safari/537.36")).toBe(false);
    expect(isWebKit("Mozilla/5.0 (Windows NT 10.0) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0 Safari/537.36 Edg/120.0")).toBe(false);
  });
});
