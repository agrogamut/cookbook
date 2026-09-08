import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { ChildInputForm } from "./child-input-form";
import { matchProfiles, getProfile } from "@/lib/api";
import type { MatchCandidate, StoredProfile } from "@/lib/types";

// ChildInputForm loads six reference vocabularies on mount, same as ProfileForm; mocking
// the whole api module keeps the test off the network. matchProfiles and getProfile are the
// two calls this test actually drives, so they're left as bare vi.fn()s configured per test.
vi.mock("@/lib/api", () => ({
  getRegions: vi.fn(() => Promise.resolve([])),
  getCuisines: vi.fn(() => Promise.resolve([])),
  getAllergens: vi.fn(() => Promise.resolve([])),
  getEnums: vi.fn(() => Promise.resolve({})),
  getSpecialCareConditions: vi.fn(() => Promise.resolve([])),
  getClinicalMarkers: vi.fn(() => Promise.resolve([])),
  matchProfiles: vi.fn(),
  getProfile: vi.fn(),
}));

const candidate: MatchCandidate = {
  child_id: "MG-C-00042",
  case_id: "CASE-1",
  display_name: "Aarav Sen",
  date_of_birth: "2022-05-01",
  last_touched: "2026-08-01T00:00:00Z",
};

const storedProfile: StoredProfile = {
  child_id: "MG-C-00042",
  case_id: "CASE-1",
  mother_name: "Priya Sen",
  display_name: "Aarav Sen",
  date_of_birth: "2022-05-01",
  sex: "male",
  allergens: [],
};

describe("ChildInputForm", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.mocked(matchProfiles).mockResolvedValue([candidate]);
    vi.mocked(getProfile).mockResolvedValue(storedProfile);
  });

  it("surfaces a match banner for a typed case id and loads it into the form", async () => {
    render(<ChildInputForm busy={false} onGenerate={() => {}} />);

    // The lookup is debounced 500ms after the operator stops typing a case id, matching
    // internal/profile's FindMatches rule that a case id alone is enough to search on.
    await userEvent.type(screen.getByLabelText(/case id/i), "CASE-1");

    expect(await screen.findByText(/MG-C-00042/, {}, { timeout: 2000 }))
      .toBeInTheDocument();
    expect(screen.getByRole("button", { name: /load this record/i })).toBeInTheDocument();

    await userEvent.click(screen.getByRole("button", { name: /load this record/i }));

    await waitFor(() => expect(getProfile).toHaveBeenCalledWith("MG-C-00042"));
    // Loading the record prefills the form from the fetched profile, not from the match
    // candidate row -- the candidate is a summary, the profile is the source of truth.
    await waitFor(() =>
      expect(screen.getByLabelText(/^name$/i)).toHaveValue("Aarav Sen"));
  });

  it("never applies a match to the form until the operator explicitly loads it", async () => {
    render(<ChildInputForm busy={false} onGenerate={() => {}} />);

    const nameInput = screen.getByLabelText(/^name$/i);
    await userEvent.type(nameInput, "Aarav");
    await userEvent.type(screen.getByLabelText(/case id/i), "CASE-1");

    expect(await screen.findByText(/MG-C-00042/, {}, { timeout: 2000 }))
      .toBeInTheDocument();

    // A match being found is not itself a reason to touch any field -- only the operator's
    // explicit "Load this record" click may do that.
    expect(nameInput).toHaveValue("Aarav");

    await userEvent.click(screen.getByRole("button", { name: /load this record/i }));

    await waitFor(() => expect(nameInput).toHaveValue("Aarav Sen"));

    // loadMatch just rewrote caseId/name/dob/motherName to the loaded values -- exactly the
    // debounce effect's own dependency array. That must not re-trigger a fresh query that
    // finds the same record again and re-shows the banner the operator just dismissed by
    // loading it. Wait past the 500ms debounce window to give a regression a chance to fire.
    await new Promise((resolve) => setTimeout(resolve, 700));
    expect(screen.queryByRole("button", { name: /load this record/i })).not.toBeInTheDocument();
  });
});
