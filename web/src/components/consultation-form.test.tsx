import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ConsultationForm } from "./consultation-form";
import {
  createCheckoutOrder,
  createPublicAppointment,
  getPublicSettings,
  getRegistrationStatus,
  listPublicAvailability,
  listPublicDoctors,
  registerConsultation,
  verifyCheckout,
} from "@/lib/api";
import { loadCheckout } from "@/lib/checkout";

vi.mock("@/lib/api", () => ({
  getPublicSettings: vi.fn(),
  getRegistrationStatus: vi.fn(),
  registerConsultation: vi.fn(),
  createCheckoutOrder: vi.fn(),
  createPublicAppointment: vi.fn(),
  listPublicAvailability: vi.fn(),
  listPublicDoctors: vi.fn(),
  verifyCheckout: vi.fn(),
}));
vi.mock("@/lib/checkout", () => ({ loadCheckout: vi.fn() }));

async function completeForm() {
  await userEvent.type(
    screen.getByLabelText("Guardian’s name"),
    "Test Guardian",
  );
  await userEvent.type(screen.getByLabelText("Child’s name"), "Test Child");
  fireEvent.change(screen.getByLabelText("Child’s date of birth"), {
    target: { value: "28/02/2023" },
  });
  await userEvent.type(screen.getByLabelText("Phone number"), "+919876543210");
  await userEvent.click(
    screen.getByRole("button", { name: "Request a consultation" }),
  );
}

describe("consultation intake", () => {
  afterEach(() => vi.restoreAllMocks());
  beforeEach(() => {
    vi.clearAllMocks();
    sessionStorage.clear();
    vi.mocked(getPublicSettings).mockResolvedValue({
      amount_paise: null,
      currency: "INR",
      checkout_available: false,
    });
    vi.mocked(registerConsultation).mockResolvedValue({
      id: "test-registration",
    });
    vi.mocked(listPublicDoctors).mockResolvedValue([{id:"doctor-1",name:"Doctor One"}]);
    vi.mocked(listPublicAvailability).mockResolvedValue([
      { doctor_id: "doctor-1", doctor_name: "Doctor One", starts_at: "2099-10-05T04:30:00Z", ends_at: "2099-10-05T05:00:00Z" },
    ]);
    vi.mocked(createPublicAppointment).mockResolvedValue({
      id: "appointment-1", status: "awaiting_payment", hold_expires_at: "2099-10-05T05:15:00Z",
    });
  });
  it("stores the requested fields with optional email and never requires checkout", async () => {
    render(<ConsultationForm />);
    await completeForm();
    expect(
      await screen.findByText("Your request is with us."),
    ).toBeInTheDocument();
    expect(registerConsultation).toHaveBeenCalledWith(
      expect.objectContaining({
        guardian_name: "Test Guardian",
        child_name: "Test Child",
        date_of_birth: "2023-02-28",
        phone: "+919876543210",
        email: "",
        token: expect.stringMatching(/^[a-f0-9]{64}$/),
      }),
    );
    expect(createCheckoutOrder).not.toHaveBeenCalled();
    expect(screen.queryByText(/Save this private token/)).not.toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Open family portal" })).toHaveAttribute("href", "/family/login");
    expect(screen.getByText("Unpaid")).toBeInTheDocument();
    expect(
      sessionStorage.getItem("madamgy.consultation.receipt"),
    ).not.toContain("Test Guardian");
  });
  it("keeps the form and its token for retries when saving fails", async () => {
    vi.mocked(registerConsultation).mockRejectedValueOnce(
      new Error("Temporary storage failure"),
    );
    render(<ConsultationForm />);
    await completeForm();
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Temporary storage failure",
    );
    expect(
      screen.queryByText("Your request is with us."),
    ).not.toBeInTheDocument();
    await userEvent.click(
      screen.getByRole("button", { name: "Request a consultation" }),
    );
    await screen.findByText("Your request is with us.");
    expect(vi.mocked(registerConsultation).mock.calls[0][0].token).toBe(
      vi.mocked(registerConsultation).mock.calls[1][0].token,
    );
  });
  it("can register another child when browser storage is disabled", async () => {
    for (const method of ["getItem", "setItem", "removeItem"] as const) {
      vi.spyOn(Storage.prototype, method).mockImplementation(() => {
        throw new DOMException("Storage disabled", "SecurityError");
      });
    }
    render(<ConsultationForm />);
    await completeForm();
    await userEvent.click(
      await screen.findByRole("button", { name: "Register another child" }),
    );
    expect(screen.getByLabelText("Guardian’s name")).toHaveValue("");
    await completeForm();
    expect(registerConsultation).toHaveBeenCalledTimes(2);
    expect(vi.mocked(registerConsultation).mock.calls[0][0].token).not.toBe(
      vi.mocked(registerConsultation).mock.calls[1][0].token,
    );
  });
  it("does not declare payment successful when the checkout callback cannot be verified", async () => {
    vi.mocked(getPublicSettings).mockResolvedValue({
      amount_paise: 12345,
      currency: "INR",
      checkout_available: true,
    });
    vi.mocked(getRegistrationStatus).mockResolvedValue({ id: "test-registration", payment_status: "unpaid", appointment_id: "appointment-1", appointment_status: "awaiting_payment" });
    vi.mocked(createCheckoutOrder).mockResolvedValue({
      order_id: "order_test",
      amount_paise: 12345,
      currency: "INR",
      key_id: "test_key",
    });
    vi.mocked(verifyCheckout).mockRejectedValue(
      new Error("Payment confirmation could not be verified."),
    );
    let handler:
      | ((data: {
          razorpay_payment_id: string;
          razorpay_order_id: string;
          razorpay_signature: string;
        }) => void)
      | undefined;
    class Checkout {
      constructor(options: { handler: typeof handler }) {
        handler = options.handler;
      }
      open() {
        handler?.({
          razorpay_payment_id: "pay_test",
          razorpay_order_id: "order_test",
          razorpay_signature: "invalid",
        });
      }
      on() {}
    }
    vi.mocked(loadCheckout).mockResolvedValue(Checkout);
    render(<ConsultationForm />);
    await completeForm();
    expect(screen.getByText("Choose an available consultation time above to continue to payment.")).toBeInTheDocument();
    expect(screen.queryByText(/Online payment is not available/)).not.toBeInTheDocument();
    fireEvent.change(screen.getByLabelText("Date"), { target: { value: "05/10/2099" } });
    await userEvent.click(await screen.findByRole("button", { name: "Find available times" }));
    await userEvent.click(await screen.findByRole("button", { name: /Available consultation/ }));
    await userEvent.click(screen.getByRole("button", { name: "Continue to payment" }));
    await userEvent.click(
      await screen.findByRole("button", { name: /pay.*for consultation/i }),
    );
    expect(await screen.findByRole("status")).toHaveTextContent(
      "Payment confirmation could not be verified.",
    );
    expect(screen.getByText("Unpaid")).toBeInTheDocument();
    expect(screen.queryByText(/Payment received/)).not.toBeInTheDocument();
  });
  it("shows the verified server payment state after a refresh", async () => {
    vi.mocked(getRegistrationStatus).mockResolvedValue({
      id: "test-registration",
      payment_status: "paid",
    });
    render(<ConsultationForm />);
    await completeForm();
    await userEvent.click(
      await screen.findByRole("button", { name: "Check payment status" }),
    );
    await waitFor(() => expect(screen.getByText("Paid")).toBeInTheDocument());
  });
});
