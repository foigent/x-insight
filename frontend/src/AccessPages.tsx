import { getLocale, t, tx, useLocale, useMessageState } from "./i18n";
import { FormEvent, useEffect, useState } from "react";

import {
  accessAPI,
  ApiRequestError,
  isAbortError,
  type AccessSession,
  type AccessSessionResponse,
  type AccessProviderCapabilities,
} from "./api";

export function LoginPage({
  onAuthenticated,
}: {
  onAuthenticated: (session: AccessSessionResponse) => void;
}) {
  useLocale();
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useMessageState("");
  const [providers, setProviders] = useState<AccessProviderCapabilities>();
  const isHTTPS = window.location.protocol === "https:";

  useEffect(() => {
    const controller = new AbortController();
    accessAPI.providerCapabilities(controller.signal).then(setProviders).catch(() => undefined);
    return () => controller.abort();
  }, []);

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!isHTTPS) {
      setPassword("");
      setError(() => t("m0000"));
      return;
    }
    setSubmitting(true);
    setError(() => "");
    let credentialsAccepted = false;
    try {
      await accessAPI.login(username, password);
      credentialsAccepted = true;
      setPassword("");
      // Login establishes the cookie; session resolves current scope before any
      // business page is mounted. Do not adopt the unqualified login response.
      const session = await accessAPI.session();
      if (session.session.permissions.includes("investigation.scoped.read") && !session.scope_revision) {
        throw new Error("Scoped session revision is unavailable");
      }
      onAuthenticated(session);
    } catch (failure) {
      setPassword("");
      if (failure instanceof ApiRequestError && failure.status === 429) {
        setError(() => t("m0001"));
      } else if (failure instanceof ApiRequestError && failure.status === 401) {
        setError(() => credentialsAccepted ? t("m0002") : t("m0003"));
      } else {
        setError(() => credentialsAccepted ? t("m0004") : t("m0005"));
      }
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <main className="login-shell" id="main-content" tabIndex={-1}>
      <section className="login-card" aria-labelledby="login-title">
        <div className="login-brand" aria-hidden="true">XI</div>
        <p className="eyebrow">{tx("m0006")}</p>
        <h1 id="login-title">{tx("m0007")}</h1>
        <p className="login-boundary">{tx("m0008")}</p>
        <div className="login-provider-state">
          <span>{t("m0009")}<strong>{accessStateLabel(providers?.local_account_state)}</strong></span>
          <span>{t("m0010")}<strong>{accessStateLabel(providers?.oidc_runtime_state)}</strong></span>
          {providers ? <><small>{accessBoundaryLabel(providers)}</small><details><summary>{tx("m0011")}</summary><p>{tx("m0012")}</p><p>{providers.boundary}</p><p>{tx("m0013")}</p></details></> : null}
        </div>
        <form onSubmit={submit}>
          <label>
            {t("m0014")}<input
              autoComplete="username"
              disabled={!isHTTPS}
              name="username"
              required
              maxLength={128}
              value={username}
              onChange={(event) => setUsername(event.target.value)}
            />
          </label>
          <label>
            {t("m0015")}<input
              autoComplete="current-password"
              disabled={!isHTTPS}
              name="password"
              required
              maxLength={1024}
              type="password"
              value={password}
              onChange={(event) => setPassword(event.target.value)}
            />
          </label>
          {!isHTTPS ? (
            <p className="login-error" role="alert">{tx("m0016")}</p>
          ) : null}
          {error ? <p className="login-error" role="alert">{error}</p> : null}
          <button className="primary-button" disabled={submitting || !isHTTPS} type="submit">
            {submitting ? t("m0017") : t("m0018")}
          </button>
        </form>
        <p className="login-note">{tx("m0019")}</p>
      </section>
    </main>
  );
}

function accessStateLabel(state?: string): string {
  switch (state) {
    case "active":
      return t("m0020");
    case "not_configured":
      return t("m0021");
    default:
      return t("m0022");
  }
}

function accessBoundaryLabel(providers: AccessProviderCapabilities): string {
  if (providers.oidc_runtime_state === "not_configured") {
    return t("m0023");
  }
  return t("m0024");
}

export function AccessSessionsPage({
  current,
  onUnauthenticated,
}: {
  current: AccessSessionResponse;
  onUnauthenticated: () => void;
}) {
  useLocale();
  const [sessions, setSessions] = useState<AccessSession[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useMessageState("");
  const [revoking, setRevoking] = useState("");

  useEffect(() => {
    const controller = new AbortController();
    accessAPI.listSessions(controller.signal)
      .then((response) => setSessions(response.items))
      .catch((failure) => {
        if (isAbortError(failure)) {
          return;
        }
        if (failure instanceof ApiRequestError && failure.status === 403) {
          setError(() => t("m0025"));
        } else {
          setError(() => t("m0026"));
        }
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false);
      });
    return () => controller.abort();
  }, []);

  async function revoke(sessionRef: string) {
    setRevoking(sessionRef);
    setError(() => "");
    try {
      await accessAPI.revokeSession(sessionRef, current.csrf_token);
      setSessions((items) => items.filter((item) => item.session_ref !== sessionRef));
      if (sessionRef === current.session.session_ref) {
        onUnauthenticated();
      }
    } catch {
      setError(() => t("m0027"));
    } finally {
      setRevoking("");
    }
  }

  return (
    <>
      <header className="topbar">
        <div>
          <p className="eyebrow">Access · operator only</p>
          <h1>{tx("m0028")}</h1>
          <p className="page-intro">{tx("m0029")}</p>
        </div>
      </header>
      <section className="access-session-panel" aria-busy={loading}>
        {error ? <p className="feedback feedback-error" role="alert">{error}</p> : null}
        {loading ? <p>{tx("m0030")}</p> : null}
        {!loading && sessions.length === 0 ? <p>{tx("m0031")}</p> : null}
        <ul className="access-session-list" aria-label={t("m0032")}>
          {sessions.map((session) => (
            <li key={session.session_ref}>
              <div>
                <strong>{session.subject_id}</strong>
                <span>{session.role}</span>
                {session.session_ref === current.session.session_ref ? <em>{tx("m0033")}</em> : null}
              </div>
              <code>{session.session_ref}</code>
              <small>{tx("m0034", { value0: formatAccessTime(session.last_seen_at), value1: formatAccessTime(session.absolute_expires_at) })}</small>
              <button
                className="secondary-button"
                disabled={revoking === session.session_ref}
                onClick={() => void revoke(session.session_ref)}
                type="button"
              >
                {revoking === session.session_ref ? t("m0035") : t("m0036")}
              </button>
            </li>
          ))}
        </ul>
      </section>
    </>
  );
}

function formatAccessTime(value: string) {
  const parsed = new Date(value);
  if (Number.isNaN(parsed.getTime())) {
    return value;
  }
  return new Intl.DateTimeFormat(getLocale(), {
    dateStyle: "short",
    timeStyle: "medium",
    hour12: false,
  }).format(parsed);
}
