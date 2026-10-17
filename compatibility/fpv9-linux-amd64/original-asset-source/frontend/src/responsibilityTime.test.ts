import { describe, expect, it } from "vitest";
import { deadlineCandidates, preciseDeadlineValue } from "./responsibilityTime";

describe("responsibility deadline", () => {
  it("rejects a nonexistent local time and exposes both repeated instants", () => {
    expect(deadlineCandidates("2026-03-08T02:30", "America/New_York")).toEqual([]);
    const values = deadlineCandidates("2026-11-01T01:30", "America/New_York");
    expect(values.map((item) => item.value)).toEqual(["2026-11-01T01:30:00-04:00", "2026-11-01T01:30:00-05:00"]);
  });
  it("preserves stored seconds and microseconds when only responsibility changes", () => {
    expect(preciseDeadlineValue("2026-09-09T12:30:24.123456Z", "Asia/Shanghai")).toBe("2026-09-09T20:30:24.123456+08:00");
    expect(preciseDeadlineValue("2026-11-01T06:30:12Z", "America/New_York")).toBe("2026-11-01T01:30:12-05:00");
  });
  it("does not normalize an invalid calendar date", () => {
    expect(deadlineCandidates("2026-02-30T12:00", "Asia/Shanghai")).toEqual([]);
  });
});
