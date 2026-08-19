"use client";

import { useState } from "react";
import {
  getBookPreview, getBookPdf, BookBlockedError, RendererUnavailableError,
  PrintFailedError,
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

export function BookGenerator() {
  const [childID, setChildID] = useState("");
  const [book, setBook] = useState<"book1" | "book2">("book1");
  const [html, setHtml] = useState<string | null>(null);
  const [omissions, setOmissions] = useState<string[]>([]);
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

  async function preview() {
    setBusy(true);
    setProblem(null);
    setHtml(null);
    setOmissions([]);
    try {
      const result = await getBookPreview(childID, book);
      setHtml(result.html);
      setOmissions(result.omissions);
    } catch (err) {
      setProblem(classify(err));
    } finally {
      setBusy(false);
    }
  }

  async function download() {
    setBusy(true);
    setProblem(null);
    try {
      const blob = await getBookPdf(childID, book);
      const url = URL.createObjectURL(blob);
      const a = document.createElement("a");
      a.href = url;
      a.download = `${childID}-${book}.pdf`;
      a.click();
      URL.revokeObjectURL(url);
    } catch (err) {
      setProblem(classify(err));
    } finally {
      setBusy(false);
    }
  }

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
        <Tabs value={book} onValueChange={(v) => setBook(v as "book1" | "book2")}>
          <TabsList>
            <TabsTrigger value="book1">Book 1 &middot; daily life</TabsTrigger>
            <TabsTrigger value="book2">Book 2 &middot; recipes</TabsTrigger>
          </TabsList>
        </Tabs>
        <Button onClick={preview} disabled={!childID || busy}>Preview</Button>
        {html !== null && (
          <Button variant="outline" onClick={download} disabled={busy}>Download PDF</Button>
        )}
      </div>

      {problem?.kind === "blocked" && (
        <Alert className="border-[var(--color-blocked,theme(colors.amber.600))]">
          <AlertTitle>Generation stopped by a clinical rule</AlertTitle>
          <AlertDescription className="space-y-1">
            <p>{problem.message}</p>
            {problem.reviewer && <p className="font-mono text-xs">Reviewer: {problem.reviewer}</p>}
            <p className="text-xs text-muted-foreground">
              This is the provider&apos;s stop gate, not a fault. There is no override.
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

      {omissions.length > 0 && (
        <div className="rounded border p-3">
          <div className="mb-2 flex items-center gap-2">
            <Badge variant="outline">{omissions.length} omitted</Badge>
            <span className="text-xs text-muted-foreground">
              Facts the book does not contain, reported by the assembler.
            </span>
          </div>
          <ul className="space-y-1 font-mono text-xs">
            {omissions.map((o) => <li key={o}>{o}</li>)}
          </ul>
        </div>
      )}

      {html !== null && (
        <iframe
          title="Book preview"
          sandbox=""
          srcDoc={html}
          className="h-[80vh] w-full rounded border bg-white"
        />
      )}
    </div>
  );
}
