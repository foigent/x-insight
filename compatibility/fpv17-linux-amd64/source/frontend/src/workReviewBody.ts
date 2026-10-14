import { scheduleBodyExpiry } from "./bodyExpiry";
import { currentScopeSessionKey, scopeSessionKey, type AccessSessionResponse } from "./api";

const maximumBytes = 32 << 20;
const previewBytes = 64 << 10;
let activeRead: AbortController | undefined;

export type VerifiedWorkReviewBody = { bytes: Uint8Array; text: string | null; previewOnly: boolean; expiresAt: string };

export function previewVerifiedWorkReviewBody(bytes: Uint8Array): string | null {
  try { return new TextDecoder("utf-8", { fatal: true }).decode(bytes.subarray(0, previewBytes), { stream: bytes.byteLength > previewBytes }); }
  catch { return null; }
}

export async function fetchVerifiedWorkReviewBody(ref: string, digest: string, session: AccessSessionResponse, signal: AbortSignal): Promise<VerifiedWorkReviewBody> {
  if (activeRead) throw new Error("另一项正文读取仍在进行");
  if (!/^sha256:[a-f0-9]{64}$/.test(digest) || !session.scope_revision) throw new Error("正文引用或授权状态无效");
  const controller = new AbortController();
  activeRead = controller;
  const identity = scopeSessionKey(session);
  let expiresAt = "";
  let deadline = Infinity;
  let cancelExpiry: (() => void) | undefined;
  const cancel = () => controller.abort();
  signal.addEventListener("abort", cancel, { once: true });
  if (signal.aborted) cancel();
  window.addEventListener("xinsight:scope-changed", cancel);
  window.addEventListener("xinsight:unauthorized", cancel);
  const current = () => {
    if (Date.now() >= deadline) throw new Error("XI 复查副本已过期");
    if (controller.signal.aborted || currentScopeSessionKey() !== identity) throw new DOMException("Authorization changed", "AbortError");
  };
  try {
    current();
    const response = await fetch(`/api/v1/work-activity/contents/${encodeURIComponent(ref)}`, {
      method: "GET", credentials: "same-origin", cache: "no-store", redirect: "error", signal: controller.signal,
      headers: { Accept: "application/octet-stream", "X-XInsight-Scope-Revision": session.scope_revision },
    });
    current();
    if (response.status === 401) window.dispatchEvent(new Event("xinsight:unauthorized"));
    if (response.status === 409 || response.status === 200 && response.headers.get("X-XInsight-Scope-Revision") !== session.scope_revision) {
      window.dispatchEvent(new Event("xinsight:scope-changed"));
      throw new DOMException("Authorization changed", "AbortError");
    }
    if (response.status !== 200) throw new Error("当前正文副本不可读；可能已撤权、过期或副本不可用");
    expiresAt = response.headers.get("X-XInsight-Content-Expires-At") ?? "";
    deadline = Date.parse(expiresAt);
    if (!/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{6}Z$/.test(expiresAt) || !Number.isFinite(deadline) ||
      new Date(deadline).toISOString().slice(0, 19) !== expiresAt.slice(0, 19)) throw new Error("正文到期时间未通过核验");
    current();
    cancelExpiry = scheduleBodyExpiry(deadline, cancel);
    const lengthHeader = response.headers.get("Content-Length");
    const size = Number(lengthHeader);
    if (lengthHeader === null || !Number.isSafeInteger(size) || size < 0 || size > maximumBytes ||
      response.headers.get("Content-Type")?.split(";")[0] !== "application/octet-stream" ||
      response.headers.get("X-XInsight-Content-SHA256") !== digest ||
      ![null, "identity"].includes(response.headers.get("Content-Encoding"))) throw new Error("正文响应元数据未通过核验");
    const bytes = new Uint8Array(size);
    let offset = 0;
    if (!response.body && size !== 0) throw new Error("正文响应没有可读字节流");
    if (response.body) {
      const reader = response.body.getReader();
      while (true) {
        current();
        const chunk = await reader.read();
        if (chunk.done) break;
        if (offset + chunk.value.byteLength > size) throw new Error("正文响应超出声明长度");
        bytes.set(chunk.value, offset);
        offset += chunk.value.byteLength;
      }
    }
    current();
    if (offset !== size) throw new Error("正文响应未完整到达");
    const hash = await crypto.subtle.digest("SHA-256", bytes);
    current();
    const actual = "sha256:" + [...new Uint8Array(hash)].map(value => value.toString(16).padStart(2, "0")).join("");
    if (actual !== digest) throw new Error("正文摘要与来源引用不一致");
    const text = previewVerifiedWorkReviewBody(bytes);
    return { bytes, text, previewOnly: bytes.byteLength > previewBytes, expiresAt };
  } finally {
    controller.abort();
    cancelExpiry?.();
    signal.removeEventListener("abort", cancel);
    window.removeEventListener("xinsight:scope-changed", cancel);
    window.removeEventListener("xinsight:unauthorized", cancel);
    if (activeRead === controller) activeRead = undefined;
  }
}
