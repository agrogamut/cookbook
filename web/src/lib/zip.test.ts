import { describe, it, expect } from "vitest";
import { buildZip, crc32 } from "./zip";

const u32 = (b: Uint8Array, at: number) => new DataView(b.buffer).getUint32(at, true);
const u16 = (b: Uint8Array, at: number) => new DataView(b.buffer).getUint16(at, true);

describe("buildZip", () => {
  it("computes the standard CRC-32 check value", () => {
    expect(crc32(new TextEncoder().encode("123456789"))).toBe(0xcbf43926);
  });

  it("writes each entry's bytes verbatim and a central directory that points back at them", () => {
    const a = new TextEncoder().encode("%PDF-one");
    const b = new TextEncoder().encode("%PDF-second");
    const zip = buildZip([{ name: "x-book1.pdf", data: a }, { name: "x-book2.pdf", data: b }]);

    // End of central directory: two entries, and the directory starts where it says.
    const eocd = zip.length - 22;
    expect(u32(zip, eocd)).toBe(0x06054b50);
    expect(u16(zip, eocd + 10)).toBe(2);
    let at = u32(zip, eocd + 16);

    const dec = new TextDecoder();
    for (const [name, data] of [["x-book1.pdf", a], ["x-book2.pdf", b]] as const) {
      expect(u32(zip, at)).toBe(0x02014b50);
      expect(u32(zip, at + 16)).toBe(crc32(data));
      const nameLen = u16(zip, at + 28);
      expect(dec.decode(zip.slice(at + 46, at + 46 + nameLen))).toBe(name);
      const local = u32(zip, at + 42);
      expect(u32(zip, local)).toBe(0x04034b50);
      const start = local + 30 + u16(zip, local + 26);
      expect(Array.from(zip.slice(start, start + data.length))).toEqual(Array.from(data));
      at += 46 + nameLen;
    }
  });
});
