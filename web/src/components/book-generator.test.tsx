import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { BookGenerator } from "./book-generator";

function mockFetch(init: { status: number; body?: string; omissions?: string }) {
  return vi.fn().mockResolvedValue({
    ok: init.status >= 200 && init.status < 300,
    status: init.status,
    headers: { get: (k: string) => (k === "X-Book-Omissions" ? init.omissions ?? null : null) },
    text: async () => init.body ?? "",
    json: async () => JSON.parse(init.body ?? "{}"),
  });
}

beforeEach(() => { vi.stubGlobal("fetch", mockFetch({ status: 200, body: "<html></html>" })); });
afterEach(() => { vi.unstubAllGlobals(); });

describe("BookGenerator", () => {
  it("renders a clinical stop, not an error, when the engine blocks the child", async () => {
    vi.stubGlobal("fetch", mockFetch({
      status: 409,
      body: JSON.stringify({ error: "Down syndrome is a STOP-REVIEW condition", reviewer: "Pediatrician + dietitian" }),
    }));
    render(<BookGenerator />);
    await userEvent.type(screen.getByLabelText(/child id/i), "C-1");
    await userEvent.click(screen.getByRole("button", { name: /preview/i }));

    expect(await screen.findByText(/STOP-REVIEW/)).toBeInTheDocument();
    expect(screen.getByText(/Pediatrician \+ dietitian/)).toBeInTheDocument();
    // A stop is a clinical decision, so the operator is never offered the artifact anyway.
    expect(screen.queryByRole("button", { name: /download pdf/i })).toBeNull();
  });

  it("distinguishes an unavailable renderer from a clinical stop", async () => {
    vi.stubGlobal("fetch", mockFetch({ status: 503, body: JSON.stringify({ error: "headless chromium unavailable" }) }));
    render(<BookGenerator />);
    await userEvent.type(screen.getByLabelText(/child id/i), "C-1");
    await userEvent.click(screen.getByRole("button", { name: /preview/i }));

    expect(await screen.findByText(/renderer unavailable/i)).toBeInTheDocument();
    expect(screen.queryByText(/STOP-REVIEW/)).toBeNull();
    expect(screen.queryByText(/clinical/i)).toBeNull();
  });

  it("lists every omission the API reported", async () => {
    vi.stubGlobal("fetch", mockFetch({
      status: 200,
      body: "<html><body>book</body></html>",
      omissions: "B1-009 vaccination schedule: no drafted text permitted; B1-014 development by age: no drafted text permitted",
    }));
    render(<BookGenerator />);
    await userEvent.type(screen.getByLabelText(/child id/i), "C-1");
    await userEvent.click(screen.getByRole("button", { name: /preview/i }));

    expect(await screen.findByText(/B1-009 vaccination schedule/)).toBeInTheDocument();
    expect(screen.getByText(/B1-014 development by age/)).toBeInTheDocument();
  });
});
