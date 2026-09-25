import type { PaymentStatus } from "./portal-types";

const indiaTimeZone = "Asia/Kolkata";

function indiaParts(value: Date) {
  const parts = new Intl.DateTimeFormat("en-CA", {
    timeZone: indiaTimeZone,
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    hourCycle: "h23",
  }).formatToParts(value);
  return Object.fromEntries(parts.filter((part) => part.type !== "literal").map((part) => [part.type, part.value]));
}

export function indiaCalendarDate(now = new Date()): Date {
  const parts = indiaParts(now);
  return new Date(Number(parts.year), Number(parts.month) - 1, Number(parts.day));
}

export function calendarDayValue(date: Date): string {
  return [date.getFullYear(), String(date.getMonth() + 1).padStart(2, "0"), String(date.getDate()).padStart(2, "0")].join("-");
}

export function indiaTimeValue(value: string): string {
  const parts = indiaParts(new Date(value));
  return `${parts.hour}:${parts.minute}`;
}

export function indiaDateTimeInput(value: string): string {
  const parts = indiaParts(new Date(value));
  return `${parts.year}-${parts.month}-${parts.day}T${parts.hour}:${parts.minute}`;
}

export function indiaDateFromInstant(value: string): Date {
  const parts = indiaParts(new Date(value));
  return new Date(Number(parts.year), Number(parts.month) - 1, Number(parts.day));
}

export function indiaInstant(day: string, time: string): string {
  return `${day}T${time}:00+05:30`;
}

export function calendarDate(now = new Date()): string {
  return new Intl.DateTimeFormat("en-CA", {
    timeZone: indiaTimeZone,
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
  }).format(now);
}

/** Completed calendar months, with the original birth day as the monthly boundary. */
export function ageFromBirth(
  date: string,
  asOf = calendarDate(),
): string | null {
  if (!/^\d{4}-\d{2}-\d{2}$/.test(date) || date > asOf) return null;
  const [year, month, day] = date.split("-").map(Number);
  const birth = new Date(Date.UTC(year, month - 1, day));
  if (
    year < 1900 ||
    birth.getUTCFullYear() !== year ||
    birth.getUTCMonth() !== month - 1 ||
    birth.getUTCDate() !== day
  )
    return null;
  const [y, m, d] = asOf.split("-").map(Number);
  const months = (y - year) * 12 + m - month - (d < day ? 1 : 0);
  if (months < 0) return null;
  if (months === 0) return "Under 1 month";
  const years = Math.floor(months / 12),
    remainder = months % 12;
  return [
    years ? `${years} year${years === 1 ? "" : "s"}` : "",
    remainder ? `${remainder} month${remainder === 1 ? "" : "s"}` : "",
  ]
    .filter(Boolean)
    .join(", ");
}
export function money(paise: number | null, currency = "INR"): string {
  return paise === null
    ? "Not set"
    : new Intl.NumberFormat("en-IN", {
        style: "currency",
        currency,
        maximumFractionDigits: 2,
      }).format(paise / 100);
}
export const paymentLabels: Record<PaymentStatus, string> = {
  unpaid: "Unpaid",
  pending: "Pending",
  authorized: "Awaiting capture",
  paid: "Paid",
  failed: "Failed",
  partially_refunded: "Partially refunded",
  refunded: "Refunded",
};
export function newRegistrationToken(): string {
  return Array.from(crypto.getRandomValues(new Uint8Array(32)), (b) =>
    b.toString(16).padStart(2, "0"),
  ).join("");
}
export function errorMessage(error: unknown): string {
  return error instanceof Error
    ? error.message
    : "The request failed. Please try again.";
}
