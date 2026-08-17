import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { ProvenanceChip } from "./provenance-chip";

describe("ProvenanceChip", () => {
  it("renders the source and confidence", () => {
    render(<ProvenanceChip source="ifct" confidence={0.92} explanation="IFCT 2017 exact match" />);
    expect(screen.getByText(/ifct/i)).toBeInTheDocument();
    expect(screen.getByText(/92%/)).toBeInTheDocument();
  });

  it("renders without a confidence badge when none is given", () => {
    render(<ProvenanceChip source="provider" explanation="Provider group-level placeholder" />);
    expect(screen.getByText(/provider/i)).toBeInTheDocument();
    expect(screen.queryByText(/%/)).not.toBeInTheDocument();
  });
});
