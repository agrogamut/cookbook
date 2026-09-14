import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { BookGenerator } from "./book-generator";

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

function jsonResponse(status: number, body: string) {
  return {
    ok: status >= 200 && status < 300,
    status,
    headers: { get: () => null },
    text: async () => body,
    json: async () => JSON.parse(body || "{}"),
  };
}

/** One book's print result as generate.printed sends it: the PDF base64-encoded, or an error
 *  of kind "unavailable" (no browser) or "print-failed" (a browser was there and failed). */
function printResult(status: number, errorBody?: string) {
  if (status >= 200 && status < 300) return { pdf: btoa("%PDF-mock") };
  const message = errorBody ? JSON.parse(errorBody).error : "print failed";
  return { error: { kind: status === 503 ? "unavailable" : "print-failed", message } };
}

/** Routes fetch calls by URL shape: reference-data calls the form loads on mount get an empty
 *  list so the form itself renders; /api/books/generate.printed is the one generation request,
 *  carrying both books' HTML and both print results. Each book's print is independently
 *  configurable, because a PDF can fail while the set does not. */
function mockFetch(opts: {
  generateStatus?: number;
  generateBody?: string;
  book1Status?: number;
  book2Status?: number;
  book1Body?: string;
  book2Body?: string;
} = {}) {
  const {
    generateStatus = 200,
    generateBody = setBody(),
    book1Status = 200,
    book2Status = 200,
    book1Body,
    book2Body,
  } = opts;
  return vi.fn().mockImplementation((url: string) => {
    const u = String(url);
    if (!u.includes("/api/books/")) {
      return Promise.resolve(jsonResponse(200, "[]"));
    }
    if (generateStatus !== 200) {
      return Promise.resolve(jsonResponse(generateStatus, generateBody));
    }
    const body = JSON.stringify({
      ...JSON.parse(generateBody),
      book1_print: printResult(book1Status, book1Body),
      book2_print: printResult(book2Status, book2Body),
    });
    return Promise.resolve(jsonResponse(200, body));
  });
}

// The form takes the child's details inline; date of birth is the only required one, and the
// Generate button stays disabled without it.
async function generate(dob = "2022-05-01") {
  render(<BookGenerator />);
  await userEvent.type(screen.getByLabelText(/date of birth/i), dob);
  await userEvent.click(screen.getByRole("button", { name: /generate both books/i }));
}

let objectUrlSeq = 0;
let createObjectURLSpy: ReturnType<typeof vi.fn>;
let revokeObjectURLSpy: ReturnType<typeof vi.fn>;

beforeEach(() => {
  objectUrlSeq = 0;
  vi.stubGlobal("fetch", mockFetch());
  // jsdom does not implement the object URL registry at all (createObjectURL is not a
  // function), so every test that reaches the PDF-embedding path needs it stubbed regardless
  // of what that test is asserting.
  createObjectURLSpy = vi.fn(() => `blob:mock-${++objectUrlSeq}`);
  revokeObjectURLSpy = vi.fn();
  URL.createObjectURL = createObjectURLSpy as unknown as typeof URL.createObjectURL;
  URL.revokeObjectURL = revokeObjectURLSpy as unknown as typeof URL.revokeObjectURL;
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("BookGenerator", () => {
  it("produces both books from one run and switches between them without refetching", async () => {
    const fetchMock = mockFetch();
    vi.stubGlobal("fetch", fetchMock);
    await generate();

    // Both books print alongside the set, so the printed PDF -- not the HTML -- is what is on
    // screen for the active tab by the time generation settles.
    const frame = await screen.findByTitle("Book 1 preview");
    await waitFor(() => expect(frame.getAttribute("src")).toMatch(/^blob:mock-/));

    const bookCalls = () =>
      fetchMock.mock.calls.filter((c) => String(c[0]).includes("/api/books/")).length;
    // One request carries the set and both prints, so the server assembles once per click;
    // generating never re-runs on a tab switch either.
    expect(bookCalls()).toBe(1);

    await userEvent.click(screen.getByRole("tab", { name: /book 2/i }));
    const book2Frame = await screen.findByTitle("Book 2 preview");
    await waitFor(() => expect(book2Frame.getAttribute("src")).toMatch(/^blob:mock-/));
    expect(bookCalls()).toBe(1);
  });

  // The clinical stop this used to assert is gone (SP1: see
  // docs/superpowers/specs/2026-09-05-direct-generation-design.md). Nothing returns 409, so
  // what is worth pinning now is the negative: the console carries no stop-gate state at all,
  // and a selected special-care condition produces books like any other profile rather than
  // an explanation of why it will not.
  it("shows no clinical stop state, because nothing stops generation", async () => {
    vi.stubGlobal("fetch", mockFetch());
    await generate();

    expect(await screen.findByTitle("Book 1 preview")).toBeInTheDocument();
    expect(screen.queryByText(/STOP-REVIEW/)).toBeNull();
    expect(screen.queryByText(/stopped by a clinical rule/i)).toBeNull();
    expect(screen.queryByText(/generation will halt/i)).toBeNull();
  });

  it("distinguishes an unavailable renderer from a print failure", async () => {
    vi.stubGlobal("fetch", mockFetch({
      generateStatus: 503,
      generateBody: JSON.stringify({ error: "headless chromium unavailable" }),
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
      generateBody: setBody({
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

  it("embeds the printed PDF, not the HTML", async () => {
    await generate();

    const frame = await screen.findByTitle("Book 1 preview");
    await waitFor(() => expect(frame.getAttribute("src")).toMatch(/^blob:mock-/));
    // The HTML iframe and the PDF iframe share a title, so the only reliable way to tell which
    // one rendered is that a PDF iframe never carries srcdoc.
    expect(frame.getAttribute("srcdoc")).toBeNull();
    expect(frame).not.toHaveAttribute("sandbox");
  });

  it("falls back to the HTML preview when no browser is available, and says so", async () => {
    vi.stubGlobal("fetch", mockFetch({
      book1Status: 503,
      book1Body: JSON.stringify({ error: "headless chromium unavailable" }),
      book2Status: 503,
      book2Body: JSON.stringify({ error: "headless chromium unavailable" }),
    }));
    await generate();

    const frame = await screen.findByTitle("Book 1 preview");
    expect(frame.getAttribute("srcdoc")).toContain("daily life");
    expect(frame.getAttribute("src")).toBeFalsy();

    // Explained as an approximation, not raised as an error: the set itself printed fine and
    // the HTML preview genuinely works, so this is not the same situation as generation itself
    // failing.
    expect(screen.getByText(/it cannot paginate/i)).toBeInTheDocument();
    expect(screen.queryByText(/renderer unavailable/i)).toBeNull();
    expect(screen.queryByRole("alert")).toBeNull();

    // Nothing to open for a book with no printed PDF.
    expect(screen.getByRole("button", { name: /open book 1 pdf/i })).toBeDisabled();
  });

  it("surfaces a non-503 print failure as an error, unlike the renderer-unavailable case", async () => {
    vi.stubGlobal("fetch", mockFetch({
      book1Status: 500,
      book1Body: JSON.stringify({ error: "pdf render failed: context deadline exceeded" }),
    }));
    await generate();

    // A browser was available and the print itself failed -- this is not the same as no
    // browser being installed, and must not be absorbed into the quiet HTML fallback.
    expect(await screen.findByText(/print failed/i)).toBeInTheDocument();
    expect(screen.queryByText(/it cannot paginate/i)).toBeNull();
  });

  it("opens the tab from the blob it already has, without printing again", async () => {
    const fetchMock = mockFetch();
    vi.stubGlobal("fetch", fetchMock);
    const fakeTab = { closed: false } as unknown as Window;
    const openSpy = vi.spyOn(window, "open").mockReturnValue(fakeTab);

    await generate();
    const button = await screen.findByRole("button", { name: /open book 1 pdf/i });
    await waitFor(() => expect(button).toBeEnabled());

    const bookCalls = () =>
      fetchMock.mock.calls.filter((c) => String(c[0]).includes("/api/books/")).length;
    expect(bookCalls()).toBe(1);

    await userEvent.click(button);

    // No second print: the tab is opened directly on the object URL already resolved while
    // generating the set.
    expect(bookCalls()).toBe(1);
    expect(openSpy).toHaveBeenCalledTimes(1);
    const [openedUrl] = openSpy.mock.calls[0];
    expect(openedUrl).toMatch(/^blob:mock-/);

    openSpy.mockRestore();
  });

  it("packages the zip from the PDFs it already has, without generating again", async () => {
    const fetchMock = mockFetch();
    vi.stubGlobal("fetch", fetchMock);
    await generate();
    const button = await screen.findByRole("button", { name: /download .zip/i });
    await waitFor(() => expect(button).toBeEnabled());
    const clickSpy = vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(() => {});

    await userEvent.click(button);

    await waitFor(() => expect(clickSpy).toHaveBeenCalledTimes(1));
    expect(fetchMock.mock.calls.filter((c) => String(c[0]).includes("/api/books/")).length).toBe(1);
    clickSpy.mockRestore();
  });

  it("revokes the previous object URLs when a new set is generated", async () => {
    await generate();
    await waitFor(() => expect(createObjectURLSpy).toHaveBeenCalledTimes(2));
    const firstRunUrls = createObjectURLSpy.mock.results.map((r) => r.value as string);

    await userEvent.click(screen.getByRole("button", { name: /generate both books/i }));

    await waitFor(() => expect(createObjectURLSpy).toHaveBeenCalledTimes(4));
    // The first run's two PDF URLs are released once the second run's replace them; the second
    // run's own URLs are not touched by this generation.
    const revoked = revokeObjectURLSpy.mock.calls.map((c) => c[0]);
    expect(revoked).toEqual(expect.arrayContaining(firstRunUrls));
    const secondRunUrls = createObjectURLSpy.mock.results.slice(2).map((r) => r.value as string);
    expect(revoked).not.toEqual(expect.arrayContaining(secondRunUrls));
  });
});
