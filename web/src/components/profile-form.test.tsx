import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi, beforeEach } from "vitest";
import { ProfileForm } from "./profile-form";

// ProfileForm loads six reference vocabularies on mount. Mocking the whole api module
// keeps the test off the network; the empty arrays are the honest shape for "the
// vocabularies loaded and contained nothing", which is what an unseeded database returns.
vi.mock("@/lib/api", () => ({
  getAllergens: vi.fn(() => Promise.resolve([])),
  getClinicalMarkers: vi.fn(() => Promise.resolve([])),
  getEnums: vi.fn(() => Promise.resolve({})),
  getRegions: vi.fn(() => Promise.resolve([])),
  getCuisines: vi.fn(() => Promise.resolve([])),
  getSpecialCareConditions: vi.fn(() => Promise.resolve([])),
  getProfileEngineInput: vi.fn(),
}));

describe("ProfileForm", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("shows the age field without any interaction", async () => {
    // Age is engine step 1, a hard filter and the only required field. It must never
    // sit behind a disclosure control.
    render(<ProfileForm onSubmit={() => {}} loading={false} />);
    await waitFor(() => expect(screen.getByLabelText(/age \(months\)/i)).toBeInTheDocument());
  });

  it("shows the declared allergens fieldset without any interaction", async () => {
    // Allergens are engine step 2, the other hard filter. Same rule as age.
    render(<ProfileForm onSubmit={() => {}} loading={false} />);
    // Matched on the legend element specifically: "Declared allergens" also appears in
    // the suspected-allergen fieldset's prose, which points back at this control.
    await waitFor(() => expect(
      screen.getByText(/declared allergens/i, { selector: "legend" }),
    ).toBeInTheDocument());
  });

  it("disables submit until an age is entered", async () => {
    render(<ProfileForm onSubmit={() => {}} loading={false} />);
    const submit = await screen.findByRole("button", { name: /search/i });
    expect(submit).toBeDisabled();
  });

  it("submits the entered age", async () => {
    const onSubmit = vi.fn();
    render(<ProfileForm onSubmit={onSubmit} loading={false} />);
    const age = await screen.findByLabelText(/age \(months\)/i);
    fireEvent.change(age, { target: { value: "8" } });
    fireEvent.click(screen.getByRole("button", { name: /search/i }));
    await waitFor(() => expect(onSubmit).toHaveBeenCalledWith(
      expect.objectContaining({ age_months: 8 }),
    ));
  });

  it("sends no undefined optional fields when only age is set", async () => {
    // The engine treats a present-but-empty filter differently from an absent one.
    // Nothing the operator did not fill in may reach the request as a value.
    const onSubmit = vi.fn();
    render(<ProfileForm onSubmit={onSubmit} loading={false} />);
    const age = await screen.findByLabelText(/age \(months\)/i);
    fireEvent.change(age, { target: { value: "8" } });
    fireEvent.click(screen.getByRole("button", { name: /search/i }));
    await waitFor(() => expect(onSubmit).toHaveBeenCalled());
    const payload = onSubmit.mock.calls[0][0];
    expect(payload.diet_type).toBeUndefined();
    expect(payload.allergens).toBeUndefined();
    expect(payload.region_culture).toBeUndefined();
    expect(payload.meal_type).toBeUndefined();
  });

  it("shows the running state while a search is in flight", async () => {
    render(<ProfileForm onSubmit={() => {}} loading={true} />);
    expect(await screen.findByRole("button", { name: /running engine/i })).toBeDisabled();
  });
});
