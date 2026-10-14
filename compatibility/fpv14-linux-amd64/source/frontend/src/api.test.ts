import { afterEach, describe, expect, it, vi } from "vitest";

import { accessAPI, ApiResponseError, investigationAPI, workbenchAPI } from "./api";

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

  it("uses the shared authenticated JSON and Problem transport for generic GET calls", async () => {
    const fetchMock = vi.fn(async () => new Response(JSON.stringify({
      type: "https://x-insight.local/problems/workbench-access-changed",
      title: "Workbench authorization changed",
      status: 409,
      code: "workbench_access_changed",
      correlation_id: `xc_${"a".repeat(32)}`,
      retryable: false,
    }), {
      status: 409,
      headers: { "Content-Type": "application/problem+json" },
    }));
    vi.stubGlobal("fetch", fetchMock);

    await expect(workbenchAPI.summary("xwr_AAAAAAAAAAAAAAAAAAAAAA")).rejects.toMatchObject({
      status: 409,
      problem: { code: "workbench_access_changed" },
    });
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/v1/workbench/readings/xwr_AAAAAAAAAAAAAAAAAAAAAA/summary",
      expect.objectContaining({ method: "GET", cache: "no-store", credentials: "same-origin" }),
    );
  });
  it("preserves legacy GET headers, abort signal, cache and global 401 handling", async () => {
    const controller = new AbortController();
    const unauthorized = vi.fn();
    window.addEventListener("xinsight:unauthorized", unauthorized);
    try {
      const fetchMock = vi.fn(async () => new Response(JSON.stringify({type:"urn:test",title:"Authentication required",status:401,code:"authentication_required",correlation_id:"xc_test",retryable:false}),{status:401,headers:{"Content-Type":"application/problem+json"}}));
      vi.stubGlobal("fetch",fetchMock);
      await expect(investigationAPI.listSources(controller.signal)).rejects.toMatchObject({status:401,problem:{code:"authentication_required"}});
      expect(fetchMock).toHaveBeenCalledWith("/api/v1/sources?limit=100",expect.objectContaining({method:"GET",signal:controller.signal,cache:"no-store",credentials:"same-origin",headers:{Accept:"application/json, application/problem+json"}}));
      expect(unauthorized).toHaveBeenCalledTimes(1);
      const aborted=new DOMException("Aborted","AbortError");
      fetchMock.mockRejectedValueOnce(aborted);
      await expect(investigationAPI.listSources(controller.signal)).rejects.toBe(aborted);
    } finally {window.removeEventListener("xinsight:unauthorized",unauthorized)}
  });

});


it("keeps amount series as the default and explicitly requests economic request buckets", async () => {
  const fetch = vi.fn(async () => new Response("{}", {status:200,headers:{"Content-Type":"application/json"}}));
  vi.stubGlobal("fetch",fetch);
  await workbenchAPI.series("reading-a");
  await workbenchAPI.series("reading-a",undefined,7200,"requests");
  expect(fetch.mock.calls.map(call => String((call as unknown as [string])[0]))).toEqual([
    "/api/v1/workbench/readings/reading-a/series?metric=amount&bucket_seconds=3600",
    "/api/v1/workbench/readings/reading-a/series?metric=requests&bucket_seconds=7200",
  ]);
});
