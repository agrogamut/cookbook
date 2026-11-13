import { render, screen, within } from "@testing-library/react";
import { describe, it, expect, vi } from "vitest";
import { DoctorBookings } from "./doctor-bookings";
import { listAppointments } from "@/lib/api";
import type { Appointment } from "@/lib/portal-types";
vi.mock("@/lib/api", () => ({
  listAppointments: vi.fn(),
  saveMeeting: vi.fn(),
}));
const item = {
  registration_id: "reg",
  doctor_id: "doctor",
  doctor_name: "Doctor",
  mode: "specific_doctor",
  payment_status: "paid",
  requested_by: "reg",
  decided_by: "admin",
  created_at: "2026-01-01T00:00:00Z",
} as const;
describe("doctor calls", () => {
  it("separates pending requests, future calls and old appointments with real times", async () => {
    vi.mocked(listAppointments).mockResolvedValue([
      {
        ...item,
        id: "later",
        child_name: "Later child",
        status: "confirmed",
        starts_at: "2099-01-02T10:00:00Z",
        ends_at: "2099-01-02T10:30:00Z",
      },
      {
        ...item,
        id: "first",
        child_name: "First child",
        status: "confirmed",
        starts_at: "2099-01-01T10:00:00Z",
        ends_at: "2099-01-01T10:30:00Z",
      },
      {
        ...item,
        id: "pending",
        child_name: "Pending child",
        status: "paid_pending_admin",
        starts_at: "2099-01-03T10:00:00Z",
        ends_at: "2099-01-03T10:30:00Z",
        guardian_description: "<script>guardian text</script>",
      },
      {
        ...item,
        id: "old",
        child_name: "Old child",
        status: "confirmed",
        starts_at: "2000-01-01T10:00:00Z",
        ends_at: "2000-01-01T10:30:00Z",
      },
    ] satisfies Appointment[]);
    render(<DoctorBookings />);
    await screen.findByText("First child");
    const upcoming = screen.getByRole("region", { name: "Upcoming calls" });
    expect(
      within(upcoming)
        .getAllByRole("heading", { level: 3 })
        .map((n) => n.textContent),
    ).toEqual(["First child", "Later child"]);
    expect(
      within(
        screen.getByRole("region", { name: "Consultation requests" }),
      ).getByText("Pending child"),
    ).toBeVisible();
    expect(screen.getByText("<script>guardian text</script>")).toBeVisible();
    expect(screen.queryByText("Old child")).toBeNull();
    expect(within(upcoming).getAllByText(/IST/).length).toBeGreaterThan(0);
  });
});
