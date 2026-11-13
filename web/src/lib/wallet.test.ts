import { describe, it, expect } from "vitest";
import { parseRupees, withdrawalAmounts } from "./wallet";
describe("integer wallet amounts", () => {
  it("keeps the gross reservation separate from the actual transfer", () => {
    expect(withdrawalAmounts(200000, 1800)).toEqual({
      requested: 200000,
      deduction: 36000,
      net: 164000,
    });
    expect(withdrawalAmounts(200000, 0)).toEqual({
      requested: 200000,
      deduction: 0,
      net: 200000,
    });
  });
  it("rejects fractional paise and unsafe amounts", () => {
    expect(parseRupees("2000.01")).toBe(200001);
    for (const value of ["-1", "1.001", "1e3", "NaN", "1000001", ""])
      expect(parseRupees(value)).toBeNull();
    expect(parseRupees("0.01")).toBe(1);
  });
});
