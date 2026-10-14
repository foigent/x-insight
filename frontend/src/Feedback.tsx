import { t, tx, useLocale } from "./i18n";
import { ApiRequestError } from "./api";

export function LoadingBlock({ label = t("m0680") }: { label?: string }) {
  useLocale();
  return (
    <div className="loading-block" role="status" aria-live="polite">
      <span className="loading-spinner" aria-hidden="true" />
      <div>
        <strong>{label}</strong>
        <p>{tx("m0681")}</p>
      </div>
    </div>
  );
}

export function ErrorState({
  error,
  onRetry,
  compact = false,
}: {
  error: unknown;
  onRetry?: () => void;
  compact?: boolean;
}) {
  useLocale();
  const message = errorMessage(error);
  const problem = error instanceof ApiRequestError ? error.problem : undefined;
  return (
    <div className={`error-state${compact ? " error-state-compact" : ""}`} role="alert">
      <span className="error-mark" aria-hidden="true">!</span>
      <div>
        <strong>{message.title}</strong>
        <p>{message.description}</p>
        {problem?.correlation_id && (
          <p className="correlation-ref">
            {t("m0682")}<code>{problem.correlation_id}</code>
          </p>
        )}
        {onRetry && (
          <button type="button" className="button button-secondary" onClick={onRetry}>{tx("m0255")}</button>
        )}
      </div>
    </div>
  );
}

export function EmptyState({ title, description }: { title: string; description: string }) {
  useLocale();
  return (
    <div className="empty-state">
      <span aria-hidden="true">∅</span>
      <strong>{title}</strong>
      <p>{description}</p>
    </div>
  );
}

function errorMessage(error: unknown): { title: string; description: string } {
  if (error instanceof DOMException && error.name === "AbortError") {
    return { title: t("m0363"), description: t("m0683") };
  }
  if (!(error instanceof ApiRequestError)) {
    return { title: t("m0684"), description: t("m0685") };
  }
  switch (error.problem?.code) {
    case "execution_not_found":
      return { title: t("m0686"), description: t("m0687") };
    case "event_not_found":
      return { title: t("m0688"), description: t("m0689") };
    case "cursor_expired":
      return { title: t("m0690"), description: t("m0691") };
    case "cursor_mismatch":
    case "invalid_cursor":
      return { title: t("m0692"), description: t("m0693") };
    case "request_rate_limited":
      return { title: t("m0694"), description: t("m0695") };
    case "temporarily_overloaded":
      return { title: t("m0696"), description: t("m0697") };
    case "database_unavailable":
      return { title: t("m0698"), description: t("m0699") };
    case "invalid_argument":
      return { title: t("m0700"), description: t("m0701") };
    default:
      if (error.status === 401 || error.status === 403) {
        return { title: t("m0702"), description: t("m0703") };
      }
      return { title: t("m0704"), description: t("m0705") };
  }
}
