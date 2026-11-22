import { ApiRequestError, isAbortError } from "./api";

export function isAccessDenied(error: unknown): boolean {
  return error instanceof ApiRequestError && [401, 403].includes(error.status);
}

export function canRetryRead(error: unknown): boolean {
  if (isAbortError(error) || isAccessDenied(error)) return false;
  if (!(error instanceof ApiRequestError)) return true;
  return error.status === 429 || (error.problem?.retryable ?? error.status >= 500);
}

export function readingUnavailable(error: unknown): boolean {
  if (!(error instanceof ApiRequestError)) return false;
  const code = error.problem?.code;
  return [401, 403, 404, 410].includes(error.status) ||
    code === "workbench_access_changed" || code === "scope_revision_changed" ||
    (error.status === 409 && !code);
}

// These paginated endpoints use the Workbench problem for a rejected cursor.
// A first-page 400 can instead mean invalid filters or an unsupported view.
export function rejectsPageCursor(error: unknown, cursor?: string): boolean {
  return !!cursor && error instanceof ApiRequestError && error.status === 400 &&
    (!error.problem || error.problem.code === "invalid_workbench_request");
}

export class ReadingCursorError extends Error {}

const readInvalidationEvents = ["xinsight:scope-pending", "xinsight:scope-changed", "xinsight:unauthorized"];

export function subscribeReadInvalidation(revoke: () => void): () => void {
  for (const event of readInvalidationEvents) window.addEventListener(event, revoke);
  return () => {
    for (const event of readInvalidationEvents) window.removeEventListener(event, revoke);
  };
}
