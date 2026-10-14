import { beforeEach as beforeLegacyLocale } from "vitest";
import { setLocale as setLegacyLocale } from "./i18n";
// These existing semantic assertions explicitly exercise the Chinese translation.
beforeLegacyLocale(() => setLegacyLocale("zh-CN"));
import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, expect, it } from "vitest";
import { SourceCollectionStatus } from "./SourceCollectionStatus";
afterEach(cleanup);

it("shows the authorized profile, retained receipt and administrator attention", () => {
  render(<SourceCollectionStatus items={[{ stream_id: "stream", profile: "unirouter.execution-context.v1", source_instance: "synthetic-source", org_id: "17", project_id: "101", scope_generation: "2", status: "paused", requires_attention: true, last_error: "cursor_expired", last_received_at: "2026-09-11T01:02:03Z" }]} formatTime={value => value} />);
  expect(screen.getByText("执行上下文")).toBeTruthy();
  expect(screen.getByText(/已暂停 · 需管理员处理/)).toBeTruthy();
  expect(screen.getByText("消费位置已过期。")).toBeTruthy();
  expect(screen.getByText(/2026-09-11T01:02:03Z/)).toBeTruthy();
  expect(screen.queryByText("账户与授权来源")).toBeNull();
});
it("omits collection details when none are authorized", () => {
  render(<SourceCollectionStatus formatTime={value => value} />);
  expect(screen.queryByLabelText("来源字段采集状态")).toBeNull();
});
