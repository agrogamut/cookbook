import { describe, expect, it } from "vitest";
import {
  ageFromBirth,
  calendarDate,
  newRegistrationToken,
} from "./portal-utils";

describe("calendar age", () => {
  it("counts completed months without treating a year as a fixed day count", () => {
    expect(ageFromBirth("2023-02-28", "2026-09-11")).toBe("3 years, 6 months");
    expect(ageFromBirth("2025-09-12", "2026-09-11")).toBe("11 months");
    expect(ageFromBirth("2025-09-11", "2026-09-11")).toBe("1 year");
    expect(ageFromBirth("2026-09-11", "2026-09-11")).toBe("Under 1 month");
    expect(ageFromBirth("2024-01-31", "2024-02-29")).toBe("Under 1 month");
    expect(ageFromBirth("2024-02-29", "2025-03-01")).toBe("1 year");
  });
  it("rejects impossible and future dates", () => {
    expect(ageFromBirth("2025-02-29", "2026-09-11")).toBeNull();
    expect(ageFromBirth("2026-09-12", "2026-09-11")).toBeNull();
    expect(ageFromBirth("2025-13-01", "2026-09-11")).toBeNull();
    expect(ageFromBirth("", "2026-09-11")).toBeNull();
  });
  it("uses the Indian calendar day across the UTC boundary", () => {
    expect(calendarDate(new Date("2026-09-11T20:00:00Z"))).toBe("2026-09-12");
  });
  it("creates independent private registration tokens", () => {
    const first = newRegistrationToken();
    expect(first).toMatch(/^[a-f0-9]{64}$/);
    expect(newRegistrationToken()).not.toBe(first);
  });
});
