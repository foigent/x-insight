import { localizedLabels, t, tx, useLocale } from "./i18n";
import type { WorkbenchSourceFactsStatus } from "./api";
import "./source-facts.css";

const profiles: Record<string, string> = localizedLabels({}, {"unirouter.execution-context.v1": "m2355", "unirouter.execution-identity.v1": "m2356", "unirouter.cost-basis.v1": "m2357"});
const states = localizedLabels({}, {active: "m2002", paused: "m1601", revoked: "m2358"});
const failures: Record<string, string> = localizedLabels({}, {invalid_capabilities: "m2359", scope_changed: "m2360", source_paused: "m2361", invalid_page: "m2362", cursor_or_epoch_conflict: "m2363", cursor_expired: "m2364", source_authorization_denied: "m2365", source_fact_conflict: "m2366", source_unavailable: "m2367", restore_requires_authorization: "m2368"});

export function SourceCollectionStatus({ items, formatTime }: {
  items?: WorkbenchSourceFactsStatus[]; formatTime: (value: string) => string;
}) {
  useLocale();
  if (!items?.length) return null;
  return <section className="source-collection-profiles" aria-label={t("m2369")}>
    <strong>{tx("m2370")}</strong>
    {items.map(item => <div key={item.stream_id}>
      <strong>{profiles[item.profile] ?? t("m2371")}</strong>
      <span>{states[item.status]}{item.requires_attention ? t("m2372") : ""}</span>
      <small>{tx("m2373", { value0: item.source_instance, value1: item.org_id, value2: item.project_id, value3: item.scope_generation })}</small>
      <small>{tx("m2375", { value0: item.last_received_at ? formatTime(item.last_received_at) : t("m2374") })}</small>
      {item.last_error ? <p>{failures[item.last_error] ?? t("m2376")}。</p> : null}
      {item.requires_attention ? <p>{tx("m2377")}</p> : null}
    </div>)}
    <p>{tx("m2378")}</p>
  </section>;
}
