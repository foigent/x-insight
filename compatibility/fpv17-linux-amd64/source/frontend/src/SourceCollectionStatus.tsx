import type { WorkbenchSourceFactsStatus } from "./api";
import "./source-facts.css";

const profiles: Record<string, string> = {
  "unirouter.execution-context.v1": "执行上下文",
  "unirouter.execution-identity.v1": "账户与授权来源",
  "unirouter.cost-basis.v1": "计费依据",
};
const states = { active: "已启用", paused: "已暂停", revoked: "已撤销" };
const failures: Record<string, string> = {
  invalid_capabilities: "来源能力与登记不符", scope_changed: "来源范围已变化",
  source_paused: "来源已暂停", invalid_page: "来源页未通过校验",
  cursor_or_epoch_conflict: "消费位置与来源不符", cursor_expired: "消费位置已过期",
  source_authorization_denied: "来源访问授权已失效", source_fact_conflict: "来源事实发生冲突",
  source_unavailable: "暂时无法连接来源", restore_requires_authorization: "恢复后等待管理员重新确认",
};

export function SourceCollectionStatus({ items, formatTime }: {
  items?: WorkbenchSourceFactsStatus[]; formatTime: (value: string) => string;
}) {
  if (!items?.length) return null;
  return <section className="source-collection-profiles" aria-label="来源字段采集状态">
    <strong>来源字段采集</strong>
    {items.map(item => <div key={item.stream_id}>
      <strong>{profiles[item.profile] ?? "来源字段"}</strong>
      <span>{states[item.status]}{item.requires_attention ? " · 需管理员处理" : ""}</span>
      <small>来源 {item.source_instance} · 组织 {item.org_id} · 项目 {item.project_id} · 范围版本 {item.scope_generation}</small>
      <small>最近成功接收页：{item.last_received_at ? formatTime(item.last_received_at) : "尚未接收"}</small>
      {item.last_error ? <p>{failures[item.last_error] ?? "采集暂不可用"}。</p> : null}
      {item.requires_attention ? <p>修正问题并由管理员确认后才能继续，进程重启不会自动恢复。</p> : null}
    </div>)}
    <p>从启用时的位置向前接收；接收页不代表历史已完整采集。停止采集后，历史记录仍按人员阅读权限展示。</p>
  </section>;
}
