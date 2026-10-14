import type { ExecutionView } from "./executionReading";

export function ViewTabs({
  active,
  onChange, contents = false, contentOnly = false,
}: {
  contents?: boolean; contentOnly?: boolean; active: ExecutionView;
  onChange: (view: ExecutionView) => void;
}) {
  const tabs = [
    ["story", "执行概况", "结果、路径与未知"],
    ["graph", "因果图", "仅显式因果边"],
    ["chronology", "时间线", "原始时间顺序"],
    ["proof", "证据依据", "来源、哈希与缺口"],
    ["systems", "系统与数据", "资源、输入与目的地"],
  ] as const;
  const visible: readonly (readonly [ExecutionView,string,string])[] = [...(contentOnly?[]:tabs),...(contents?[["contents","交互内容","保存表示与人工复核"] as const]:[])];
  return (
    <nav className="execution-view-tabs" aria-label="执行详情视图">
      {visible.map(([view, label, description]) => (
        <button
          key={view}
          type="button"
          className={active === view ? "execution-view-tab-active" : undefined}
          aria-current={active === view ? "page" : undefined}
          onClick={() => onChange(view)}
        >
          <strong>{label}</strong>
          <small>{description}</small>
        </button>
      ))}
    </nav>
  );
}
