"use client";

import { useState } from "react";
import { search, ApiError } from "@/lib/api";
import type { ChildProfile, EngineResult } from "@/lib/types";
import { ProfileForm } from "@/components/profile-form";
import { ResultsTable } from "@/components/results-table";
import { WhyThisResultSheet } from "@/components/why-this-result-sheet";
import { UnscreenedAllergenAlert } from "@/components/unscreened-allergen-alert";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Skeleton } from "@/components/ui/skeleton";
import { PageHeader } from "@/components/page-header";
import {
  ResizableHandle, ResizablePanel, ResizablePanelGroup,
} from "@/components/ui/resizable";

export default function EngineConsolePage() {
  const [result, setResult] = useState<EngineResult | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function handleSearch(profile: ChildProfile) {
    setLoading(true);
    setError(null);
    try {
      setResult(await search(profile));
    } catch (e) {
      setError(e instanceof ApiError ? e.message : "The search failed. Check that the API server is running.");
    } finally {
      setLoading(false);
    }
  }

  return (
    <div className="flex h-full flex-col">
      <PageHeader
        title="Engine console"
        description="Enter a child profile and search. Age is the only required field."
      />
      {/* react-resizable-panels v4 names the axis `orientation`, and reads a bare number
          as pixels -- sizes are given as percentage strings so the split stays
          proportional rather than pinning the form to 26px. */}
      <ResizablePanelGroup orientation="horizontal" className="min-h-0 flex-1">
        <ResizablePanel defaultSize="26%" minSize="18%" maxSize="45%">
          <div className="h-full pr-4">
            <ProfileForm onSubmit={handleSearch} loading={loading} />
          </div>
        </ResizablePanel>
        <ResizableHandle withHandle />
        <ResizablePanel defaultSize="74%">
          <section className="h-full overflow-y-auto pl-4">
            {error && (
              <Alert variant="destructive" className="mb-4">
                <AlertTitle>Search failed</AlertTitle>
                <AlertDescription>{error}</AlertDescription>
              </Alert>
            )}

            {!loading && result?.unscreened_allergens && (
              <UnscreenedAllergenAlert groups={result.unscreened_allergens} />
            )}

            {loading && (
              <div className="space-y-2">
                <Skeleton className="h-8 w-full" />
                <Skeleton className="h-8 w-full" />
                <Skeleton className="h-8 w-full" />
              </div>
            )}

            {!loading && result?.blocked && (
              <Alert variant="destructive">
                <AlertTitle>Automated recipe generation blocked</AlertTitle>
                <AlertDescription>{result.block_reason}</AlertDescription>
              </Alert>
            )}

            {!loading && result && !result.blocked && result.recipes.length === 0 && (
              <Alert>
                <AlertTitle>No recipes matched</AlertTitle>
                <AlertDescription>
                  Every step is recorded in &quot;Why this result&quot; -- open it to see which step
                  emptied the pool.
                </AlertDescription>
              </Alert>
            )}

            {!loading && result && !result.blocked && result.recipes.length > 0 && (
              <div className="space-y-3">
                <div className="flex items-center justify-between border-b pb-2">
                  <span className="font-mono text-xs uppercase tracking-wide text-muted-foreground">
                    Results
                  </span>
                  <div className="flex items-center gap-3">
                    <span className="font-mono text-xs text-muted-foreground">
                      {result.recipes.length} recipes &middot; target {result.active_target}
                    </span>
                    <WhyThisResultSheet result={result} />
                  </div>
                </div>
                <ResultsTable recipes={result.recipes} />
              </div>
            )}

            {!loading && !result && !error && (
              <div className="rounded border border-dashed p-6 text-sm text-muted-foreground">
                <p>No query run yet.</p>
                <p className="mt-1 text-xs">
                  Enter an age in months and press Search. Every other control narrows or
                  reorders the result; none of them is required.
                </p>
              </div>
            )}
          </section>
        </ResizablePanel>
      </ResizablePanelGroup>
    </div>
  );
}
