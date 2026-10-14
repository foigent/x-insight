import { describe, expect, it } from "vitest";
import { previewVerifiedWorkReviewBody } from "./workReviewBody";

describe("review-body text preview", () => {
  it("preserves valid Chinese text when 64 KiB ends inside a character", () => {
    const value = "中".repeat(30_000);
    const preview = previewVerifiedWorkReviewBody(new TextEncoder().encode(value));
    expect(preview).not.toBeNull();
    expect(preview?.length).toBeGreaterThan(0);
    expect(preview?.endsWith("中")).toBe(true);
  });

  it("keeps invalid UTF-8 as verified binary", () => {
    expect(previewVerifiedWorkReviewBody(new Uint8Array([0xff]))).toBeNull();
  });
});
