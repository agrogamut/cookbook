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
    <div>
      <PageHeader
        title="Engine console"
        description="Enter a child profile and search. Age is the only required field."
      />
      <div className="grid grid-cols-1 gap-5 lg:grid-cols-[300px_1fr]">
        <aside>
          <ProfileForm onSubmit={handleSearch} loading={loading} />
        </aside>

        <section>
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
            <div className="rounded border border-dashed p-6 text-center text-sm text-muted-foreground">
              No query run yet.
            </div>
          )}
        </section>
      </div>
    </div>
  );
}
