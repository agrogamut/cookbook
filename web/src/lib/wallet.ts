export function parseRupees(value: string): number | null {
  if (!/^\d+(\.\d{1,2})?$/.test(value.trim())) return null;
  const [whole, part = ""] = value.trim().split(".");
  const result = Number(whole) * 100 + Number(part.padEnd(2, "0"));
  return Number.isSafeInteger(result) && result <= 100000000 ? result : null;
}
export function withdrawalAmounts(requested: number, bps: number) {
  const deduction = Math.floor((requested * bps + 5000) / 10000);
  return { requested, deduction, net: requested - deduction };
}
