import type { WorkActivityEvent, WorkActivityPage } from "./api";

export type WorkActivityItem = WorkActivityPage["items"][number];

export function groupActivityPage(items: WorkActivityItem[]) {
  const groups = new Map<string, { key: string; runID?: string; items: WorkActivityItem[] }>();
  for (const item of items) {
    const { reporter, run_id: runID, event_ref: eventRef } = item.event;
    const scope = [reporter.source_system_id, reporter.source_instance, reporter.org_id, reporter.project_id];
    const key = JSON.stringify(runID ? [...scope, "run", runID] : [...scope, "event", eventRef]);
    const group = groups.get(key) ?? { key, runID, items: [] };
    group.items.push(item);
    groups.set(key, group);
  }
  return [...groups.values()];
}

export function resourceOperation(event: WorkActivityEvent): "read" | "change" | "other" | undefined {
  if (event.subject_kind !== "resource_action") return;
  if (["resource.read", "resource.read.failed", "resource.read.not_found", "resource.read.unconfirmed"].includes(event.action_kind)) return "read";
  if (["resource.write", "resource.write.failed", "resource.edit", "resource.edit.failed"].includes(event.action_kind)) return "change";
  return "other";
}

function resourceOutcome(event: WorkActivityEvent): "completed" | "failed" | "optional_not_found" | "unknown" {
  if (event.action_kind === "resource.read.not_found" && event.lifecycle_state === "failed") return "optional_not_found";
  if (event.action_kind.endsWith(".failed") || event.lifecycle_state === "failed") return "failed";
  if (event.lifecycle_state === "completed" && ["resource.read", "resource.write", "resource.edit"].includes(event.action_kind)) return "completed";
  return "unknown";
}

export function workEventLabel(event: WorkActivityEvent): string {
  switch (event.action_kind) {
    case "task.started": return "任务开始";
    case "task.completed": return "任务框架正常结束";
    case "task.failed": return "任务框架报告失败";
    case "task.ended_observed": return "观察到任务结束，结果未确认";
    case "message.received_observed": return "观察到收到消息";
    case "message.sent_observed": return "观察到发送消息";
    case "model.requested": return "请求模型";
    case "model.started": return "模型调用开始";
    case "model.completed": return "模型调用返回";
    case "model.failed": return "模型调用失败";
    case "model.input_observed": return "观察到模型输入，未关联具体调用";
    case "model.output_observed": return "观察到模型输出，未关联具体调用";
    case "tool.invoked": return "调用工具";
    case "tool.completed": return "工具返回";
    case "tool.failed": return "工具调用失败";
    case "resource.read": return resourceOutcome(event) === "completed" ? "来源报告读取完成" : resourceOutcome(event) === "failed" ? "读取失败" : "读取结果未确认";
    case "resource.read.failed": return "读取失败";
    case "resource.read.not_found": return resourceOutcome(event) === "optional_not_found" ? "可选路径不存在，未读取内容" : "路径缺失观察，读取结果未确认";
    case "resource.read.unconfirmed": return "可选读取结果未确认";
    case "resource.write": return resourceOutcome(event) === "completed" ? "来源报告写入完成" : resourceOutcome(event) === "failed" ? "写入失败" : "写入结果未确认";
    case "resource.write.failed": return "写入失败";
    case "resource.edit": return resourceOutcome(event) === "completed" ? "来源报告修改完成" : resourceOutcome(event) === "failed" ? "修改失败" : "修改结果未确认";
    case "resource.edit.failed": return "修改失败";
    default: return ({ task: "任务观察", message: "消息观察", model_call: "模型观察", tool_call: "工具观察", resource_action: "资源操作" } as Record<string,string>)[event.subject_kind] ?? "工作活动";
  }
}

export function workEventObject(event: WorkActivityEvent): string {
  if (event.subject_kind === "task") return "运行状态";
  if (event.subject_kind === "message") return "消息";
  if (event.subject_kind === "model_call") return "模型交互";
  if (event.subject_kind === "tool_call") {
    return event.resource?.name_state === "readable" ? `工具：${event.resource.name ?? "名称未提供"}` : "工具名称未报告";
  }
  return event.resource?.name_state === "readable" ? event.resource.name ?? "资源名称未报告" :
    event.resource?.name_state === "hidden" ? "资源名称按来源策略隐藏" : "资源对象未报告";
}

export function workEventResultSummary(event: WorkActivityEvent): string {
  if (event.subject_kind === "task") {
    if (event.action_kind === "task.completed") return "框架正常结束";
    if (event.action_kind === "task.failed") return "框架报告失败";
    return "框架结果未确认";
  }
  if (event.subject_kind === "resource_action") {
    const outcome = resourceOutcome(event);
    if (outcome === "optional_not_found") return "可选路径不存在";
    if (outcome === "failed") return "来源报告失败";
    if (outcome === "completed") return "来源报告完成";
    return "结果未确认";
  }
  if (event.subject_kind === "message") return "来源消息观察";
  if (event.action_kind.endsWith(".failed") || event.lifecycle_state === "failed") return "来源报告失败";
  if (event.lifecycle_state === "completed") return "来源报告返回";
  return "来源观察，结果未确认";
}

export function workEventResultNote(event: WorkActivityEvent): string {
  if (event.subject_kind === "task") return "只说明框架终态，不代表业务目标完成或资源丢失。";
  if (event.subject_kind === "resource_action") {
    const outcome = resourceOutcome(event);
    if (event.action_kind === "resource.read.not_found") {
      if (outcome === "optional_not_found") return "来源报告可选路径不存在，未读取内容；不能据此判定工具调用失败。";
      return "来源动作标记路径不存在，但生命周期未确认该资源结果。";
    }
    if (resourceOperation(event) === "other") return "来源报告此资源动作；动作类型未归类，不能推断文件变化。";
    if (outcome === "unknown") return "来源尚未确认操作结果。";
    if (outcome === "failed") return "来源报告操作失败。";
    if (resourceOperation(event) === "read") return "来源报告读取完成；内容未独立核验。";
    return "来源报告操作完成；文件变化未独立核验。";
  }
  if (event.subject_kind === "tool_call") return "工具正文只属于本工具事件；不能凭同次运行或时间邻近绑定某个资源路径。";
  if (event.subject_kind === "model_call") return "模型输入／输出观察与请求关系分别核对；时间先后不证明因果。";
  return "来源报告的消息观察，不证明任务或交付结果。";
}

export function groupWorkRun(items: WorkActivityItem[]) {
  const reads: WorkActivityItem[] = [];
  const changes: WorkActivityItem[] = [];
  const otherResources: WorkActivityItem[] = [];
  const modelCalls = new Map<string, WorkActivityItem[]>();
  const modelObservations: WorkActivityItem[] = [];
  const other: WorkActivityItem[] = [];
  for (const item of items) {
    const event = item.event;
    if (event.subject_kind === "resource_action") {
      const operation = resourceOperation(event);
      (operation === "read" ? reads : operation === "change" ? changes : otherResources).push(item);
    } else if (event.subject_kind === "model_call") {
      if (!event.model_call_id) modelObservations.push(item);
      else modelCalls.set(event.model_call_id, [...(modelCalls.get(event.model_call_id) ?? []), item]);
    } else other.push(item);
  }
  return { reads, changes, otherResources, modelCalls: [...modelCalls].map(([id, events]) => ({ id, events })), modelObservations, other };
}
