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
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState("");
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
      setError("非 HTTPS 入口已禁用登录。请改用已批准的 HTTPS 产品入口。");
      return;
    }
    setSubmitting(true);
    setError("");
    try {
      const session = await accessAPI.login(username, password);
      setPassword("");
      onAuthenticated(session);
    } catch (failure) {
      setPassword("");
      if (failure instanceof ApiRequestError && failure.status === 429) {
        setError("登录尝试过多，请稍后再试。");
      } else if (failure instanceof ApiRequestError && failure.status === 401) {
        setError("用户名或密码无效。");
      } else {
        setError("暂时无法登录，请确认当前访问使用已批准的 HTTPS 产品入口。");
      }
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <main className="login-shell" id="main-content" tabIndex={-1}>
      <section className="login-card" aria-labelledby="login-title">
        <div className="login-brand" aria-hidden="true">XI</div>
        <p className="eyebrow">X-Insight operator access</p>
        <h1 id="login-title">登录证据调查平面</h1>
        <p className="login-boundary">
          本地账号 · 产品 Session · metadata-only
        </p>
        <div className="login-provider-state">
          <span>Local accounts <strong>{accessStateLabel(providers?.local_account_state)}</strong></span>
          <span>Enterprise OIDC <strong>{accessStateLabel(providers?.oidc_runtime_state)}</strong></span>
          {providers ? <small>{accessBoundaryLabel(providers)}</small> : null}
        </div>
        <form onSubmit={submit}>
          <label>
            用户名
            <input
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
            密码
            <input
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
            <p className="login-error" role="alert">
              非 HTTPS 入口已禁用登录；此页面不会提交账号或密码。
            </p>
          ) : null}
          {error ? <p className="login-error" role="alert">{error}</p> : null}
          <button className="primary-button" disabled={submitting || !isHTTPS} type="submit">
            {submitting ? "正在验证…" : "登录"}
          </button>
        </form>
        <p className="login-note">
          Session 仅保存在当前服务进程；服务重启后需要重新登录。
        </p>
      </section>
    </main>
  );
}

function accessStateLabel(state?: string): string {
  switch (state) {
    case "active":
      return "可用";
    case "not_configured":
      return "未配置";
    default:
      return "检查中";
  }
}

function accessBoundaryLabel(providers: AccessProviderCapabilities): string {
  if (providers.oidc_runtime_state === "not_configured") {
    return "仅保留供应商中立配置接口；当前未连接任何企业 OIDC。";
  }
  return providers.boundary;
}

export function AccessSessionsPage({
  current,
  onUnauthenticated,
}: {
  current: AccessSessionResponse;
  onUnauthenticated: () => void;
}) {
  const [sessions, setSessions] = useState<AccessSession[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
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
          setError("当前角色没有 Session 管理权限。");
        } else {
          setError("暂时无法读取 Session 列表。");
        }
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false);
      });
    return () => controller.abort();
  }, []);

  async function revoke(sessionRef: string) {
    setRevoking(sessionRef);
    setError("");
    try {
      await accessAPI.revokeSession(sessionRef, current.csrf_token);
      setSessions((items) => items.filter((item) => item.session_ref !== sessionRef));
      if (sessionRef === current.session.session_ref) {
        onUnauthenticated();
      }
    } catch {
      setError("Session 撤销失败；列表未被本地假定更新。");
    } finally {
      setRevoking("");
    }
  }

  return (
    <>
      <header className="topbar">
        <div>
          <p className="eyebrow">Access · operator only</p>
          <h1>Session 管理</h1>
          <p className="page-intro">仅显示非秘密 Session 元数据；页面不会显示 Cookie 或 CSRF token。</p>
        </div>
      </header>
      <section className="access-session-panel" aria-busy={loading}>
        {error ? <p className="feedback feedback-error" role="alert">{error}</p> : null}
        {loading ? <p>正在读取 Sessions…</p> : null}
        {!loading && sessions.length === 0 ? <p>当前没有可显示的 Session。</p> : null}
        <ul className="access-session-list" aria-label="活动 Sessions">
          {sessions.map((session) => (
            <li key={session.session_ref}>
              <div>
                <strong>{session.subject_id}</strong>
                <span>{session.role}</span>
                {session.session_ref === current.session.session_ref ? <em>当前 Session</em> : null}
              </div>
              <code>{session.session_ref}</code>
              <small>最后活动 {formatAccessTime(session.last_seen_at)} · 绝对到期 {formatAccessTime(session.absolute_expires_at)}</small>
              <button
                className="secondary-button"
                disabled={revoking === session.session_ref}
                onClick={() => void revoke(session.session_ref)}
                type="button"
              >
                {revoking === session.session_ref ? "正在撤销…" : "撤销 Session"}
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
  return new Intl.DateTimeFormat("zh-CN", {
    dateStyle: "short",
    timeStyle: "medium",
    hour12: false,
  }).format(parsed);
}
