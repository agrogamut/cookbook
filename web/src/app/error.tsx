"use client";

import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";

export default function GlobalError({ error, reset }: { error: Error; reset: () => void }) {
  return (
    <Alert variant="destructive" className="m-6">
      <AlertTitle>Something failed</AlertTitle>
      <AlertDescription className="space-y-2">
        <p className="font-mono text-xs">{error.message}</p>
        <p>
          If this is a connection error, confirm the API server is running
          (`go run ./cmd/server`) and `NEXT_PUBLIC_API_URL` in `.env.local` points at it.
        </p>
        <Button size="sm" onClick={reset}>Retry</Button>
      </AlertDescription>
    </Alert>
  );
}
