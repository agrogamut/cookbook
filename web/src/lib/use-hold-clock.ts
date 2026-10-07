"use client";

import { useEffect, useState } from "react";

export function useHoldClock(expiresAt: string | null | undefined) {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (!expiresAt) return;
    const timer = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(timer);
  }, [expiresAt]);
  const seconds = expiresAt
    ? Math.max(0, Math.ceil((new Date(expiresAt).getTime() - now) / 1000))
    : null;
  return {
    expired: seconds === 0,
    label:
      seconds === null
        ? ""
        : `${Math.floor(seconds / 60)}:${String(seconds % 60).padStart(2, "0")}`,
  };
}
