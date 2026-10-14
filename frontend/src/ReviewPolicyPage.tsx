import { t, tx, useLocale, useMessageState } from "./i18n";
import { useEffect, useRef, useState, type FormEvent } from "react";
import { ApiRequestError, investigationAPI, isAbortError, reviewPolicyAPI, type AccessSessionResponse,
  type ReviewCaptureDefaultView, type ReviewCaptureSettings, type ReviewCaptureProjectCatalog } from "./api";
import { ReviewURPolicyPanel } from "./ReviewURPolicyPanel";
import { ReviewExceptionPanel } from "./ReviewExceptionPanel";
import { WorkInstallPanel } from "./WorkInstallPanel";

interface Props { session: AccessSessionResponse }
const mib = 1024 * 1024;
const gib = 1024 * mib;
const hour = 3600;
const day = 24 * hour;
type URPairing = "loading" | "paired" | "standalone" | "unknown";
const modeChangedFromEffective = (current: ReviewCaptureDefaultView, draft: ReviewCaptureSettings) =>
  current.state === "unconfigured" || !current.policy.effective || draft.mode !== current.policy.effective.mode;
const amount = (value: number) => Number(value.toFixed(10)).toString();
function showFailure(error: unknown) {
  if (error instanceof ApiRequestError && error.problem?.code === "review_policy_unconfirmed")
    return t("m1796");
  if (error instanceof ApiRequestError && error.status === 403) return t("m1797");
  return t("m1798");
}

export function ReviewPolicyPage({ session }: Props) {
  useLocale();
  const [current, setCurrent] = useState<ReviewCaptureDefaultView>();
  const [draft, setDraft] = useState<ReviewCaptureSettings>();
  const [retentionUnit, setRetentionUnit] = useState<"days" | "hours">("days");
  const [projects, setProjects] = useState<ReviewCaptureProjectCatalog>();
  const [sourceChoice, setSourceChoice] = useState("");
  const [targetProject, setTargetProject] = useState("__new__");
  const [newProjectName, setNewProjectName] = useState("");
  const [busy, setBusy] = useState(false);
  const [pairing, setPairing] = useState<URPairing>("loading");
  const pairingRef = useRef<URPairing>("loading");
  const [enterpriseRequestKey, setEnterpriseRequestKey] = useState(0);
const [enterpriseWrite,setEnterpriseWrite]=useState<"available"|"denied"|"unknown">("unknown");
  const [error, setError] = useMessageState("");
  const [notice, setNotice] = useMessageState("");
  const read = async (signal?: AbortSignal) => {
    const value = await reviewPolicyAPI.current(signal);
    setCurrent(value);
    setDraft(value.state === "unconfigured" ? value.suggested : value.policy.desired);
    const seconds = value.state === "unconfigured" ? value.suggested.retention_seconds : value.policy.desired.retention_seconds;
    setRetentionUnit(seconds % day === 0 ? "days" : "hours");
    setError(() => "");
    return value;
  };
  const readPairing = async (signal?: AbortSignal): Promise<URPairing> => {
    try {
      const value = await investigationAPI.listConnectors(signal);
      const installations = value.installations ?? [];
      const observed: URPairing = installations.some(item => item.connector_type === "unirouter" &&
        Boolean(item.installation_acknowledgement_sha256)) ? "paired" :
        value.installation_control?.enabled && installations.length === 0 ? "standalone" : "unknown";
      const next = pairingRef.current === "paired" ? "paired" : observed;
      pairingRef.current = next; setPairing(next);
      return next;
    } catch (reason) {
      if (!isAbortError(reason)) {
        const next = pairingRef.current === "paired" ? "paired" : "unknown";
        pairingRef.current = next; setPairing(next);
      }
      return pairingRef.current;
    }
  };
  useEffect(() => {
    const controller = new AbortController();
    pairingRef.current = "loading"; setPairing("loading");
    void read(controller.signal).catch(reason => { if (!isAbortError(reason)) setError(() => showFailure(reason)); });
    void readPairing(controller.signal);
    void reviewPolicyAPI.projects(controller.signal).then(setProjects)
      .catch(reason => { if (!isAbortError(reason)) setError(() => t("m1799")); });
    return () => controller.abort();
  }, [session.session.session_ref]);
  async function refresh() {
    setBusy(true); setNotice(() => "");
    try { await read(); setProjects(await reviewPolicyAPI.projects()); await readPairing(); } catch (reason) { setError(() => showFailure(reason)); }
    finally { setBusy(false); }
  }
  async function apply(event: FormEvent) {
    event.preventDefault();
    if (!draft || !current) return;
    if (modeChangedFromEffective(current, draft)) {
      const pairingNow = pairingRef.current === "standalone" ? await readPairing() : pairingRef.current;
      if (pairingNow === "paired") {
 if(enterpriseWrite!=="available"){setNotice(() => enterpriseWrite==="denied"?t("m1800"):t("m1801"));return}
        setEnterpriseRequestKey(previous => previous + 1);
        setNotice(() => t("m1802"));
        return;
      }
      if (pairingNow !== "standalone") {
        setNotice(() => t("m1803"));
        return;
      }
    }
    setBusy(true); setError(() => ""); setNotice(() => "");
    try {
      const result = await reviewPolicyAPI.apply(draft,
        current.state === "unconfigured" ? 0 : current.policy.desired_revision, session.csrf_token);
      setCurrent(result);
      setNotice(() => result.state === "pending" ? t("m1804") : t("m1805"));
    } catch (reason) { setError(() => showFailure(reason)); }
    finally { setBusy(false); }
  }
  async function confirm() {
    if (!current || current.state === "unconfigured" || !current.drift) return;
    setBusy(true); setError(() => ""); setNotice(() => "");
    try {
      const latest = await reviewPolicyAPI.current();
      setCurrent(latest);
      setDraft(latest.state === "unconfigured" ? latest.suggested : latest.policy.desired);
      const seconds = latest.state === "unconfigured" ? latest.suggested.retention_seconds : latest.policy.desired.retention_seconds;
      setRetentionUnit(seconds % day === 0 ? "days" : "hours");
      if (latest.state === "unconfigured" || !latest.drift || latest.policy.desired_revision !== current.policy.desired_revision) {
        setNotice(() => t("m1806"));
        return;
      }
      const result = await reviewPolicyAPI.confirm(latest.policy.desired_revision, session.csrf_token);
      setCurrent(result); setNotice(() => t("m1807"));
    } catch (reason) { setError(() => showFailure(reason)); }
    finally { setBusy(false); }
  }
  const change = (key: keyof ReviewCaptureSettings, value: string) => setDraft(previous => previous ? {
    ...previous, [key]: key === "mode" ? value : Number(value),
  } as ReviewCaptureSettings : previous);
  const formatRetention = (seconds: number) => seconds % day === 0 ? t("m1808", { value0: seconds / day }) : t("m1809", { value0: amount(seconds / hour) });
  const choiceKey = (item: { source_system_id: string; source_instance: string; org_id: string; project_id: string }) =>
    `${item.source_system_id}:${item.source_instance}:${item.org_id}:${item.project_id}`;
  const selectedChoice = projects?.items.find(item => choiceKey(item) === sourceChoice);
  const modeChanged = Boolean(current && draft && modeChangedFromEffective(current, draft));
  async function bindProject(event: FormEvent) {
    event.preventDefault();
    if (!selectedChoice || selectedChoice.xi_project_id) return;
    const existing = projects?.projects.find(item => item.id === targetProject);
    const name = existing?.display_name ?? newProjectName.trim();
    if (!name) return;
    setBusy(true); setError(() => ""); setNotice(() => "");
    try {
      const result = await reviewPolicyAPI.bindProject(selectedChoice, existing?.id ?? "", name, session.csrf_token);
      setProjects(result); setNotice(() => t("m1810"));
      setSourceChoice(""); setNewProjectName("");
    } catch (reason) { setError(() => showFailure(reason)); }
    finally { setBusy(false); }
  }
  return <section className="product-page review-policy-page" aria-labelledby="review-policy-title">
    <header className="page-header"><div><p className="eyebrow">{tx("m1811")}</p><h1 id="review-policy-title">{tx("m1812")}</h1>
      <p>{tx("m1813")}</p></div>
      <button type="button" onClick={() => void refresh()} disabled={busy}>{tx("m1814")}</button></header>
    {error ? <p role="alert">{error}</p> : null}{notice ? <p role="status">{notice}</p> : null}
    {current && draft ? <><section aria-label={t("m1815")}>
      <details><summary>{tx("m1816")}</summary><p>{tx("m1817", { value0: current.state === "unconfigured" ? t("m0021") : current.policy.desired_revision, value1: current.state === "unconfigured" || current.policy.effective_revision === 0 ? t("m0342") : current.policy.effective_revision })}</p></details>
      {current.state !== "unconfigured" && current.drift ? <p role="status">{tx("m1818")}</p> : null}
      {current.state !== "unconfigured" && current.policy.effective ? <p>{tx("m1819", { value0: current.policy.effective.mode === "full_content" ? t("m1792") : t("m1777"), value1: formatRetention(current.policy.effective.retention_seconds), value2: amount(current.policy.effective.project_quota_bytes / gib), value3: amount(current.policy.effective.max_item_bytes / mib) })}</p> : null}
    </section><form onSubmit={event => void apply(event)}>
      <h2>{tx("m1820")}</h2>
      <label>{t("m1821")}<select value={draft.mode} onChange={event => change("mode", event.target.value)} disabled={busy}>
        <option value="disabled">{tx("m1777")}</option><option value="full_content">{tx("m1792")}</option></select></label>
      <label>{t("m1822")}<input type="number" min={retentionUnit === "days" ? 1 / day : 1 / hour}
        max={retentionUnit === "days" ? 365 : 8760} step="any" required
        value={amount(draft.retention_seconds / (retentionUnit === "days" ? day : hour))}
        onChange={event => change("retention_seconds", String(Math.round(Number(event.target.value) * (retentionUnit === "days" ? day : hour))))} disabled={busy} />
        <select aria-label={t("m1823")} value={retentionUnit} onChange={event => setRetentionUnit(event.target.value as "days" | "hours")} disabled={busy}>
          <option value="days">{tx("m1824")}</option><option value="hours">{tx("m1468")}</option></select></label>
      <label>{t("m1825")}<input type="number" min={1 / gib} max="1024" step="any" required
        value={amount(draft.project_quota_bytes / gib)} onChange={event => change("project_quota_bytes", String(Math.round(Number(event.target.value) * gib)))} disabled={busy} /></label>
      <label>{t("m1794")}<input type="number" min="0" max="32" step="any" required
        value={amount(draft.max_item_bytes / mib)} onChange={event => change("max_item_bytes", String(Math.round(Number(event.target.value) * mib)))} disabled={busy} /></label>
      {pairing === "paired"&&enterpriseWrite!=="available"?<p role="status">{enterpriseWrite==="denied"?t("m1826"):t("m1827")}</p>:null}
      {modeChanged && pairing === "paired" ? <p role="status">{tx("m1828")}</p> : null}
      {modeChanged && (pairing === "loading" || pairing === "unknown") ? <p role="status">{tx("m1829")}</p> : null}
      <button type="submit" disabled={busy || modeChanged && (pairing === "loading" || pairing === "unknown" || pairing==="paired"&&enterpriseWrite!=="available")}>
        {modeChanged && pairing === "paired" ? t("m1830") : modeChanged ? t("m1831") : t("m1832")}</button>
      {(pairing === "loading" || pairing === "unknown") ? <button type="button" disabled={busy} onClick={() => void readPairing()}>{tx("m1833")}</button> : null}
      {current.state !== "unconfigured" && current.drift ? <button type="button" onClick={() => void confirm()}
        disabled={busy || JSON.stringify(draft) !== JSON.stringify(current.policy.desired)}>{tx("m1834", { value0: current.policy.desired_revision })}</button> : null}
    </form><section aria-label={t("m1835")}><h2>{tx("m1836")}</h2>
      <p>{tx("m1837")}</p>
      {projects ? <><ul>{projects.items.map(item => <li key={choiceKey(item)}>{tx("m1840", { value0: item.source_name, value1: item.source_instance, value2: item.org_id, value3: item.project_id, value4: item.xi_project_id ? t("m1838", { value0: item.xi_project_name }) : t("m1839") })}</li>)}</ul>
        {projects.items.some(item => !item.xi_project_id) ? <form onSubmit={event => void bindProject(event)}>
          <label>{t("m1841")}<select value={sourceChoice} onChange={event => setSourceChoice(event.target.value)} disabled={busy} required>
            <option value="">{tx("m1788")}</option>{projects.items.filter(item => !item.xi_project_id).map(item =>
              <option key={choiceKey(item)} value={choiceKey(item)}>{tx("m1842", { value0: item.source_name, value1: item.source_instance, value2: item.project_id })}</option>)}</select></label>
          <label>{t("m1843")}<select value={targetProject} onChange={event => setTargetProject(event.target.value)} disabled={busy}>
            <option value="__new__">{tx("m1844")}</option>{projects.projects.map(item => <option key={item.id} value={item.id}>{item.display_name}</option>)}</select></label>
          {targetProject === "__new__" ? <label>{t("m1845")}<input value={newProjectName} onChange={event => setNewProjectName(event.target.value)}
            maxLength={128} required disabled={busy} /></label> : null}
          <button type="submit" disabled={busy || !sourceChoice}>{tx("m1846")}</button>
        </form> : null}</> : <p>{tx("m1847")}</p>}
    </section></> : !error ? <p>{tx("m1848")}</p> : null}
    <ReviewExceptionPanel session={session} current={current} projects={projects} />
    <ReviewURPolicyPanel session={session} xi={current} xiDraft={draft} onXIDraft={setDraft}
      enterpriseRequestKey={enterpriseRequestKey} onEnterpriseWrite={setEnterpriseWrite} onPaired={() => { pairingRef.current = "paired"; setPairing("paired"); }}
      onXIReadback={async () => { setCurrent(await reviewPolicyAPI.current()); }} />
    <WorkInstallPanel session={session} projects={projects} />
  </section>;
}
