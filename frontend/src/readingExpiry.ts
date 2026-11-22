import { useEffect } from "react";

type ReadingTimes = { cutoff: string; expires_at: string };

export function readingDeadline(reading: ReadingTimes, sentAt: number): number {
  return sentAt + Date.parse(reading.expires_at) - Date.parse(reading.cutoff);
}

export function restoredReadingDeadline(reading: ReadingTimes): number {
  const now = performance.now();
  return Math.min(readingDeadline(reading, now), now + Date.parse(reading.expires_at) - Date.now());
}

export function useReadingExpiry(deadline: number | undefined, expire: () => void) {
  useEffect(() => {
    if (deadline === undefined) return;
    const timer = window.setTimeout(expire, Math.min(Math.max(0, deadline - performance.now()), 2_147_483_647));
    return () => window.clearTimeout(timer);
  }, [deadline, expire]);
}
