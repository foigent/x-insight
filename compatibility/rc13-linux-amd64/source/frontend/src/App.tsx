import { MouseEvent, useCallback, useEffect, useState } from "react";

import { AccessSessionsPage, LoginPage } from "./AccessPages";
import { accessAPI, ApiRequestError, isAbortError, type AccessSessionResponse } from "./api";
import { ExecutionDetailPage } from "./ExecutionDetailPage";
import { ExecutionSearchPage } from "./ExecutionSearchPage";
import {
  CaseDetailPage,
  CasesPage,
  ComparePage,
  ConnectorsPage,
  DiscoveryPage,
  ServiceIdentitiesPage,
  SessionDetailPage,
  SessionsPage,
} from "./ProductPages";
import { PosturePage } from "./PosturePage";

type Route =
  | { kind: "login" }
  | { kind: "inbox" }
  | { kind: "search" }
  | { kind: "detail"; executionID: string; asOf?: string }
  | { kind: "cases" }
  | { kind: "case-detail"; caseID: string }
  | { kind: "sessions" }
  | { kind: "session-detail"; sessionID: string }
  | { kind: "compare" }
  | { kind: "connectors" }
  | { kind: "discovery" }
  | { kind: "access-sessions" }
  | { kind: "service-identities" };

export function App() {
  const [route, setRoute] = useState<Route>(() => readRoute());
  const [session, setSession] = useState<AccessSessionResponse | null>(null);
  const [accessState, setAccessState] = useState<"loading" | "authenticated" | "unauthenticated" | "error">("loading");
  const [logoutError, setLogoutError] = useState("");
  const [mobileNavOpen, setMobileNavOpen] = useState(false);

  const unauthenticate = useCallback(() => {
    setSession(null);
    setAccessState("unauthenticated");
    setLogoutError("");
    window.history.replaceState(null, "", "/login");
    setRoute({ kind: "login" });
  }, []);

  useEffect(() => {
    const onPopState = () => {
      setRoute(readRoute());
      setMobileNavOpen(false);
    };
    window.addEventListener("popstate", onPopState);
    return () => window.removeEventListener("popstate", onPopState);
  }, []);

  useEffect(() => {
    const controller = new AbortController();
    accessAPI.session(controller.signal)
      .then((current) => {
        setSession(current);
        setAccessState("authenticated");
        if (window.location.pathname.replace(/\/+$/, "") === "/login") {
          window.history.replaceState(null, "", "/inbox");
          setRoute(readRoute());
        }
      })
      .catch((failure) => {
        if (isAbortError(failure)) {
          return;
        }
        setSession(null);
        if (failure instanceof ApiRequestError && failure.status === 401) {
          unauthenticate();
          return;
        }
        setAccessState("error");
      });
    return () => controller.abort();
  }, [unauthenticate]);

  useEffect(() => {
    const onUnauthorized = () => unauthenticate();
    window.addEventListener("xinsight:unauthorized", onUnauthorized);
    return () => window.removeEventListener("xinsight:unauthorized", onUnauthorized);
  }, [unauthenticate]);

  const navigate = useCallback((path: string) => {
    window.history.pushState(null, "", path);
    setRoute(readRoute());
    setMobileNavOpen(false);
    window.scrollTo?.({ top: 0, behavior: "auto" });
  }, []);

  function navigateFromLink(event: MouseEvent<HTMLAnchorElement>, path: string) {
    if (event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) {
      return;
    }
    event.preventDefault();
    navigate(path);
  }

  function authenticated(current: AccessSessionResponse) {
    setSession(current);
    setAccessState("authenticated");
    window.history.replaceState(null, "", "/inbox");
    setRoute({ kind: "inbox" });
  }

  async function logout() {
    if (!session) {
      return;
    }
    setLogoutError("");
    try {
      await accessAPI.logout(session.csrf_token);
      unauthenticate();
    } catch (failure) {
      if (failure instanceof ApiRequestError && failure.status === 401) {
        unauthenticate();
        return;
      }
      setLogoutError("退出失败；当前 Session 仍保持登录。");
    }
  }

  if (accessState === "loading") {
    return (
      <main className="access-state-shell" aria-live="polite">
        <span className="status-dot" aria-hidden="true" />
        <p>正在验证产品 Session…</p>
      </main>
    );
  }

  if (accessState === "error") {
    return (
      <main className="access-state-shell" id="main-content" tabIndex={-1}>
        <h1>暂时无法验证 Session</h1>
        <p>没有进入证据调查页面。请确认当前产品服务可用后刷新。</p>
      </main>
    );
  }

  if (accessState === "unauthenticated" || session === null || route.kind === "login") {
    return <LoginPage onAuthenticated={authenticated} />;
  }

  return (
    <div className="app-shell">
      <a className="skip-link" href="#main-content">跳到主要内容</a>
      <aside className={`sidebar${mobileNavOpen ? " sidebar-nav-open" : ""}`} aria-label="主导航">
        <div className="sidebar-header">
          <a className="brand" href="/inbox" aria-label="X-Insight 首页" onClick={(event) => navigateFromLink(event, "/inbox")}>
            <span className="brand-mark" aria-hidden="true">XI</span>
            <span>
              <strong>X-Insight</strong>
              <small>Evidence intelligence</small>
            </span>
          </a>
          <button
            className="mobile-nav-toggle"
            type="button"
            aria-expanded={mobileNavOpen}
            aria-controls="product-navigation"
            onClick={() => setMobileNavOpen((open) => !open)}
          >
            <span aria-hidden="true">{mobileNavOpen ? "×" : "☰"}</span>
            <span>{mobileNavOpen ? "关闭" : "菜单"}</span>
          </button>
        </div>

        <nav className="nav-list" id="product-navigation" aria-label="产品功能">
          <NavLink active={route.kind === "inbox"} href="/inbox" icon="posture" label="运行态势" detail="Posture · 风险与证据" onClick={navigateFromLink} />
          <span className="nav-section-label">调查</span>
          <a
            className={`nav-item${route.kind === "search" || route.kind === "detail" ? " nav-item-active" : ""}`}
            href="/executions"
            aria-current={route.kind === "search" || route.kind === "detail" ? "page" : undefined}
            onClick={(event) => navigateFromLink(event, "/executions")}
          >
            <NavIcon name="executions" />
            <span>
              执行记录
              <small>Executions · 路径与证据</small>
            </span>
          </a>
          <NavLink active={route.kind === "cases" || route.kind === "case-detail"} href="/cases" icon="cases" label="调查案件" detail="Cases · 结论与证据包" onClick={navigateFromLink} />
          <span className="nav-section-label">分析</span>
          <NavLink active={route.kind === "sessions" || route.kind === "session-detail"} href="/sessions" icon="sessions" label="会话" detail="Sessions · 来源声明" onClick={navigateFromLink} />
          <NavLink active={route.kind === "compare"} href="/compare" icon="compare" label="执行对比" detail="Compare · 缺失不等于零" onClick={navigateFromLink} />
          <NavLink active={route.kind === "discovery"} href="/discovery" icon="discovery" label="调查发现" detail="Discovery · 假设与弃权" onClick={navigateFromLink} />
          <span className="nav-section-label">信任与访问</span>
          <NavLink active={route.kind === "connectors"} href="/connectors" icon="connectors" label="连接器" detail="Connectors · 覆盖与交付" onClick={navigateFromLink} />
          {session.session.permissions.includes("access.sessions.read") ? (
            <a
              className={`nav-item${route.kind === "access-sessions" ? " nav-item-active" : ""}`}
              href="/access/sessions"
              aria-current={route.kind === "access-sessions" ? "page" : undefined}
              onClick={(event) => navigateFromLink(event, "/access/sessions")}
            >
              <NavIcon name="access" />
              <span>
                访问会话
                <small>Access Sessions · 查看与吊销</small>
              </span>
            </a>
          ) : null}
          {session.session.permissions.includes("service_identities.read") ? (
            <NavLink active={route.kind === "service-identities"} href="/access/service-identities" icon="agent" label="Agent 身份" detail="Agent Access · 只读身份" onClick={navigateFromLink} />
          ) : null}
          <span className="nav-section-label">帮助</span>
          <a className="nav-item" href="/manual/zh-CN/">
            <NavIcon name="manual" />
            <span>
              产品手册
              <small>Manual · 与版本匹配</small>
            </span>
          </a>
        </nav>

        <div className="session-card" aria-label="当前产品 Session">
          <span className="eyebrow">Signed in</span>
          <strong>{session.session.subject_id}</strong>
          <small>{session.session.role}</small>
          <button type="button" onClick={() => void logout()}>退出登录</button>
          {logoutError ? <p role="alert">{logoutError}</p> : null}
        </div>

      </aside>

      <main id="main-content" tabIndex={-1} className={`workspace${route.kind === "detail" ? " workspace-detail" : ""}`}>
        <div className="context-bar">
          <span><span className="status-dot" aria-hidden="true" /> 仅元数据 · 不展示原文</span>
          <span>单部署空间</span>
          <span>来源 / 导入 / 投影 / 因果</span>
          <span className="context-role">{session.session.role}</span>
        </div>
        {route.kind === "inbox" ? (
          <PosturePage session={session} navigate={navigate} />
        ) : route.kind === "search" ? (
          <ExecutionSearchPage navigate={navigate} session={session} />
        ) : route.kind === "detail" ? (
          <ExecutionDetailPage
            key={`${route.executionID}:${route.asOf ?? "latest"}`}
            executionID={route.executionID}
            asOf={route.asOf}
            navigate={navigate}
            session={session}
          />
        ) : route.kind === "cases" ? (
          <CasesPage session={session} navigate={navigate} />
        ) : route.kind === "case-detail" ? (
          <CaseDetailPage caseID={route.caseID} session={session} navigate={navigate} />
        ) : route.kind === "sessions" ? (
          <SessionsPage session={session} navigate={navigate} />
        ) : route.kind === "session-detail" ? (
          <SessionDetailPage sessionID={route.sessionID} session={session} navigate={navigate} />
        ) : route.kind === "compare" ? (
          <ComparePage session={session} navigate={navigate} />
        ) : route.kind === "connectors" ? (
          <ConnectorsPage session={session} navigate={navigate} />
        ) : route.kind === "discovery" ? (
          <DiscoveryPage session={session} navigate={navigate} />
        ) : route.kind === "service-identities" ? (
          <ServiceIdentitiesPage session={session} navigate={navigate} />
        ) : route.kind === "access-sessions" && session.session.permissions.includes("access.sessions.read") ? (
          <AccessSessionsPage current={session} onUnauthenticated={unauthenticate} />
        ) : (
          <section className="access-session-panel">
            <h1>没有 Session 管理权限</h1>
            <p>当前角色未授予 <code>access.sessions.read</code>；Evidence 调查权限不受影响。</p>
          </section>
        )}
        <footer className="product-footer">
          <span>X-Insight</span>
          <span>Evidence-cited · bounded views · no raw</span>
        </footer>
      </main>
    </div>
  );
}

function readRoute(): Route {
  const path = window.location.pathname.replace(/\/+$/, "") || "/";
  if (path === "/login") {
    return { kind: "login" };
  }
  if (path === "/access/sessions") {
    return { kind: "access-sessions" };
  }
  if (path === "/access/service-identities") {
    return { kind: "service-identities" };
  }
  if (path === "/" || path === "/inbox") {
    return { kind: "inbox" };
  }
  if (path === "/executions") {
    return { kind: "search" };
  }
  if (path === "/cases") {
    return { kind: "cases" };
  }
  if (path === "/sessions") {
    return { kind: "sessions" };
  }
  if (path === "/compare") {
    return { kind: "compare" };
  }
  if (path === "/connectors") {
    return { kind: "connectors" };
  }
  if (path === "/discovery") {
    return { kind: "discovery" };
  }
  const caseMatch = path.match(/^\/cases\/([^/]+)$/);
  if (caseMatch) {
    return { kind: "case-detail", caseID: decodeSegment(caseMatch[1]) };
  }
  const sessionMatch = path.match(/^\/sessions\/([^/]+)$/);
  if (sessionMatch) {
    return { kind: "session-detail", sessionID: decodeSegment(sessionMatch[1]) };
  }
  const match = path.match(/^\/executions\/([^/]+)$/);
  if (!match) {
    return { kind: "inbox" };
  }
  const executionID = decodeSegment(match[1]);
  const asOf = new URLSearchParams(window.location.search).get("as_of") || undefined;
  return { kind: "detail", executionID, asOf };
}

function decodeSegment(segment: string) {
  try {
    return decodeURIComponent(segment);
  } catch {
    return segment;
  }
}

function NavLink({
  active,
  href,
  icon,
  label,
  detail,
  onClick,
}: {
  active: boolean;
  href: string;
  icon: NavIconName;
  label: string;
  detail: string;
  onClick: (event: MouseEvent<HTMLAnchorElement>, path: string) => void;
}) {
  return (
    <a
      className={`nav-item${active ? " nav-item-active" : ""}`}
      href={href}
      aria-current={active ? "page" : undefined}
      onClick={(event) => onClick(event, href)}
    >
      <NavIcon name={icon} />
      <span>{label}<small>{detail}</small></span>
    </a>
  );
}

type NavIconName = "posture" | "executions" | "cases" | "sessions" | "compare" | "discovery" | "connectors" | "access" | "agent" | "manual";

function NavIcon({ name }: { name: NavIconName }) {
  return (
    <span className="nav-symbol" aria-hidden="true">
      <svg viewBox="0 0 20 20" focusable="false">
        {name === "posture" ? <><path d="M3.5 13.5a6.5 6.5 0 1 1 13 0" /><path d="m10 10 3-3" /><circle cx="10" cy="10" r="1" /></> : null}
        {name === "executions" ? <><circle cx="8.5" cy="8.5" r="4.5" /><path d="m12 12 4 4" /><path d="M6.5 8.5h4M8.5 6.5v4" /></> : null}
        {name === "cases" ? <><rect x="4" y="3" width="12" height="14" rx="2" /><path d="M7 7h6M7 10h6M7 13h4" /></> : null}
        {name === "sessions" ? <><path d="M3.5 5.5A2.5 2.5 0 0 1 6 3h5a2.5 2.5 0 0 1 2.5 2.5v2A2.5 2.5 0 0 1 11 10H7l-2.5 2v-2.8a2.5 2.5 0 0 1-1-2z" /><path d="M9 13h4l2.5 2v-3a2.5 2.5 0 0 0 1-2V8" /></> : null}
        {name === "compare" ? <><path d="M3 6h11M11 3l3 3-3 3M17 14H6M9 11l-3 3 3 3" /></> : null}
        {name === "discovery" ? <><circle cx="10" cy="4" r="2" /><circle cx="5" cy="14" r="2" /><circle cx="15" cy="14" r="2" /><path d="m9 6-3 6M11 6l3 6M7 14h6" /></> : null}
        {name === "connectors" ? <><path d="m8 12-1.5 1.5a3 3 0 0 1-4-4L5 7a3 3 0 0 1 4 0" /><path d="m12 8 1.5-1.5a3 3 0 0 1 4 4L15 13a3 3 0 0 1-4 0" /><path d="m7 10 6 0" /></> : null}
        {name === "access" ? <><circle cx="7" cy="10" r="3" /><path d="M10 10h7M14 10v3M16 10v2" /></> : null}
        {name === "agent" ? <><rect x="4" y="5" width="12" height="10" rx="3" /><path d="M10 2v3M7.5 10h.01M12.5 10h.01M8 13h4" /></> : null}
        {name === "manual" ? <><path d="M3.5 4.5A2.5 2.5 0 0 1 6 3h4v13H6a2.5 2.5 0 0 0-2.5 1z" /><path d="M16.5 4.5A2.5 2.5 0 0 0 14 3h-4v13h4a2.5 2.5 0 0 1 2.5 1z" /></> : null}
      </svg>
    </span>
  );
}
