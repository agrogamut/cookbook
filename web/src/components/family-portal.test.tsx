import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { FamilyRegistrationCard } from "./family-portal";
import type { FamilyRegistration } from "@/lib/portal-types";

const registration: FamilyRegistration = {
  id: "registration-1",
  child_name: "Child One",
  date_of_birth: "2022-05-01",
  registration_status: "new",
  payment_status: "paid",
  amount_paise: 1000,
  currency: "INR",
  doctor_name: "Doctor One",
  appointment_id: null,
  appointment_status: null,
  appointment_starts_at: null,
  appointment_ends_at: null,
  appointment_hold_expires_at: null,
  book1_release_id: "release-1",
  book1_status: "pending_admin",
  book2_release_id: "release-2",
  book2_status: "rejected",
  created_at: "2026-09-25T00:00:00Z",
};

describe("FamilyRegistrationCard", () => {
  it("does not render a download action for an unapproved book", () => {
    render(
      <FamilyRegistrationCard
        registration={registration}
        onPay={vi.fn()}
        onBook={vi.fn()}
        onDownload={vi.fn()}
        onCancel={vi.fn()}
      />,
    );
    expect(screen.queryByRole("button", { name: /download/i })).toBeNull();
    expect(screen.getByText("Book 1: pending admin")).toBeInTheDocument();
    expect(screen.getByText("Book 2: rejected")).toBeInTheDocument();
  });

  it("requires a new time before payment after a rejected request", () => {
    render(
      <FamilyRegistrationCard
        registration={{ ...registration, payment_status: "failed", appointment_status: "rejected", book1_release_id: null, book1_status: null, book2_release_id: null, book2_status: null }}
        onPay={vi.fn()}
        onBook={vi.fn()}
        onDownload={vi.fn()}
        onCancel={vi.fn()}
      />,
    );
    expect(screen.queryByRole("button", { name: /pay/i })).toBeNull();
    expect(screen.getByRole("button", { name: /book a consultation/i })).toBeInTheDocument();
  });
});
