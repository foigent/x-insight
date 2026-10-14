import { beforeEach as beforeLegacyLocale } from "vitest";
import { setLocale as setLegacyLocale } from "./i18n";
// These existing semantic assertions explicitly exercise the Chinese translation.
beforeLegacyLocale(() => setLegacyLocale("zh-CN"));
import { describe, expect, it } from "vitest";

import { eventTitle } from "./format";

describe("eventTitle", () => {
  it("names completed-execution terminal events explicitly", () => {
    expect(eventTitle({ event_type: "billing.execution.settled", stage: "billing" })).toBe("执行计费已结算");
    expect(eventTitle({ event_type: "billing.execution.cancelled", stage: "billing" })).toBe("执行计费已取消");
  });
});
