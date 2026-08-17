import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { ProvenanceChip } from "./provenance-chip";

describe("ProvenanceChip", () => {
  it("renders the source and confidence", () => {
    render(<ProvenanceChip source="ifct" confidence={0.92} explanation="IFCT 2017 exact match" />);
    expect(screen.getByText(/ifct/i)).toBeInTheDocument();
    expect(screen.getByText(/92\.0%/)).toBeInTheDocument();
  });

  it("does not round away a meaningful confidence gap", () => {
    // 0.996 rounded to whole percent reads as 100% -- indistinguishable from fully
    // verified. One decimal place keeps a near-but-not-fully-verified value honest.
    render(<ProvenanceChip source="ifct" confidence={0.996} explanation="near-complete coverage" />);
    expect(screen.getByText(/99\.6%/)).toBeInTheDocument();
    expect(screen.queryByText(/100\.0%|100%/)).not.toBeInTheDocument();
  });

  it("renders without a confidence badge when none is given", () => {
    render(<ProvenanceChip source="provider" explanation="Provider group-level placeholder" />);
    expect(screen.getByText(/provider/i)).toBeInTheDocument();
    expect(screen.queryByText(/%/)).not.toBeInTheDocument();
  });
});
