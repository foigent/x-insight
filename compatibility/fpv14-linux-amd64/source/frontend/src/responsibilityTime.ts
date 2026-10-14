function partsAt(epoch: number, formatter: Intl.DateTimeFormat): number[] {
  const parts = formatter.formatToParts(new Date(epoch));
  return ["year", "month", "day", "hour", "minute"].map((name) => Number(parts.find((part) => part.type === name)?.value));
}

function formatterFor(zone: string) {
  return new Intl.DateTimeFormat("en-CA", { timeZone: zone, year: "numeric", month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit", hourCycle: "h23" });
}

function localStamp(parts: number[]) {
  const [year, month, day, hour, minute] = parts;
  const two = (value: number) => String(value).padStart(2, "0");
  return `${String(year).padStart(4, "0")}-${two(month)}-${two(day)}T${two(hour)}:${two(minute)}`;
}

export function localDeadline(instant: string, zone: string): string {
  return localStamp(partsAt(Date.parse(instant), formatterFor(zone)));
}

export function preciseDeadlineValue(instant: string, zone: string): string {
  const local = localDeadline(instant, zone);
  const offset = Date.parse(`${local}:00Z`) - Math.floor(Date.parse(instant) / 60_000) * 60_000;
  const minutes = Math.abs(offset / 60_000);
  const seconds = instant.match(/:(\d{2}(?:\.\d+)?)(?:Z|[+-]\d{2}:\d{2})$/)?.[1];
  if (!seconds) throw new Error("Invalid stored deadline");
  return `${local}:${seconds}${offset >= 0 ? "+" : "-"}${String(Math.floor(minutes / 60)).padStart(2, "0")}:${String(minutes % 60).padStart(2, "0")}`;
}

// Resolve the selected wall time using actual timezone offsets around that date.
// A gap yields no candidates; a repeated time requires the user to choose one.
export function deadlineCandidates(local: string, zone: string): { value: string; label: string }[] {
  if (!/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}$/.test(local)) return [];
  const naive = Date.parse(`${local}:00Z`);
  if (!Number.isFinite(naive) || new Date(naive).toISOString().slice(0, 16) !== local) return [];
  const formatter = formatterFor(zone);
  const offsets = new Set<number>();
  for (const hours of [-36, -24, -12, 0, 12, 24, 36]) {
    const probe = naive + hours * 3_600_000;
    const stamp = localStamp(partsAt(probe, formatter));
    offsets.add(Date.parse(`${stamp}:00Z`) - probe);
  }
  const results: { value: string; label: string }[] = [];
  for (const offset of offsets) {
    const instant = naive - offset;
    if (localStamp(partsAt(instant, formatter)) !== local) continue;
    const minutes = Math.abs(offset / 60_000);
    const suffix = `${offset >= 0 ? "+" : "-"}${String(Math.floor(minutes / 60)).padStart(2, "0")}:${String(minutes % 60).padStart(2, "0")}`;
    results.push({ value: `${local}:00${suffix}`, label: `UTC${suffix} · ${new Date(instant).toISOString()}` });
  }
  return results.sort((a, b) => Date.parse(a.value) - Date.parse(b.value));
}
