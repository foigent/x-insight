import { ApiRequestError } from "./api";

export function LoadingBlock({ label = "正在读取证据" }: { label?: string }) {
  return (
    <div className="loading-block" role="status" aria-live="polite">
      <span className="loading-spinner" aria-hidden="true" />
      <div>
        <strong>{label}</strong>
        <p>仅查询当前受限快照，不补写事实。</p>
      </div>
    </div>
  );
}

export function ErrorState({
  error,
  onRetry,
  compact = false,
}: {
  error: unknown;
  onRetry?: () => void;
  compact?: boolean;
}) {
  const message = errorMessage(error);
  const problem = error instanceof ApiRequestError ? error.problem : undefined;
  return (
    <div className={`error-state${compact ? " error-state-compact" : ""}`} role="alert">
      <span className="error-mark" aria-hidden="true">!</span>
      <div>
        <strong>{message.title}</strong>
        <p>{message.description}</p>
        {problem?.correlation_id && (
          <p className="correlation-ref">
            关联编号 <code>{problem.correlation_id}</code>
          </p>
        )}
        {onRetry && (
          <button type="button" className="button button-secondary" onClick={onRetry}>
            重试
          </button>
        )}
      </div>
    </div>
  );
}

export function EmptyState({ title, description }: { title: string; description: string }) {
  return (
    <div className="empty-state">
      <span aria-hidden="true">∅</span>
      <strong>{title}</strong>
      <p>{description}</p>
    </div>
  );
}

function errorMessage(error: unknown): { title: string; description: string } {
  if (error instanceof DOMException && error.name === "AbortError") {
    return { title: "请求已取消", description: "页面切换取消了上一次读取。" };
  }
  if (!(error instanceof ApiRequestError)) {
    return { title: "无法读取调查数据", description: "网络连接或服务响应异常，请确认本机服务状态后重试。" };
  }
  switch (error.problem?.code) {
    case "execution_not_found":
      return { title: "未找到该执行", description: "该 execution ID 在当前快照中不存在，或证据尚未导入。" };
    case "event_not_found":
      return { title: "未找到该证据事件", description: "事件不属于当前执行，或不在当前 as_of 快照内。" };
    case "cursor_expired":
      return { title: "调查快照已过期", description: "固定快照只支持 24 小时，请返回搜索重新获取当前快照。" };
    case "cursor_mismatch":
    case "invalid_cursor":
      return { title: "分页状态不匹配", description: "筛选条件或执行范围已变化，请从第一页重新读取。" };
    case "request_rate_limited":
      return { title: "请求过快", description: "服务正在保护查询预算，请稍后重试。" };
    case "temporarily_overloaded":
      return { title: "调查服务暂时过载", description: "查询已在受限预算内停止，没有扩大数据库工作量。" };
    case "database_unavailable":
      return { title: "证据库不可用", description: "X-Insight 无法读取 PostgreSQL；UniRouter 路由不依赖此服务。" };
    case "invalid_argument":
      return { title: "筛选参数无效", description: "请检查时间范围、ID 形式或重复参数后重试。" };
    default:
      if (error.status === 401 || error.status === 403) {
        return { title: "没有读取权限", description: "请由现场 IT 检查 tunnel 或反向代理的访问授权。" };
      }
      return { title: "调查请求失败", description: "服务返回了安全错误响应，未暴露内部数据库细节。" };
  }
}
