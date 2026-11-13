import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { AdminWallet } from "./admin-wallet";
import { WithdrawalAmounts } from "./doctor-wallet";
import { getWallets, getWithdrawals, decideWithdrawal } from "@/lib/api";
import type { Withdrawal } from "@/lib/portal-types";

vi.mock("@/lib/api", () => ({
  getWallets: vi.fn(), getWithdrawals: vi.fn(), decideWithdrawal: vi.fn(),
  revealWithdrawalDestination: vi.fn(), getConsultationSettings: vi.fn(),
  saveDoctorEarning: vi.fn(), cancelWithdrawal: vi.fn(), getDoctorWallet: vi.fn(),
  requestWithdrawal: vi.fn(), savePayoutDestination: vi.fn(),
}));
const request: Withdrawal = {
  id: "request", doctor_id: "doctor", doctor_name: "Synthetic Doctor",
  requested_paise: 200000, deduction_paise: 36000, net_paise: 164000,
  deduction_bps: 1800, masked_destination: "Bank account ending 7890",
  status: "requested", created_at: "2026-01-01T10:00:00Z", policy_pending: true,
  transferred_paise: null, transfer_reference: null, transferred_at: null,
  decision_note: "",
};
beforeEach(() => { vi.clearAllMocks(); vi.mocked(getWallets).mockResolvedValue([]); });
describe("withdrawal policy review", () => {
  it("never presents an inactive-policy estimate as the final payout", () => {
    render(<WithdrawalAmounts amount={200000} bps={1800} pending />);
    expect(screen.getByText("Pending policy review")).toBeVisible();
    expect(screen.getByText("₹360.00")).toBeVisible();
    expect(screen.getByText("₹1,640.00, not yet approved for payment.")).toBeVisible();
    expect(screen.queryByText("₹2,000.00", { selector: "strong" })).toBeNull();
  });
  it("provides no payment controls for a pending or revoked policy snapshot", async () => {
    vi.mocked(getWithdrawals).mockResolvedValue([request]);
    render(<AdminWallet />);
    await screen.findByText(/Do not transfer or mark paid/);
    expect(screen.queryByText("Record completed manual transfer")).toBeNull();
    expect(screen.queryByRole("button", { name: "Mark paid" })).toBeNull();
    expect(screen.getByRole("button", { name: "Reject and release reservation" })).toBeVisible();
    expect(decideWithdrawal).not.toHaveBeenCalled();
  });
  it("requires confirmation of an actual transfer for a reviewed snapshot", async () => {
    vi.mocked(getWithdrawals).mockResolvedValue([{ ...request, policy_pending: false }]);
    render(<AdminWallet />);
    await userEvent.click(await screen.findByText("Record completed manual transfer"));
    expect(screen.getByRole("button", { name: "Mark paid" })).toBeDisabled();
    expect(screen.getByLabelText("Actual transferred amount (INR)", { exact: true })).toHaveValue("");
    expect(screen.getByLabelText("Transfer reference", { exact: true })).toHaveValue("");
    expect(decideWithdrawal).not.toHaveBeenCalled();
  });
});
