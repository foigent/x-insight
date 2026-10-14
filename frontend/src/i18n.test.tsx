import { act, cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { App } from "./App";
import { accessAPI, ApiRequestError } from "./api";
import { LanguageSelector, LocalizedError, getLocale, localeStorageKey, readLocalePreference, setLocale, t, tx, useMessageState, type MessageKey } from "./i18n";
import { formatCanonicalTime, formatMicroCNY, detailFieldLabels } from "./format";
import en from "./i18n/en-US.json";
import zh from "./i18n/zh-CN.json";

beforeEach(() => {
  localStorage.clear();
  setLocale("en-US");
  history.replaceState(null, "", "/login");
});
afterEach(() => { cleanup(); vi.restoreAllMocks(); });

it("uses English for absent, invalid, and inaccessible storage without consulting browser language", () => {
  localStorage.removeItem(localeStorageKey);
  expect(readLocalePreference()).toBe("en-US");
  localStorage.setItem(localeStorageKey, "en");
  expect(readLocalePreference()).toBe("en-US");
  localStorage.setItem(localeStorageKey, "zh-CN");
  expect(readLocalePreference()).toBe("zh-CN");
  vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => { throw new DOMException("blocked", "SecurityError"); });
  expect(readLocalePreference()).toBe("en-US");
  vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => { throw new DOMException("blocked", "SecurityError"); });
  setLocale("zh-CN");
  expect(getLocale()).toBe("zh-CN");
});

it("keeps keys and interpolation parameters complete in both catalogs and leaves supplied data untouched", () => {
  expect(Object.keys(en).sort()).toEqual(Object.keys(zh).sort());
  const parameters = (value: string) => [...value.matchAll(/\{value\d+\}/g)].map(match => match[0]).sort();
  for (const key of Object.keys(en) as MessageKey[]) {
    expect(en[key].trim(), key).not.toBe("");
    expect(parameters(en[key]), key).toEqual(parameters(zh[key]));
    expect(en[key], key).not.toMatch(/[\u3400-\u9fff]/);
  }
  expect(t("m2811", { value0: "用户原文 {value9}" })).toBe("Revision 用户原文 {value9}");
  const { container } = render(<p>{tx("m2811", { value0: <strong>user-supplied &lt;script&gt;</strong> })}</p>);
  expect(container.querySelector("strong")?.textContent).toBe("user-supplied <script>");
  expect(container.querySelector("script")).toBeNull();
});

it("switches a mounted login form, existing errors, metadata, and preference without another request", async () => {
  const session = vi.spyOn(accessAPI, "session").mockRejectedValue(new ApiRequestError(401));
  const providers = vi.spyOn(accessAPI, "providerCapabilities").mockResolvedValue({ local_account_state: "active", oidc_runtime_state: "not_configured", boundary: "synthetic protocol fact" } as Awaited<ReturnType<typeof accessAPI.providerCapabilities>>);
  vi.spyOn(accessAPI, "login").mockRejectedValue(new ApiRequestError(401));
  const user = userEvent.setup();
  render(<App />);
  await screen.findByRole("heading", { name: "Sign in to your workspace" });
  const input = screen.getByLabelText("Username");
  await user.type(input, "用户 supplied-name");
  await user.type(screen.getByLabelText("Password"), "synthetic-password");
  await user.selectOptions(screen.getByRole("combobox", { name: "Language" }), "zh-CN");
  expect(screen.getByLabelText("用户名")).toBe(input);
  expect((screen.getByLabelText("密码") as HTMLInputElement).value).toBe("synthetic-password");
  await user.click(screen.getByRole("button", { name: /^登录$/ }));
  await screen.findByText("用户名或密码无效。");
  const calls = [session.mock.calls.length, providers.mock.calls.length];
  await user.selectOptions(screen.getByRole("combobox", { name: "语言" }), "en-US");
  expect(screen.getByText("Invalid username or password.")).toBeTruthy();
  expect((screen.getByLabelText("Username") as HTMLInputElement).value).toBe("用户 supplied-name");
  expect([session.mock.calls.length, providers.mock.calls.length]).toEqual(calls);
  expect(location.pathname).toBe("/login");
  expect(document.documentElement.lang).toBe("en-US");
  expect(document.title).toBe("X-Insight · Workbench");
  expect(localStorage.getItem(localeStorageKey)).toBe("en-US");
});

it("updates cross-tab preferences and message recipes while preserving exact evidence values", () => {
  function Status() { const [message] = useMessageState("source-provided 用户文字"); return <p>{message}</p>; }
  render(<><LanguageSelector /><Status /></>);
  const error = new LocalizedError(() => t("m0003"));
  const money = formatMicroCNY("1234567");
  const time = formatCanonicalTime("2026-10-11T08:00:00+08:00");
  act(() => {
    localStorage.setItem(localeStorageKey, "zh-CN");
    window.dispatchEvent(new StorageEvent("storage", { key: localeStorageKey }));
  });
  expect(error.message).toBe("用户名或密码无效。");
  expect(detailFieldLabels.cost_micros).toBe("精确成本（micro-CNY）");
  expect(document.documentElement.lang).toBe("zh-CN");
  expect(screen.getByText("source-provided 用户文字")).toBeTruthy();
  expect(formatMicroCNY("1234567")).toBe(money);
  expect(formatCanonicalTime("2026-10-11T08:00:00+08:00")).toBe(time);
  act(() => { localStorage.clear(); window.dispatchEvent(new StorageEvent("storage", { key: null })); });
  expect(getLocale()).toBe("en-US");
});
