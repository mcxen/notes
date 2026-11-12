import { describe, expect, it } from "vitest";
import { crc32, zipStore } from "./zip";

describe("zip", () => {
  it("has the CRC-32 every zip tool expects", () => {
    expect(crc32(new TextEncoder().encode("123456789"))).toBe(0xcbf43926);
    expect(crc32(new Uint8Array())).toBe(0);
  });

  it("writes a readable archive: signatures, names, sizes and the central directory", () => {
    const a = new TextEncoder().encode("hello");
    const b = new Uint8Array([1, 2, 3, 4, 5, 6]);
    const zip = zipStore([{ path: "Notes/页面.md", data: a }, { path: "assets/1-a.png", data: b }]);
    const view = new DataView(zip.buffer);
    expect(view.getUint32(0, true)).toBe(0x04034b50);
    // The end record: two entries, and a directory that starts where it says.
    const end = zip.length - 22;
    expect(view.getUint32(end, true)).toBe(0x06054b50);
    expect(view.getUint16(end + 10, true)).toBe(2);
    const directory = view.getUint32(end + 16, true);
    expect(view.getUint32(directory, true)).toBe(0x02014b50);
    expect(view.getUint32(directory + 20, true)).toBe(5); // size of the first file
    const names = new TextDecoder().decode(zip);
    expect(names).toContain("Notes/页面.md");
    expect(names).toContain("assets/1-a.png");
    // Stored, so the bytes are in the archive as they are.
    expect(names).toContain("hello");
  });
});
