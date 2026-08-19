"use client";

import { useState } from "react";
import {
  getBookSet, getBookSetZip, getBookPdf, BookSet,
  BookBlockedError, RendererUnavailableError, PrintFailedError,
} from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Badge } from "@/components/ui/badge";

type Blocked = { kind: "blocked"; message: string; reviewer?: string };
type Unavailable = { kind: "unavailable"; message: string };
type PrintFailed = { kind: "print-failed"; message: string };
type Failed = { kind: "failed"; message: string };
type Problem = Blocked | Unavailable | PrintFailed | Failed;

type Which = "book1" | "book2";

export function BookGenerator() {
  const [childID, setChildID] = useState("");
  // The tab selects which of the two generated books is on screen. It is a view control, not
  // a generation control: a run always produces both, so switching tabs never refetches.
  const [shown, setShown] = useState<Which>("book1");
  const [set, setSet] = useState<BookSet | null>(null);
  const [problem, setProblem] = useState<Problem | null>(null);
  const [busy, setBusy] = useState(false);

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

  async function generate() {
    setBusy(true);
    setProblem(null);
    setSet(null);
    try {
      setSet(await getBookSet(childID));
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

  async function downloadBoth() {
    setBusy(true);
    setProblem(null);
    try {
      save(await getBookSetZip(childID), `${childID}-books.zip`);
    } catch (err) {
      setProblem(classify(err));
    } finally {
      setBusy(false);
    }
  }

  async function downloadOne(book: Which) {
    setBusy(true);
    setProblem(null);
    try {
      save(await getBookPdf(childID, book), `${childID}-${book}.pdf`);
    } catch (err) {
      setProblem(classify(err));
    } finally {
      setBusy(false);
    }
  }

  const html = set === null ? null : shown === "book1" ? set.book1Html : set.book2Html;
  const bookOmissions =
    set === null ? [] : shown === "book1" ? set.book1Omissions : set.book2Omissions;
  const profileOmissions = set?.profileOmissions ?? [];

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-end gap-3">
        <div className="space-y-1">
          <Label htmlFor="child-id" className="text-xs uppercase tracking-wide">Child ID</Label>
          <Input
            id="child-id"
            className="w-56 font-mono"
            value={childID}
            onChange={(e) => setChildID(e.target.value)}
            placeholder="C-0001"
          />
        </div>
        <Button onClick={generate} disabled={!childID || busy}>
          {busy ? "Generating…" : "Generate both books"}
        </Button>
        {set !== null && (
          <>
            <Button variant="outline" onClick={downloadBoth} disabled={busy}>
              Download both (.zip)
            </Button>
            {/* Both single downloads are always offered, not just the tab that happens to be
                open: a run produced both books, so an operator who wants one of them should
                not have to switch tabs to reach it. */}
            <Button variant="ghost" onClick={() => downloadOne("book1")} disabled={busy}>
              Book 1 PDF
            </Button>
            <Button variant="ghost" onClick={() => downloadOne("book2")} disabled={busy}>
              Book 2 PDF
            </Button>
          </>
        )}
      </div>

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

      {html !== null && (
        <iframe
          title={shown === "book1" ? "Book 1 preview" : "Book 2 preview"}
          sandbox=""
          srcDoc={html}
          className="h-[80vh] w-full rounded border bg-white"
        />
      )}
    </div>
  );
}
