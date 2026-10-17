import { afterEach, describe, expect, it, vi } from "vitest";

import { accessAPI, ApiResponseError, investigationAPI } from "./api";

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("API response boundary", () => {
  it("rejects a successful response with malformed JSON", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => new Response("{", {
      status: 200,
      headers: { "Content-Type": "application/json" },
    })));

    await expect(investigationAPI.listSources()).rejects.toMatchObject({
      name: "ApiResponseError",
      path: "/api/v1/sources?limit=100",
    } satisfies Partial<ApiResponseError>);
  });

  it("rejects a successful response without a JSON object", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => new Response("[]", {
      status: 200,
      headers: { "Content-Type": "application/json" },
    })));

    await expect(investigationAPI.listSources()).rejects.toThrow("expected a JSON object body");
  });

  it("accepts the explicitly empty logout response", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => new Response(null, { status: 204 })));

    await expect(accessAPI.logout("csrf-token")).resolves.toBeUndefined();
  });
});
