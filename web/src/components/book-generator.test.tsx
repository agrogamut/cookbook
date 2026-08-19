import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { BookGenerator } from "./book-generator";

function mockFetch(init: { status: number; body?: string }) {
  return vi.fn().mockResolvedValue({
    ok: init.status >= 200 && init.status < 300,
    status: init.status,
    headers: { get: () => null },
    text: async () => init.body ?? "",
    json: async () => JSON.parse(init.body ?? "{}"),
  });
}

/** A set response as the API sends it. Both books always present: that is the product rule
 *  the endpoint exists to carry, so a fixture that omits one would not be testing the app. */
function setBody(over: Partial<{
  book1_html: string; book2_html: string;
  profile_omissions: string[]; book1_omissions: string[]; book2_omissions: string[];
}> = {}) {
  return JSON.stringify({
    child_id: "C-1",
    as_of: "2026-08-19T00:00:00Z",
    book1_html: "<html><body>daily life</body></html>",
    book2_html: "<html><body>recipes</body></html>",
    profile_omissions: [],
    book1_omissions: [],
    book2_omissions: [],
    ...over,
  });
}

async function generate(childID = "C-1") {
  render(<BookGenerator />);
  await userEvent.type(screen.getByLabelText(/child id/i), childID);
  await userEvent.click(screen.getByRole("button", { name: /generate both books/i }));
}

beforeEach(() => { vi.stubGlobal("fetch", mockFetch({ status: 200, body: setBody() })); });
afterEach(() => { vi.unstubAllGlobals(); });

describe("BookGenerator", () => {
  it("produces both books from one run and switches between them without refetching", async () => {
    const fetchMock = mockFetch({ status: 200, body: setBody() });
    vi.stubGlobal("fetch", fetchMock);
    await generate();

    const frame = await screen.findByTitle("Book 1 preview");
    expect(frame.getAttribute("srcdoc")).toContain("daily life");
    expect(fetchMock).toHaveBeenCalledTimes(1);

    // The tab is a view control over an already-generated pair, not a second generation.
    await userEvent.click(screen.getByRole("tab", { name: /book 2/i }));
    expect((await screen.findByTitle("Book 2 preview")).getAttribute("srcdoc"))
      .toContain("recipes");
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  it("renders a clinical stop, not an error, when the engine blocks the child", async () => {
    vi.stubGlobal("fetch", mockFetch({
      status: 409,
      body: JSON.stringify({
        error: "Down syndrome is a STOP-REVIEW condition",
        reviewer: "Pediatrician + dietitian",
      }),
    }));
    await generate();

    expect(await screen.findByText(/STOP-REVIEW/)).toBeInTheDocument();
    expect(screen.getByText(/Pediatrician \+ dietitian/)).toBeInTheDocument();
    // A stop is a clinical decision, so the operator is never offered the artifact anyway --
    // and it withholds both books, so neither download appears.
    expect(screen.queryByRole("button", { name: /download/i })).toBeNull();
  });

  it("distinguishes an unavailable renderer from a clinical stop", async () => {
    vi.stubGlobal("fetch", mockFetch({
      status: 503,
      body: JSON.stringify({ error: "headless chromium unavailable" }),
    }));
    await generate();

    expect(await screen.findByText(/renderer unavailable/i)).toBeInTheDocument();
    expect(screen.queryByText(/STOP-REVIEW/)).toBeNull();
    expect(screen.queryByText(/clinical/i)).toBeNull();
  });

  it("separates omissions that are facts about the child from omissions of one book", async () => {
    vi.stubGlobal("fetch", mockFetch({
      status: 200,
      body: setBody({
        profile_omissions: ["Peanut is suspected, not confirmed"],
        book1_omissions: ["[block] B1-009 vaccination schedule: no drafted text permitted"],
        book2_omissions: ["[meal category] MC-04 has no recipes mapped to it at all"],
      }),
    }));
    await generate();

    // The profile fact holds for both books, so it stays on screen across the tab switch;
    // each book's own omission appears only under that book.
    expect(await screen.findByText(/Peanut is suspected/)).toBeInTheDocument();
    expect(screen.getByText(/B1-009 vaccination schedule/)).toBeInTheDocument();
    expect(screen.queryByText(/MC-04/)).toBeNull();

    await userEvent.click(screen.getByRole("tab", { name: /book 2/i }));
    expect(await screen.findByText(/MC-04/)).toBeInTheDocument();
    expect(screen.getByText(/Peanut is suspected/)).toBeInTheDocument();
    expect(screen.queryByText(/B1-009/)).toBeNull();
  });
});
