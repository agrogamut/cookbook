import { fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { listPublicDoctors } from "@/lib/api";
import { AppointmentWizard } from "./appointment-wizard";

vi.mock("@/lib/api", () => ({ listPublicDoctors: vi.fn() }));

const available = {
  doctor_id: "doctor-1", doctor_name: "Doctor One",
  starts_at: "2099-10-05T03:30:00Z", ends_at: "2099-10-05T11:30:00Z",
};

async function chooseInterval(endsAt = available.ends_at) {
  const onBook = vi.fn().mockResolvedValue(undefined);
  render(<AppointmentWizard busy={false} onBook={onBook} loadAvailability={vi.fn().mockResolvedValue([{ ...available, ends_at: endsAt }])} />);
  fireEvent.change(screen.getByLabelText("Date"), { target: { value: "05/10/2099" } });
  await userEvent.click(screen.getByRole("button", { name: "Find available times" }));
  await userEvent.click(await screen.findByRole("button", { name: /Available consultation:/ }));
  return onBook;
}

describe("consultation duration limit", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.mocked(listPublicDoctors).mockResolvedValue([{ id: "doctor-1", name: "Doctor One" }]);
  });

  it("defaults an eight-hour availability block to a thirty-minute booking", async () => {
    const onBook = await chooseInterval();
    expect(screen.getByLabelText("Start time")).toHaveValue("09:00");
    expect(screen.getByLabelText("End time")).toHaveValue("09:30");
    expect(screen.getByLabelText("End time")).toHaveAttribute("max", "09:30");
    await userEvent.click(screen.getByRole("button", { name: "Continue to payment" }));
    expect(onBook).toHaveBeenCalledWith({ doctor_id: undefined, starts_at: "2099-10-05T09:00:00+05:30", ends_at: "2099-10-05T09:30:00+05:30" });
  });

  it("rejects a typed end time beyond the limit", async () => {
    const onBook = await chooseInterval();
    fireEvent.change(screen.getByLabelText("End time"), { target: { value: "09:31" } });
    await userEvent.click(screen.getByRole("button", { name: "Continue to payment" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("Consultations cannot exceed 30 minutes.");
    expect(onBook).not.toHaveBeenCalled();
  });

  it("moves the end with the start and clips it to the doctor's closing time", async () => {
    await chooseInterval();
    fireEvent.change(screen.getByLabelText("Start time"), { target: { value: "11:00" } });
    expect(screen.getByLabelText("End time")).toHaveValue("11:30");
    expect(screen.getByLabelText("End time")).toHaveAttribute("max", "11:30");
    fireEvent.change(screen.getByLabelText("Start time"), { target: { value: "16:45" } });
    expect(screen.getByLabelText("End time")).toHaveValue("17:00");
    expect(screen.getByLabelText("End time")).toHaveAttribute("max", "17:00");
    fireEvent.change(screen.getByLabelText("Start time"), { target: { value: "" } });
    expect(screen.getByLabelText("End time")).toHaveValue("");
    expect(screen.getByRole("button", { name: "Continue to payment" })).toBeDisabled();
  });

  it("keeps a shorter available interval bookable", async () => {
    const onBook = await chooseInterval("2099-10-05T03:45:00Z");
    expect(screen.getByLabelText("End time")).toHaveValue("09:15");
    await userEvent.click(screen.getByRole("button", { name: "Continue to payment" }));
    expect(onBook).toHaveBeenCalledWith(expect.objectContaining({ ends_at: "2099-10-05T09:15:00+05:30" }));
  });

  it("still rejects zero-length and out-of-hours appointments", async () => {
    const onBook = await chooseInterval();
    fireEvent.change(screen.getByLabelText("End time"), { target: { value: "09:00" } });
    await userEvent.click(screen.getByRole("button", { name: "Continue to payment" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("Choose an end time after the start time.");
    fireEvent.change(screen.getByLabelText("Start time"), { target: { value: "08:45" } });
    await userEvent.click(screen.getByRole("button", { name: "Continue to payment" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("within the selected available interval");
    expect(onBook).not.toHaveBeenCalled();
  });
});
