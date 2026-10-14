import { readWorkbenchNavigation, readRuntimeNavigation, navigationOwner, type RuntimeNavigation, type WorkbenchNavigation } from "./workbenchNavigation";
import { BudgetPage } from "./BudgetPage";
import { MouseEvent, useCallback, useEffect, useRef, useState } from "react";

import { AccessSessionsPage, LoginPage } from "./AccessPages";
import { acceptScopeSession, accessAPI, ApiRequestError, isAbortError, scopeSessionKey, type AccessSessionResponse, type WorkbenchReadingRequest } from "./api";
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
import { OverviewPage, type OverviewContext } from "./OverviewPage";
import { InvestigationQueuePage } from "./InvestigationQueuePage";
import { WorkbenchPage, type WorkbenchSnapshot } from "./WorkbenchPage";
import { RuntimePage, type RuntimeSnapshot } from "./RuntimePage";
import { WorkActivityPage } from "./WorkActivityPage";
import { ReviewPolicyPage } from "./ReviewPolicyPage";

type Route =
  | { kind: "login" }
  | { kind: "inbox" }
  | { kind: "search" }
  | { kind: "detail"; executionID: string }
  | { kind: "cases" }
  | { kind: "investigation-queue" }
  | { kind: "case-detail"; caseID: string }
  | { kind: "sessions" }
  | { kind: "session-detail"; sessionID: string }
  | { kind: "compare" }
  | { kind: "connectors" }
  | { kind: "discovery" }
  | { kind: "access-sessions" }
  | { kind: "service-identities" }
  | { kind: "workbench" }
  | { kind: "runtime" }
  | { kind: "work-activity" }
  | { kind: "review-policy" };

export function App() {
  const [route, setRoute] = useState<Route>(() => readRoute());
  const [session, setSession] = useState<AccessSessionResponse | null>(null);
  const scopeKey = scopeSessionKey(session);
  const scopedRead = session?.session.permissions.includes("investigation.scoped.read") ?? false;
  const [accessState, setAccessState] = useState<"loading" | "authenticated" | "unauthenticated" | "error" | "scope-denied">("loading");
  const deniedScope=useRef<string|null>(null);
  const [logoutError, setLogoutError] = useState("");
  const [mobileNavOpen, setMobileNavOpen] = useState(false);
  const runtimeSnapshot=useRef<RuntimeSnapshot|null>(null);
  const keepRuntime=useCallback((value:RuntimeSnapshot)=>{
    runtimeSnapshot.current=value;
    if(!session||!value.reading||!value.applied)return;
    const reading:RuntimeNavigation={version:1,owner:navigationOwner(session),readingID:value.reading.reading_id,
      request:{contract_version:"xinsight.runtime-reading.v1",metric_version:"xinsight.runtime.request-observations.v1",
        period:{start_inclusive:value.reading.period.start_inclusive,end_exclusive:value.reading.period.end_exclusive,display_timezone:value.reading.period.timezone},
        filters:{source_ids:[...value.applied.filters.source_ids],source_projects:[...value.applied.filters.source_projects],cost_projects:[...value.applied.filters.cost_projects]}},
      entry:value.view.entry,selectedRef:value.view.selected?.request_ref,detailView:value.view.selected?.observation_count===0?"contents":value.view.detailView??"story"};
    window.history.replaceState({...window.history.state,runtimeReading:reading},"",window.location.href);
  },[session]);
  const workbenchSnapshot=useRef<WorkbenchSnapshot|null>(null);
  const workbenchTransitionConfig=useRef<WorkbenchReadingRequest|null>(null);
  const keepWorkbench=useCallback((value:WorkbenchSnapshot|null)=>{workbenchSnapshot.current=value},[]);
  const keepWorkbenchNavigation=useCallback((value:WorkbenchNavigation)=>{
    const old=window.history.state?.workbenchReading;
    const related=old?.readingID===value.readingID&&old?.owner===value.owner?old.related:undefined;
    window.history.replaceState({...window.history.state, workbenchReading:{...value, ...(related?{related}:{})}}, "", window.location.href);
  },[]);
  const keepOverview=useCallback((value:OverviewContext)=>{
    window.history.replaceState({...window.history.state, overviewContext:value, overviewIdentity:scopeKey}, "", window.location.href);
  },[scopeKey]);

  const unauthenticate = useCallback(() => {
    runtimeSnapshot.current=null;
    workbenchSnapshot.current=null;
    workbenchTransitionConfig.current=null;
    acceptScopeSession(null);
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
        acceptScopeSession(current);
        setSession(current);
        setAccessState("authenticated");
        const currentRoute = readRoute();
        if (window.location.pathname.replace(/\/+$/, "") === "/login" || !routeAllowed(currentRoute, current)) {
          window.history.replaceState(null, "", homePath(current));
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

  useEffect(()=>{
    if(!scopedRead)return;
    let alive=true;let revisionRequest=0;const controller=new AbortController();
    const refresh=(event?:Event)=>{
      const invalidated=event?.type==="xinsight:scope-changed";
      if(document.visibilityState==="hidden"&&!invalidated)return;
      const request=++revisionRequest;
      // Ordinary focus/pageshow checks preserve mounted drafts. A reported denial
      // closes the old content immediately while its authorization is rechecked.
      if(invalidated){deniedScope.current=scopeKey;workbenchSnapshot.current=null;workbenchTransitionConfig.current=null;runtimeSnapshot.current=null;setAccessState("loading");}
      void accessAPI.session(controller.signal).then(current=>{
        if(!alive||request!==revisionRequest)return;
        if(scopeSessionKey(current)!==scopeKey){workbenchSnapshot.current=null;workbenchTransitionConfig.current=null;}
        const unchangedDenial=deniedScope.current===scopeSessionKey(current);
        if(!unchangedDenial)deniedScope.current=null;
        acceptScopeSession(current);setSession(current);setAccessState(unchangedDenial?"scope-denied":"authenticated");
        if(!routeAllowed(readRoute(),current)){window.history.replaceState(null,"",homePath(current));setRoute(readRoute());}
      }).catch(error=>{if(!alive||request!==revisionRequest||isAbortError(error))return;workbenchSnapshot.current=null;workbenchTransitionConfig.current=null;if(error instanceof ApiRequestError&&error.status===401)unauthenticate();else setAccessState("error");});
    };
    window.addEventListener("pageshow",refresh);window.addEventListener("focus",refresh);window.addEventListener("xinsight:scope-changed",refresh);
    refresh();
    return()=>{alive=false;controller.abort();window.removeEventListener("pageshow",refresh);window.removeEventListener("focus",refresh);window.removeEventListener("xinsight:scope-changed",refresh);};
  },[route,scopedRead,scopeKey,unauthenticate]);

  const navigate = useCallback((path: string, selectedRuntimeRequestRef?: string, activityReading?: RuntimeNavigation) => {
    const current=window.location.pathname+window.location.search;
    const retained=window.history.state?.workbenchOrigin;
    const origin=validOriginPath(retained)?retained:validOriginPath(current)?current:null;
    const executionList = window.history.state?.executionList;
    const overview: OverviewContext | undefined = window.history.state?.overviewIdentity === scopeKey ? window.history.state?.overviewContext : undefined;
    if (current === "/inbox" && overview) {
      if (path.startsWith("/workbench/cost")) workbenchSnapshot.current=null;
      if (path.startsWith("/workbench/runtime")) runtimeSnapshot.current=null;
    }
    const switchingWorkbenchView=current.startsWith("/workbench/cost")&&path.startsWith("/workbench/cost")&&(current.includes("view=usage")!==path.includes("view=usage"));
    if(switchingWorkbenchView){
      let retainedRequest:WorkbenchReadingRequest|undefined;
      try{retainedRequest=readWorkbenchNavigation(window.history.state?.workbenchReading)?.request}catch{retainedRequest=undefined}
      workbenchTransitionConfig.current=workbenchSnapshot.current?.appliedConfig??retainedRequest??null;
      workbenchSnapshot.current=null;
    }else if(!path.startsWith("/workbench/cost"))workbenchTransitionConfig.current=null;
    const continuingCost = current.startsWith("/workbench/cost") || (validOriginPath(retained) && retained.startsWith("/workbench/cost"));
    const reading = continuingCost && current !== "/inbox" && !switchingWorkbenchView ? window.history.state?.workbenchReading : current === "/inbox" && (path.startsWith("/workbench/cost") || path.startsWith("/executions/")) && overview?.economicReadingID && session ? {
      version: 1, owner: navigationOwner(session), readingID: overview.economicReadingID, request: overview.request,
      dimension: path.includes("view=usage") ? "purpose" : "source_project", mode: "top5", resolution: "overview", activeBucket: "", contributionPages: 1, detailPages: 1,
      ...(path.startsWith("/executions/") && overview.runtimeReadingID ? { related: { executionID: path.split("/")[2].split("?")[0], readingID: overview.runtimeReadingID } } : {}),
    } satisfies WorkbenchNavigation : undefined;
    const activityContext = window.history.state?.workActivityContext?.owner === scopeKey ? window.history.state.workActivityContext : undefined;
    const savedActivityOrigin = window.history.state?.activityOrigin;
    const activityOrigin = window.location.pathname === "/work/activity" && activityReading ? current :
      activityContext && typeof savedActivityOrigin === "string" && /^\/work\/activity(?:\?[^#]*)?$/.test(savedActivityOrigin) ? savedActivityOrigin : undefined;
    const continuingRuntime=current.startsWith("/workbench/runtime")||(validOriginPath(retained)&&retained.startsWith("/workbench/runtime"));
    let runtimeReading:RuntimeNavigation|null|undefined;
    if(activityReading && window.location.pathname === "/work/activity" && path === "/workbench/runtime" && session && activityReading.owner === navigationOwner(session)) {
      runtimeReading = readRuntimeNavigation(activityReading);
      runtimeSnapshot.current = null;
    } else if(continuingRuntime&&current!=="/inbox"){
      try{runtimeReading=readRuntimeNavigation(window.history.state?.runtimeReading)}catch{runtimeReading=null}
    } else if (current === "/inbox" && path.startsWith("/workbench/runtime") && overview?.runtimeReadingID && session) {
      runtimeReading = { version: 1, owner: navigationOwner(session), readingID: overview.runtimeReadingID, entry: selectedRuntimeRequestRef ? "all" : "known", selectedRef: selectedRuntimeRequestRef, detailView: "story",
        request: { contract_version: "xinsight.runtime-reading.v1", metric_version: "xinsight.runtime.request-observations.v1", period: overview.request.period, filters: overview.request.filters } };
    }
    const keepActivity = activityContext && activityOrigin && /^\/(?:work\/activity|workbench\/runtime|executions)(?:[/?]|$)/.test(path);
    window.history.pushState({ ...(keepActivity ? {workActivityContext:activityContext,activityOrigin} : {}), ...(runtimeReading!==undefined?{runtimeReading}:{}), ...(reading ? {workbenchReading:reading} : {}), ...(overview ? {overviewContext:overview, overviewIdentity:scopeKey} : {}), ...(executionList ? { executionList } : {}), ...((path.startsWith("/executions/")||path.startsWith("/executions?")||path.startsWith("/cases/"))&&validOriginPath(origin)?{workbenchOrigin:origin}:{}) }, "", path);
    setRoute(readRoute());
    setMobileNavOpen(false);
    window.scrollTo?.({ top: 0, behavior: "auto" });
  }, [scopeKey, session]);

  function navigateFromLink(event: MouseEvent<HTMLAnchorElement>, path: string) {
    if (event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) {
      return;
    }
    event.preventDefault();
    navigate(path);
  }

  function authenticated(current: AccessSessionResponse) {
    runtimeSnapshot.current=null;
    workbenchSnapshot.current=null;
    workbenchTransitionConfig.current=null;
    acceptScopeSession(current);
    setSession(current);
    setAccessState("authenticated");
    window.history.replaceState(null, "", homePath(current));
    setRoute(readRoute());
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
      setLogoutError("退出失败；当前账户仍保持登录。");
    }
  }

  if (accessState === "loading") {
    return (
      <main className="access-state-shell" aria-live="polite">
        <span className="status-dot" aria-hidden="true" />
        <p>正在验证登录状态…</p>
      </main>
    );
  }

  if (accessState === "scope-denied") {
    return <main className="access-state-shell" id="main-content" tabIndex={-1}>
      <h1>当前操作被拒绝</h1><p>会话仍有效，但本次资料访问未获授权。已停止自动读取，请确认权限后重试。</p>
      <button onClick={()=>{deniedScope.current=null;navigate("/inbox");setAccessState("authenticated");}}>返回总览</button>
    </main>;
  }

  if (accessState === "error") {
    return (
      <main className="access-state-shell" id="main-content" tabIndex={-1}>
        <h1>暂时无法验证登录状态</h1>
        <p>没有进入证据调查页面。请确认当前产品服务可用后刷新。</p>
      </main>
    );
  }

  if (accessState === "unauthenticated" || session === null || route.kind === "login") {
    return <LoginPage onAuthenticated={authenticated} />;
  }

  if (session.scope_revision === "no_authorized_scope") {
    return (
      <main className="login-shell" id="main-content" tabIndex={-1}>
        <section className="login-card" aria-labelledby="access-scope-title">
          <div className="login-brand" aria-hidden="true">XI</div>
          <h1 id="access-scope-title">当前没有可访问的资料范围</h1>
          <p className="login-boundary" role="status">
            当前页面未显示工作资料。请联系管理员确认来源与项目授权。
          </p>
          <button className="primary-button" onClick={() => void logout()}>退出登录</button>
          {logoutError ? <p className="login-error" role="alert">{logoutError}</p> : null}
        </section>
      </main>
    );
  }

  const canReadEvidence = session.session.permissions.includes("evidence.read");
  const scoped=session.session.permissions.includes("investigation.scoped.read");
  const canInvestigate=canReadEvidence||scoped;
  const productHome = homePath(session);
  const overviewContext: OverviewContext | undefined = window.history.state?.overviewIdentity === scopeKey ? window.history.state?.overviewContext : undefined;
  let workbenchNavigation: WorkbenchNavigation | undefined;
  let navigationInvalid = false;
  try { workbenchNavigation = readWorkbenchNavigation(window.history.state?.workbenchReading); } catch { navigationInvalid = true; }
  let runtimeNavigation:RuntimeNavigation|undefined;
  let runtimeNavigationInvalid=false;
  try{runtimeNavigation=readRuntimeNavigation(window.history.state?.runtimeReading)}catch{runtimeNavigationInvalid=true}
  const canReadWorkbench=session.session.permissions.includes("workbench.summary.read");
  const canReadRuntime=canReadWorkbench&&session.session.permissions.includes("workbench.details.read");
  const capabilities=new URLSearchParams(window.location.search).get("view")==="capabilities";
  const usage=new URLSearchParams(window.location.search).get("view")==="usage";
  const budgets=new URLSearchParams(window.location.search).get("view")==="budgets";
  const retainedOrigin=window.history.state?.workbenchOrigin;
  const origin=["detail","case-detail"].includes(route.kind)&&validOriginPath(retainedOrigin)&&routeAllowed(readRoute(new URL(retainedOrigin,window.location.origin)),session)?retainedOrigin as string:null;
  const workbenchOrigin=!!origin?.startsWith("/workbench/cost");
  const safetyOrigin=!!origin&&/^\/(?:cases|investigation|discovery)(?:[/?]|$)/.test(origin);
  const overviewOrigin=origin==="/inbox";
  const sourcesOrigin=origin==="/connectors";
  const inWorkbench=route.kind==="workbench"||workbenchOrigin;
  const usageTopic=workbenchOrigin?!!origin?.endsWith("?view=usage"):usage;
  const executionTopic=(["runtime","work-activity","search","detail","sessions","session-detail","compare"].includes(route.kind)||(route.kind==="case-detail"&&!!origin))&&!workbenchOrigin&&!safetyOrigin&&!overviewOrigin&&!sourcesOrigin;
  const safetyTopic=["investigation-queue","cases","discovery"].includes(route.kind)||(route.kind==="case-detail"&&!origin)||safetyOrigin;
  const topic=route.kind==="work-activity"?"工作活动与数据":inWorkbench?(usageTopic?"使用与目的":"费用与使用"):executionTopic?"运行与执行":safetyTopic?"安全与复核":route.kind==="connectors"||sourcesOrigin?"来源与覆盖":route.kind==="inbox"||overviewOrigin?"总览":"访问管理";

  return (
    <div className="app-shell" key={scopeKey}>
      <a className="skip-link" href="#main-content">跳到主要内容</a>
      <header className="workbench-topbar">
        <a className="brand" href={productHome} aria-label="X-Insight 首页" onClick={event=>navigateFromLink(event,productHome)}><span className="brand-mark" aria-hidden="true">X</span><strong>X-Insight</strong></a>
        <nav className="workbench-breadcrumb" aria-label="当前位置"><span>工作台</span><span aria-hidden="true">/</span>{origin?<a href={origin} onClick={event=>navigateFromLink(event,origin)}>{topic}</a>:<span>{topic}</span>}{route.kind==="detail"||route.kind==="case-detail"?<><span aria-hidden="true">/</span><span>{route.kind==="detail"?"执行详情":"案件详情"}</span></>:null}</nav>
        <span className="workbench-space">工作空间</span>
      </header>
      <aside className={`sidebar${mobileNavOpen ? " sidebar-nav-open" : ""}`} aria-label="主导航">
        <div className="sidebar-header">
          <span className="sidebar-space-label">工作空间</span>
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
          {canReadEvidence||canReadWorkbench ? <NavLink active={route.kind === "inbox"||overviewOrigin} href="/inbox" icon="posture" label="总览" detail="使用、费用与运行" onClick={navigateFromLink} /> : null}
          {canReadWorkbench ? <NavLink active={inWorkbench&&usageTopic} href="/workbench/cost?view=usage" icon="workbench" label="使用与目的" detail="已知请求与归属" onClick={navigateFromLink} /> : null}
          {canInvestigate||canReadRuntime ? <NavLink active={executionTopic&&route.kind!=="work-activity"} href={canReadRuntime?"/workbench/runtime":"/executions"} icon="executions" label="运行与执行" detail="运行、能力与执行记录" onClick={navigateFromLink} /> : null}
          {canReadRuntime ? <NavLink active={route.kind==="work-activity"} href="/work/activity" icon="executions" label="工作活动与数据" detail="工具、资源与来源报告" onClick={navigateFromLink} /> : null}
          {canInvestigate ? <>
            <NavLink active={safetyTopic} href="/investigation/queue" icon="cases" label="安全与复核" detail="调查事项" onClick={navigateFromLink} />
          </> : null}
          {canReadWorkbench ? <NavLink active={inWorkbench&&!usageTopic} href="/workbench/cost" icon="workbench" label="费用与使用" detail="共同阅读" onClick={navigateFromLink} /> : null}
          {canReadEvidence ? <NavLink active={route.kind==="connectors"||sourcesOrigin} href="/connectors" icon="connectors" label="来源与覆盖" detail="连接器" onClick={navigateFromLink} /> : null}
          {session.session.permissions.some(permission => ["access.sessions.read", "service_identities.read", "review.policy.manage"].includes(permission)) ? <span className="nav-section-label">管理</span> : null}
          {session.session.permissions.includes("review.policy.manage") ? <NavLink active={route.kind === "review-policy"} href="/access/review-policy" icon="access" label="正文策略" detail="留存与容量" onClick={navigateFromLink} /> : null}
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

        <div className="session-card" aria-label="当前登录">
          <span className="eyebrow">已登录</span>
          <small>按当前账户的权限查看</small>
          <button type="button" onClick={() => void logout()}>退出登录</button>
          {logoutError ? <p role="alert">{logoutError}</p> : null}
        </div>

      </aside>

      <main id="main-content" tabIndex={-1} className={`workspace${route.kind === "detail" ? " workspace-detail" : ""}`}>
        {(executionTopic||safetyTopic)?<nav className="topic-tabs" aria-label={`${topic}视图`}>
          {executionTopic&&canReadRuntime ? <>
            <a href="/workbench/runtime" aria-current={route.kind==="runtime"&&!capabilities?"page":undefined} onClick={event=>navigateFromLink(event,"/workbench/runtime")}>运行概览</a>
            <a href="/work/activity" aria-current={route.kind==="work-activity"?"page":undefined} onClick={event=>navigateFromLink(event,"/work/activity")}>工作活动</a>
            <a href="/workbench/runtime?view=capabilities" aria-current={route.kind==="runtime"&&capabilities?"page":undefined} onClick={event=>navigateFromLink(event,"/workbench/runtime?view=capabilities")}>模型与能力</a>
          </> : null}
          {(!executionTopic||canInvestigate) ? (executionTopic?[["/executions","执行记录",["search","detail"]],["/sessions","会话",["sessions","session-detail"]],["/compare","执行对比",["compare"]]]:[["/investigation/queue","调查事项",["investigation-queue"]],["/cases","调查案件",["cases","case-detail"]],["/discovery","调查发现",["discovery"]]]).filter(([path])=>!scoped||!["/sessions","/discovery"].includes(path as string)).map(([path,label,kinds])=><a key={path as string} href={path as string} aria-current={(kinds as string[]).includes(route.kind)?"page":undefined} onClick={event=>navigateFromLink(event,path as string)}>{label as string}</a>) : null}
        </nav>:null}
        {origin && !(overviewOrigin && route.kind === "detail") ? <a className="origin-return" href={origin} onClick={event=>navigateFromLink(event,origin)}>← 返回{topic}{workbenchOrigin?"中的所选贡献":"的原页面"}</a>:null}
        {route.kind === "runtime" && window.history.state?.workActivityContext?.owner === scopeKey &&
          typeof window.history.state?.activityOrigin === "string" && /^\/work\/activity(?:\?[^#]*)?$/.test(window.history.state.activityOrigin) ?
          <button className="overview-return" type="button" onClick={() => navigate(window.history.state.activityOrigin)}>← 返回工作活动与当前筛选</button> : null}
        {overviewContext && route.kind !== "inbox" && route.kind !== "work-activity" && !overviewOrigin ? <button className="overview-return" type="button" onClick={() => navigate("/inbox")}>← 返回总览</button> : null}
        {route.kind === "inbox" ? (
          canReadWorkbench ? <OverviewPage session={session} navigate={navigate} context={overviewContext} onContext={keepOverview} /> : <PosturePage session={session} navigate={navigate} />
        ) : route.kind === "workbench" && session.session.permissions.includes("workbench.summary.read") ? (
          budgets ? <BudgetPage session={session} navigate={navigate} /> : <WorkbenchPage key={`${scopeKey}:${usage?"usage":"cost"}`} navigation={workbenchNavigation} navigationInvalid={navigationInvalid} onNavigation={keepWorkbenchNavigation} initialConfig={workbenchTransitionConfig.current??overviewContext?.request} session={session} navigate={navigate} snapshot={workbenchSnapshot.current} onSnapshot={keepWorkbench} usage={usage} />
        ) : route.kind === "runtime" && canReadRuntime ? (
          <RuntimePage navigation={runtimeNavigation} navigationInvalid={runtimeNavigationInvalid} initialRequest={overviewContext ? {contract_version:"xinsight.runtime-reading.v1",metric_version:"xinsight.runtime.request-observations.v1",period:overviewContext.request.period,filters:overviewContext.request.filters} : undefined} session={session} snapshot={runtimeSnapshot.current} onSnapshot={keepRuntime} capabilities={capabilities} navigate={navigate} canInvestigate={canInvestigate} />
        ) : route.kind === "work-activity" && canReadRuntime ? (
          <WorkActivityPage session={session} navigate={navigate} />
        ) : route.kind === "search" ? (
          <ExecutionSearchPage navigate={navigate} session={session} />
        ) : route.kind === "detail" ? (
          <ExecutionDetailPage
            key={route.executionID}
            workbenchNavigation={workbenchOrigin || overviewOrigin ? workbenchNavigation : undefined}
            navigationInvalid={(workbenchOrigin || overviewOrigin) && navigationInvalid}
            runtimeSnapshot={origin?.startsWith("/workbench/runtime") ? runtimeSnapshot.current : null}
            executionID={route.executionID}
            returnTo={origin?{path:origin,label:topic}:undefined}
            navigate={navigate}
            session={session}
          />
        ) : route.kind === "investigation-queue" ? (
          <InvestigationQueuePage session={session} navigate={navigate} />
        ) : route.kind === "cases" ? (
          <CasesPage session={session} navigate={navigate} />
        ) : route.kind === "case-detail" ? (
          <CaseDetailPage key={route.caseID} caseID={route.caseID} session={session} navigate={navigate} />
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
        ) : route.kind === "review-policy" && session.session.permissions.includes("review.policy.manage") ? (
          <ReviewPolicyPage session={session} />
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
          <span>按当前授权范围展示资料</span>
        </footer>
      </main>
    </div>
  );
}

function validOriginPath(value: unknown): value is string {
  if(typeof value!=="string")return false;
  return value==="/connectors"||/^\/workbench\/cost(?:\?view=usage)?$/.test(value)|| /^\/workbench\/runtime(?:\?view=capabilities)?$/.test(value)||/^\/(?:inbox|executions|cases(?:\/[A-Za-z0-9_-]+)?|investigation\/queue|discovery|sessions(?:\/[A-Za-z0-9_-]+)?|compare)(?:\?[^#]*)?$/.test(value);
}

function readRoute(location: Pick<Location,"pathname" | "search"> = window.location): Route {
  const path = location.pathname.replace(/\/+$/, "") || "/";
  if (path === "/login") {
    return { kind: "login" };
  }
  if (path === "/access/sessions") {
    return { kind: "access-sessions" };
  }
  if (path === "/access/service-identities") {
    return { kind: "service-identities" };
  }
  if (path === "/access/review-policy") { return { kind: "review-policy" }; }
  if (path === "/workbench/runtime") { return { kind: "runtime" }; }
  if (path === "/work/activity") { return { kind: "work-activity" }; }
  if (path === "/workbench/cost") {
    return { kind: "workbench" };
  }
  if (path === "/" || path === "/inbox") {
    return { kind: "inbox" };
  }
  if (path === "/executions") {
    return { kind: "search" };
  }
  if (path === "/investigation/queue") { return { kind: "investigation-queue" }; }
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
  return { kind: "detail", executionID };
}

function homePath(session: AccessSessionResponse): string {
  return session.session.permissions.some(permission => ["evidence.read", "workbench.summary.read"].includes(permission)) ? "/inbox" : "/workbench/cost";
}

function routeAllowed(route: Route, session: AccessSessionResponse): boolean {
  if (route.kind === "login") {
    return true;
  }
  if (route.kind === "inbox") {
    return session.session.permissions.some(permission => ["evidence.read", "workbench.summary.read"].includes(permission));
  }
  if (route.kind === "runtime" || route.kind === "work-activity") {
    return session.session.permissions.includes("workbench.summary.read") && session.session.permissions.includes("workbench.details.read");
  }
  if (route.kind === "workbench") {
    return session.session.permissions.includes("workbench.summary.read");
  }
  if (route.kind === "access-sessions") {
    return session.session.permissions.includes("access.sessions.read");
  }
  if (route.kind === "service-identities") {
    return session.session.permissions.includes("service_identities.read");
  }
  if (route.kind === "review-policy") return session.session.permissions.includes("review.policy.manage");
  if(session.session.permissions.includes("investigation.scoped.read")) return ["search","detail","compare","cases","case-detail","investigation-queue"].includes(route.kind);
  return session.session.permissions.includes("evidence.read");
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

type NavIconName = "posture" | "workbench" | "executions" | "cases" | "sessions" | "compare" | "discovery" | "connectors" | "access" | "agent" | "manual";

function NavIcon({ name }: { name: NavIconName }) {
  return (
    <span className="nav-symbol" aria-hidden="true">
      <svg viewBox="0 0 20 20" focusable="false">
        {name === "posture" ? <><path d="M3.5 13.5a6.5 6.5 0 1 1 13 0" /><path d="m10 10 3-3" /><circle cx="10" cy="10" r="1" /></> : null}
        {name === "workbench" ? <><path d="M3 16V9M8 16V5M13 16v-7M17 16V3"/><path d="M2 16h16"/></> : null}
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
