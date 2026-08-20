"use client";

import { useEffect, useRef, useState } from "react";
import {
  generateBooks, generateBooksZip, generateBookPdf, BookSet, GenerateInput,
  BookBlockedError, RendererUnavailableError, PrintFailedError,
} from "@/lib/api";
import { ChildInputForm } from "@/components/child-input-form";
import { Button } from "@/components/ui/button";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Badge } from "@/components/ui/badge";

type Blocked = { kind: "blocked"; message: string; reviewer?: string };
type Unavailable = { kind: "unavailable"; message: string };
type PrintFailed = { kind: "print-failed"; message: string };
type Failed = { kind: "failed"; message: string };
type Problem = Blocked | Unavailable | PrintFailed | Failed;

type Which = "book1" | "book2";

const NO_PDFS: Record<Which, string | null> = { book1: null, book2: null };

export function BookGenerator() {
  // The inputs that produced the books on screen. Held so a download re-sends exactly what
  // was generated: re-reading the form would let an edit made after Generate slip into a PDF
  // that does not match the preview beside it.
  const [submitted, setSubmitted] = useState<GenerateInput | null>(null);
  // The tab selects which of the two generated books is on screen. It is a view control, not
  // a generation control: a run always produces both, so switching tabs never refetches.
  const [shown, setShown] = useState<Which>("book1");
  const [set, setSet] = useState<BookSet | null>(null);
  const [problem, setProblem] = useState<Problem | null>(null);
  const [busy, setBusy] = useState(false);

  // The printed PDF for each book of the current run, as object URLs. Populated after
  // generateBooks resolves, by printing both books in parallel -- printing is a single
  // warm-Chromium render now that the browser is shared across requests, so paying for it on
  // every generation (rather than only when an operator clicks "open") is what lets the console
  // show the real document instead of an approximation of it. A null entry means this book's
  // PDF is not on screen, either because printing has not finished or because it failed.
  const [pdfUrls, setPdfUrls] = useState<Record<Which, string | null>>(NO_PDFS);
  // Set when a PDF could not be printed because the server has no browser installed. This is
  // not an error: the HTML preview this component already holds genuinely works, so the
  // operator gets that instead of a dead end, with this note explaining why it is not the real
  // page. Any other printing failure is surfaced through `problem` instead, because it is not
  // safe to wave away the same way an absent browser is.
  const [pdfFallbackNote, setPdfFallbackNote] = useState<string | null>(null);

  // Mirrors `pdfUrls` outside of React state so the object URLs from the run being replaced or
  // torn down are reachable for revocation without depending on a stale closure over state.
  const heldPdfUrls = useRef<Record<Which, string | null>>(NO_PDFS);

  function revokeHeldPdfUrls() {
    const held = heldPdfUrls.current;
    (Object.keys(held) as Which[]).forEach((book) => {
      const url = held[book];
      if (url) URL.revokeObjectURL(url);
    });
    heldPdfUrls.current = NO_PDFS;
  }

  // Two PDFs per run, held for the component's life: without this an operator running twenty
  // lookups an hour leaks forty documents. Unmount is the other side of that -- the tab-close
  // equivalent for a console the operator simply navigates away from.
  useEffect(() => () => revokeHeldPdfUrls(), []);

  function classify(err: unknown): Problem {
    if (err instanceof BookBlockedError) {
      return { kind: "blocked", message: err.message, reviewer: err.reviewer };
    }
    if (err instanceof RendererUnavailableError) {
      return { kind: "unavailable", message: err.message };
    }
    if (err instanceof PrintFailedError) {
      return { kind: "print-failed", message: err.message };
    }
    return { kind: "failed", message: err instanceof Error ? err.message : String(err) };
  }

  async function generate(input: GenerateInput) {
    setBusy(true);
    setProblem(null);
    setSet(null);
    setSubmitted(null);
    setPdfFallbackNote(null);
    // A new run replaces whatever PDFs are on screen, so the previous pair is released here
    // rather than left to accumulate. This is safe even though the operator may still have a
    // tab open on one of them: revoking an object URL only removes the registry entry that lets
    // a *future* navigation dereference that exact string again. It does not unload a document
    // a browsing context already navigated to and loaded -- that data stays alive for as long as
    // the tab or iframe holding it does. A tab opened on a prior run's PDF keeps showing it;
    // only reopening that same stale link afterwards would fail, and nothing here does that.
    revokeHeldPdfUrls();
    setPdfUrls(NO_PDFS);
    try {
      const bookSet = await generateBooks(input);
      setSet(bookSet);
      setSubmitted(input);

      const [book1Result, book2Result] = await Promise.allSettled([
        generateBookPdf(input, "book1"),
        generateBookPdf(input, "book2"),
      ]);

      const nextUrls: Record<Which, string | null> = { book1: null, book2: null };
      let fallbackMessage: string | null = null;
      let printProblem: Problem | null = null;

      for (const [book, result] of [
        ["book1", book1Result],
        ["book2", book2Result],
      ] as const) {
        if (result.status === "fulfilled") {
          nextUrls[book] = URL.createObjectURL(result.value);
          continue;
        }
        const classified = classify(result.reason);
        if (classified.kind === "unavailable") {
          fallbackMessage = classified.message;
        } else {
          // A browser was available and printing still failed, or the request failed for some
          // other reason. Neither is the "no browser installed" case the HTML preview quietly
          // stands in for, so both must reach the operator as an error.
          printProblem = classified;
        }
      }

      heldPdfUrls.current = nextUrls;
      setPdfUrls(nextUrls);
      if (fallbackMessage) setPdfFallbackNote(fallbackMessage);
      if (printProblem) setProblem(printProblem);
    } catch (err) {
      setProblem(classify(err));
    } finally {
      setBusy(false);
    }
  }

  function save(blob: Blob, filename: string) {
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = filename;
    a.click();
    URL.revokeObjectURL(url);
  }

  // Open a printed PDF in a new tab rather than putting it in the downloads folder.
  //
  // The browser's own viewer gives print, save, zoom and search for nothing, and an operator
  // checking a book before handing it over should not accumulate a file per attempt. The zip
  // still downloads: an archive has nothing to view.
  //
  // This used to re-print through generateBookPdf and hand the fresh blob to the tab. It no
  // longer does: by the time this button is clickable, the component already printed this exact
  // PDF while generating the set, so there is nothing left to do but point a new tab at the
  // object URL already on screen. That also removes the only way the tab and the preview could
  // ever show different bytes for the same run.
  function openInTab(book: Which) {
    const url = pdfUrls[book];
    if (!url) return; // Nothing printed for this book -- the button is disabled whenever this
    // is null, so reaching here without a URL should not happen.

    // window.open must be called synchronously from the click handler, not after an await:
    // called later it is not attributable to the click that caused it and every popup blocker
    // eats it, which presents as a button that does nothing at all. Since the PDF is already a
    // resolved object URL rather than a pending fetch, that synchronous call can go straight to
    // the real document instead of opening a blank tab and filling it in later.
    const tab = window.open(url, "_blank");
    if (tab === null) {
      // Blocked anyway, or opened in a context that cannot. Fall back to downloading the same
      // blob directly rather than leaving the button silently dead -- and rather than fetching
      // the PDF a second time, since it is already sitting in memory under this object URL.
      const a = document.createElement("a");
      a.href = url;
      a.download = `${fileStem()}-${book}.pdf`;
      a.click();
    }
  }

  function fileStem() {
    const name = (submitted?.display_name ?? "").toLowerCase()
      .replace(/[^a-z0-9]+/g, "-").replace(/^-+|-+$/g, "");
    return name || "books";
  }

  async function downloadBoth() {
    if (!submitted) return;
    setBusy(true);
    setProblem(null);
    try {
      save(await generateBooksZip(submitted), `${fileStem()}-books.zip`);
    } catch (err) {
      setProblem(classify(err));
    } finally {
      setBusy(false);
    }
  }

  const html = set === null ? null : shown === "book1" ? set.book1Html : set.book2Html;
  const pdfUrl = pdfUrls[shown];
  const bookOmissions =
    set === null ? [] : shown === "book1" ? set.book1Omissions : set.book2Omissions;
  const profileOmissions = set?.profileOmissions ?? [];

  return (
    <div className="space-y-4">
      <ChildInputForm busy={busy} onGenerate={generate} />

      {set !== null && (
        <div className="flex flex-wrap items-center gap-3 border-t pt-4">
          <span className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">
            Print
          </span>
          {/* Both books are always offered, not only the one whose tab happens to be open:
              the run produced both, so reaching one should not require switching tabs first.
              These open the PDF; the zip below downloads, because an archive has nothing to
              view. Disabled while its own PDF is not yet on screen, which also covers the
              renderer-unavailable case -- there is nothing for the button to open. */}
          <Button
            variant="outline"
            onClick={() => openInTab("book1")}
            disabled={busy || !pdfUrls.book1}
          >
            Open Book 1 PDF
          </Button>
          <Button
            variant="outline"
            onClick={() => openInTab("book2")}
            disabled={busy || !pdfUrls.book2}
          >
            Open Book 2 PDF
          </Button>
          <Button variant="outline" onClick={downloadBoth} disabled={busy}>
            Download both (.zip)
          </Button>
        </div>
      )}

      {set !== null && (
        <div className="flex flex-wrap items-center gap-3">
          <Tabs value={shown} onValueChange={(v) => setShown(v as Which)}>
            <TabsList>
              <TabsTrigger value="book1">Book 1 &middot; daily life</TabsTrigger>
              <TabsTrigger value="book2">Book 2 &middot; recipes</TabsTrigger>
            </TabsList>
          </Tabs>
          {/* The run's identity, so an operator comparing two printed books can tell whether
              they came from the same generation. */}
          <span className="font-mono text-xs text-muted-foreground">
            {set.childID} &middot; generated {set.asOf}
          </span>
        </div>
      )}

      {problem?.kind === "blocked" && (
        <Alert className="border-[var(--color-blocked,theme(colors.amber.600))]">
          <AlertTitle>Generation stopped by a clinical rule</AlertTitle>
          <AlertDescription className="space-y-1">
            <p>{problem.message}</p>
            {problem.reviewer && <p className="font-mono text-xs">Reviewer: {problem.reviewer}</p>}
            <p className="text-xs text-muted-foreground">
              This is the provider&apos;s stop gate, not a fault. There is no override, and it
              withholds both books rather than only the recipe book.
            </p>
          </AlertDescription>
        </Alert>
      )}

      {problem?.kind === "unavailable" && (
        <Alert variant="destructive">
          <AlertTitle>Renderer unavailable</AlertTitle>
          <AlertDescription>
            <p>{problem.message}</p>
            <p className="text-xs">
              An operational fault in the print pipeline. Nothing about this child changed.
            </p>
          </AlertDescription>
        </Alert>
      )}

      {problem?.kind === "print-failed" && (
        <Alert variant="destructive">
          <AlertTitle>Print failed</AlertTitle>
          <AlertDescription>
            <p>{problem.message}</p>
            <p className="text-xs">
              A browser was available and the print itself failed. This may be about this
              document rather than the pipeline, so a retry may not clear it.
            </p>
          </AlertDescription>
        </Alert>
      )}

      {problem?.kind === "failed" && (
        <Alert variant="destructive">
          <AlertTitle>Request failed</AlertTitle>
          <AlertDescription>{problem.message}</AlertDescription>
        </Alert>
      )}

      {/* Profile omissions hold for both books, so they stay on screen as the tab changes.
          Book omissions are that book's own coverage and follow the tab. Keeping them apart
          is the whole reason the API splits them. */}
      {profileOmissions.length > 0 && (
        <div className="rounded border p-3">
          <div className="mb-2 flex items-center gap-2">
            <Badge variant="outline">{profileOmissions.length} profile</Badge>
            <span className="text-xs text-muted-foreground">
              Facts about this child that apply to both books.
            </span>
          </div>
          <ul className="space-y-1 font-mono text-xs">
            {profileOmissions.map((o) => <li key={o}>{o}</li>)}
          </ul>
        </div>
      )}

      {bookOmissions.length > 0 && (
        <div className="rounded border p-3">
          <div className="mb-2 flex items-center gap-2">
            <Badge variant="outline">
              {bookOmissions.length} omitted from {shown === "book1" ? "Book 1" : "Book 2"}
            </Badge>
            <span className="text-xs text-muted-foreground">
              Facts this book does not contain, reported by the assembler.
            </span>
          </div>
          <ul className="space-y-1 font-mono text-xs">
            {bookOmissions.map((o) => <li key={o}>{o}</li>)}
          </ul>
        </div>
      )}

      {/* The printed PDF is what a family actually receives, so it is what the console shows
          whenever one is on screen. No `sandbox` here: the content is a browser-native PDF
          rendered from local bytes, not a document capable of running script in this origin,
          and sandboxing an iframe to an opaque origin risks disabling the viewer's own
          print/zoom controls for no safety this content needs. */}
      {pdfUrl !== null && (
        <iframe
          title={shown === "book1" ? "Book 1 preview" : "Book 2 preview"}
          src={pdfUrl}
          className="h-[80vh] w-full rounded border bg-white"
        />
      )}

      {/* Falls back to the HTML preview only when this book's own PDF did not print. The HTML
          preview is real -- it is print geometry rendered at screen width, not a mock -- so this
          is presented as an approximation with a reason, never as an error. */}
      {pdfUrl === null && html !== null && (
        <>
          {pdfFallbackNote && (
            <p className="text-xs text-muted-foreground">
              Showing the HTML preview, not the printed PDF ({pdfFallbackNote}). It lays out at
              the printed page width with the real margins, but it cannot paginate: a section
              that will run onto two sheets appears here as one long sheet.
            </p>
          )}
          <iframe
            title={shown === "book1" ? "Book 1 preview" : "Book 2 preview"}
            sandbox=""
            srcDoc={html}
            className="h-[80vh] w-full rounded border bg-white"
          />
        </>
      )}
    </div>
  );
}
