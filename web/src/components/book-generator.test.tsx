import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { BookGenerator } from "./book-generator";

// The form loads its option lists on mount. Those calls share this mock, so it answers a
// reference request with an empty list and the generate request with the body under test --
// otherwise every test would be exercising a form whose selects failed to populate.
function mockFetch(init: { status: number; body?: string }) {
  return vi.fn().mockImplementation((url: string) => {
    if (!String(url).includes("/api/books/")) {
      return Promise.resolve({
        ok: true, status: 200, headers: { get: () => null },
        text: async () => "[]", json: async () => [],
      });
    }
    return Promise.resolve({
      ok: init.status >= 200 && init.status < 300,
      status: init.status,
      headers: { get: () => null },
      text: async () => init.body ?? "",
      json: async () => JSON.parse(init.body ?? "{}"),
    });
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

// The form takes the child's details inline; date of birth is the only required one, and the
// Generate button stays disabled without it.
async function generate(dob = "2022-05-01") {
  render(<BookGenerator />);
  await userEvent.type(screen.getByLabelText(/date of birth/i), dob);
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
    const bookCalls = () =>
      fetchMock.mock.calls.filter((c) => String(c[0]).includes("/api/books/")).length;
    expect(bookCalls()).toBe(1);

    // The tab is a view control over an already-generated pair, not a second generation.
    await userEvent.click(screen.getByRole("tab", { name: /book 2/i }));
    expect((await screen.findByTitle("Book 2 preview")).getAttribute("srcdoc"))
      .toContain("recipes");
    expect(bookCalls()).toBe(1);
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

    const alert = (await screen.findByText(/renderer unavailable/i)).closest("[role=alert]");
    expect(alert).not.toBeNull();
    // Scoped to the alert, not the page: the input form has its own "Clinical" section, and
    // what matters is that this alert never reads as a clinical decision.
    expect(alert!.textContent).not.toMatch(/clinical/i);
    expect(alert!.textContent).not.toMatch(/STOP-REVIEW/);
    expect(screen.queryByText(/STOP-REVIEW/)).toBeNull();
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
