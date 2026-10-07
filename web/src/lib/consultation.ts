export const maxConsultationMinutes = 30;

export function consultationEnd(startsAt: string, availableUntil?: string): string {
  const end = Math.min(
    Date.parse(startsAt) + maxConsultationMinutes * 60_000,
    availableUntil ? Date.parse(availableUntil) : Infinity,
  );
  return Number.isFinite(end) ? new Date(end).toISOString() : "";
}

export function consultationDurationError(startsAt: string, endsAt: string): string {
  const duration = Date.parse(endsAt) - Date.parse(startsAt);
  if (!Number.isFinite(duration) || duration <= 0) {
    return "Choose an end time after the start time.";
  }
  return duration > maxConsultationMinutes * 60_000
    ? "Consultations cannot exceed 30 minutes."
    : "";
}
