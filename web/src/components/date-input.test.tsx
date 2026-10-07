import { useState } from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { DateInput } from "./date-input";

function Form({ initial = "" }: { initial?: string }) {
  const [value, setValue] = useState(initial);
  return (
    <form>
      <label htmlFor="date">Date</label>
      <DateInput id="date" value={value} onValueChange={setValue} min="1900-01-01" max="2026-10-31" required />
      <output data-testid="iso">{value}</output>
      <button type="button" onClick={() => setValue("2024-02-29")}>Set leap day</button>
    </form>
  );
}

describe("day-first date entry", () => {
  it("shows day first and sends the unambiguous ISO date", () => {
    render(<Form initial="2026-10-08" />);
    const input = screen.getByLabelText("Date");
    expect(input).toHaveValue("08/10/2026");
    fireEvent.change(input, { target: { value: "10/08/2026" } });
    expect(screen.getByTestId("iso")).toHaveTextContent("2026-08-10");
    expect(input).toBeValid();
  });

  it("accepts eight typed digits and formats them on blur", async () => {
    render(<Form />);
    const input = screen.getByLabelText("Date");
    await userEvent.type(input, "29022024");
    await userEvent.tab();
    expect(input).toHaveValue("29/02/2024");
    expect(screen.getByTestId("iso")).toHaveTextContent("2024-02-29");
  });

  it.each(["29/02/2023", "31/04/2024", "10/8/2026", "2026-10-08", "01/01/1899", "01/11/2026"])("blocks invalid or out-of-range date %s", (value) => {
    render(<Form initial="2026-10-08" />);
    const input = screen.getByLabelText("Date");
    fireEvent.change(input, { target: { value } });
    fireEvent.blur(input);
    expect(input).toBeInvalid();
    expect(screen.getByRole("alert")).toBeInTheDocument();
    expect(screen.getByTestId("iso")).toBeEmptyDOMElement();
  });

  it("clears a previous date and accepts an external value after an invalid draft", async () => {
    render(<Form initial="2026-10-08" />);
    const input = screen.getByLabelText("Date");
    await userEvent.clear(input);
    expect(screen.getByTestId("iso")).toBeEmptyDOMElement();
    expect(input).toBeInvalid();
    await userEvent.type(input, "31/04/2024");
    await userEvent.click(screen.getByRole("button", { name: "Set leap day" }));
    expect(input).toHaveValue("29/02/2024");
    expect(input).toBeValid();
  });
});
