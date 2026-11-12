import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { ApiRequestError } from "./api";
import { ErrorState } from "./Feedback";
import { setLocale, t } from "./i18n";

afterEach(cleanup);

describe("authentication and reading permission errors", () => {
  it.each([
    ["en-US", "permission_denied"], ["en-US", "workbench_scope_denied"],
    ["zh-CN", "permission_denied"], ["zh-CN", "workbench_scope_denied"],
  ] as const)("explains the identified application denial in %s with %s", (locale, code) => {
    setLocale(locale);
    render(<ErrorState error={new ApiRequestError(403, {
      type: "urn:test:permission", title: "Permission denied", status: 403,
      code, correlation_id: "synthetic-denial", retryable: false,
    })} />);
    const alert = screen.getByRole("alert");
    expect(alert.textContent).toContain(t("m0702"));
    expect(alert.textContent).toContain("synthetic-denial");
    expect(alert.textContent).not.toMatch(/tunnel|proxy|反向代理/);
  });

  it.each(["en-US", "zh-CN"] as const)("keeps an unidentified 403 unattributed in %s", locale => {
    setLocale(locale);
    render(<ErrorState error={new ApiRequestError(403)} />);
    expect(screen.getByRole("alert").textContent).toContain(t("error.request_denied.title"));
    expect(screen.getByRole("alert").textContent).not.toContain(t("m0702"));
  });

  it.each(["en-US", "zh-CN"] as const)("keeps a transport failure distinct in %s", locale => {
    setLocale(locale);
    render(<ErrorState error={new TypeError("synthetic transport failure")} />);
    expect(screen.getByRole("alert").textContent).toContain(t("m0685"));
    expect(screen.getByRole("alert").textContent).not.toContain(t("m0702"));
  });

  it.each(["en-US", "zh-CN"] as const)("asks for sign-in when the session is unavailable in %s", locale => {
    setLocale(locale);
    render(<ErrorState error={new ApiRequestError(401)} />);
    expect(screen.getByRole("alert").textContent).toContain(t("error.authentication_required.title"));
    expect(screen.getByRole("alert").textContent).not.toContain(t("m0702"));
  });
});
