// Browser timers have a signed 32-bit delay. Recheck the actual deadline after
// each bounded wait; a long XI retention period must not become an early expiry.
export function scheduleBodyExpiry(deadline: number, expire: () => void): () => void {
  let timer: number | undefined;
  const check = () => {
    const remaining = deadline - Date.now();
    if (!Number.isFinite(remaining) || remaining <= 0) expire();
    else timer = window.setTimeout(check, Math.min(remaining, 2147483647));
  };
  check();
  return () => { if (timer !== undefined) window.clearTimeout(timer); };
}
