import { describe, expect, it } from "vitest";
import type { WorkActivityEvent } from "./api";
import { groupActivityPage, groupWorkRun, workEventLabel, workEventObject, workEventResultNote, workEventResultSummary } from "./workActivityPresentation";

const base: WorkActivityEvent = {
  schema_version: "xinsight.work-event.v1",
  reporter: { source_system_id: "source", source_instance: "instance", org_id: "7", project_id: "9" },
  event_ref: "event", revision: 1, occurred_at: "2026-10-09T00:00:00Z", recorded_at: "2026-10-09T00:00:00Z",
  subject_kind: "resource_action", lifecycle_state: "completed", actor: { kind: "agent", ref: "agent" },
  action_kind: "resource.read", resource: { ref: "file:a", name_state: "readable", name: "report.txt" },
  content_state: "not_captured", content_refs: [],
};
const event = (overrides: Partial<WorkActivityEvent>): WorkActivityEvent => ({ ...base, ...overrides });
const item = (value: WorkActivityEvent) => ({ event: value, event_hash: "hash", received_at: value.occurred_at });

describe("source activity presentation", () => {
  it("groups a page only by the exact reporter scope and declared run ID", () => {
    const groups = groupActivityPage([
      item(event({ event_ref:"first",run_id:"run-1" })),
      item(event({ event_ref:"first",run_id:"run-1",reporter:{...base.reporter,project_id:"10"} })),
      item(event({ event_ref:"second",run_id:"run-1" })),
      item(event({ event_ref:"other-project-extra",run_id:"run-1",reporter:{...base.reporter,project_id:"10"} })),
      item(event({ event_ref:"unlinked" })),
    ]);
    expect(groups.map(group=>group.items.map(row=>row.event.event_ref))).toEqual([["first","second"],["first","other-project-extra"],["unlinked"]]);
    expect(new Set(groups.map(group=>group.key)).size).toBe(groups.length);
  });
  it("keeps framework completion, optional missing, source failure, and unverified writes distinct", () => {
    const task = event({ subject_kind: "task", action_kind: "task.completed", resource: undefined });
    expect(workEventLabel(task)).toBe("任务框架正常结束");
    expect(workEventObject(task)).toBe("运行状态");
    expect(workEventResultNote(task)).toContain("不代表业务目标完成");
    expect(workEventLabel(event({ action_kind: "resource.read.not_found", lifecycle_state: "failed" }))).toContain("可选路径不存在");
    expect(workEventResultNote(event({ action_kind: "resource.read.not_found", lifecycle_state: "failed" }))).toContain("不能据此判定工具调用失败");
    expect(workEventLabel(event({ action_kind: "resource.read.failed", lifecycle_state: "failed" }))).toBe("读取失败");
    expect(workEventLabel(event({ action_kind: "resource.read", lifecycle_state: "unknown" }))).toBe("读取结果未确认");
    expect(workEventLabel(event({ action_kind: "resource.write", lifecycle_state: "started" }))).toBe("写入结果未确认");
    expect(workEventResultNote(event({ action_kind: "resource.read", lifecycle_state: "unknown" }))).toContain("尚未确认");
    expect(workEventResultNote(event({ action_kind: "resource.write" }))).toContain("未独立核验");
    expect(workEventResultSummary(task)).toBe("框架正常结束");
    expect(workEventResultSummary(event({ action_kind: "resource.read.not_found", lifecycle_state: "failed" }))).toBe("可选路径不存在");
    expect(workEventResultSummary(event({ action_kind: "resource.read.failed", lifecycle_state: "failed" }))).toBe("来源报告失败");
    expect(workEventResultSummary(event({ action_kind: "resource.write" }))).toBe("来源报告完成");
    expect(workEventResultSummary(event({ action_kind: "resource.read", lifecycle_state: "unknown" }))).toBe("结果未确认");
    expect(workEventResultSummary(event({ subject_kind: "message", action_kind: "message.sent_observed" }))).toBe("来源消息观察");
  });

  it.each([
    ["failed", "可选路径不存在，未读取内容", "不能据此判定工具调用失败"],
    ["unknown", "路径缺失观察，读取结果未确认", "生命周期未确认"],
    ["started", "路径缺失观察，读取结果未确认", "生命周期未确认"],
    ["completed", "路径缺失观察，读取结果未确认", "生命周期未确认"],
  ] as const)("keeps not_found lifecycle %s in its reported outcome", (lifecycle, label, note) => {
    const observed = event({ action_kind: "resource.read.not_found", lifecycle_state: lifecycle });
    expect(workEventLabel(observed)).toBe(label);
    expect(workEventResultNote(observed)).toContain(note);
  });

  it("groups only events with the same explicit model call ID", () => {
    const result = groupWorkRun([
      item(event({ event_ref: "read", action_kind: "resource.read" })),
      item(event({ event_ref: "write", action_kind: "resource.write" })),
      item(event({ event_ref: "inspect", action_kind: "resource.inspect" })),
      item(event({ event_ref: "policy", action_kind: "resource.write_policy" })),
      item(event({ event_ref: "input", subject_kind: "model_call", action_kind: "model.started", model_call_id: "model-1", resource: undefined })),
      item(event({ event_ref: "output", subject_kind: "model_call", action_kind: "model.completed", model_call_id: "model-1", resource: undefined })),
      item(event({ event_ref: "other", subject_kind: "model_call", action_kind: "model.completed", model_call_id: "model-2", resource: undefined })),
      item(event({ event_ref: "observed", subject_kind: "model_call", action_kind: "model.output_observed", resource: undefined })),
      item(event({ event_ref: "tool", subject_kind: "tool_call", action_kind: "tool.completed", resource: { ref: "tool", name_state: "readable", name: "read_file" } })),
    ]);
    expect(result.reads.map(x => x.event.event_ref)).toEqual(["read"]);
    expect(result.changes.map(x => x.event.event_ref)).toEqual(["write"]);
    expect(result.otherResources.map(x => x.event.event_ref)).toEqual(["inspect", "policy"]);
    expect(workEventResultNote(event({ action_kind: "resource.write_policy" }))).toContain("不能推断文件变化");
    expect(result.modelCalls.map(x => [x.id, x.events.length])).toEqual([["model-1", 2], ["model-2", 1]]);
    expect(result.modelObservations.map(x => x.event.event_ref)).toEqual(["observed"]);
    expect(result.other.map(x => x.event.event_ref)).toEqual(["tool"]);
    expect(workEventObject(result.other[0].event)).toBe("工具：read_file");
  });
});
