import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { SuspectedAllergenFieldset } from "./suspected-allergen-fieldset";
import type { Allergen } from "@/lib/types";

// fireEvent rather than user-event: the library is not a dependency of this project and
// two clicks do not justify adding one.
const options: Allergen[] = [
  { allergen_group: "Peanut", corpus_tag: "Peanut", screens: true, note: "" },
  { allergen_group: "Milk", corpus_tag: "Milk", screens: true, note: "" },
  { allergen_group: "Tree nuts", corpus_tag: null, screens: false, note: "no corpus tag" },
  { allergen_group: "Mustard", corpus_tag: null, screens: false, note: "legacy mapping note" },
];

describe("SuspectedAllergenFieldset", () => {
  it("says it ranks down rather than excludes", () => {
    render(<SuspectedAllergenFieldset options={options} selected={[]} declared={[]} onToggle={() => {}} />);
    // The whole safety point: an operator must not read this as protection.
    expect(screen.getByText(/Known matches to suspected allergens rank down without exclusion/i)).toBeInTheDocument();
  });

  it("offers a group that is already declared, but marks it superseded", () => {
    // Declaring Peanut confirmed and also suspecting it is not a contradiction the UI
    // should hide -- the confirmed hard filter already removed those recipes, so the
    // suspicion changes nothing and the row must say so rather than looking active.
    render(<SuspectedAllergenFieldset options={options} selected={[]} declared={["Peanut"]} onToggle={() => {}} />);
    expect(screen.getByRole("button", { name: /Peanut/ })).toBeDisabled();
    expect(screen.getByRole("button", { name: /Peanut/ })).toHaveAttribute("title", "Already declared as a confirmed allergen; use its confirmed control above.");
    expect(screen.getByText(/already declared/i)).toBeInTheDocument();
  });

  it("distinguishes a legacy tag gap from ingredient-backed coverage", () => {
    render(<SuspectedAllergenFieldset options={options} selected={[]} declared={[]} onToggle={() => {}} />);
    expect(screen.getByText("Tree nuts - legacy tag gap")).toHaveClass("border-dashed");
    expect(screen.getByText("Mustard - legacy tag gap")).toHaveClass("border-dashed");
    const mustard = screen.getByRole("button", { name: "Mustard - legacy tag gap" });
    expect(mustard).toHaveAttribute("title", expect.stringContaining("coverage is incomplete"));
    expect(mustard).toHaveAttribute("title", expect.stringContaining("Legacy tag mapping note: legacy mapping note"));
    expect(screen.getByRole("button", { name: "Milk" })).toHaveAttribute("title", "Legacy corpus tag: Milk. ");
    expect(screen.queryByText(/not screened/)).not.toBeInTheDocument();
  });

  it("toggles a selectable group", () => {
    const onToggle = vi.fn();
    render(<SuspectedAllergenFieldset options={options} selected={[]} declared={[]} onToggle={onToggle} />);
    fireEvent.click(screen.getByRole("button", { name: /Milk/ }));
    expect(onToggle).toHaveBeenCalledWith("Milk");
  });

  it("does not toggle a group already declared confirmed", () => {
    const onToggle = vi.fn();
    render(<SuspectedAllergenFieldset options={options} selected={[]} declared={["Peanut"]} onToggle={onToggle} />);
    fireEvent.click(screen.getByRole("button", { name: /Peanut/ }));
    expect(onToggle).not.toHaveBeenCalled();
  });
});
