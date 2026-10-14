import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, it, vi } from "vitest";
import { Identifier, compactIdentifier } from "./Identifier";

afterEach(() => { cleanup(); vi.restoreAllMocks(); });

it("expands and copies the complete identifier instead of its shortened label", async () => {
  const user = userEvent.setup();
  const value = `request-${"a".repeat(80)}-last1234`;
  const write = vi.spyOn(navigator.clipboard, "writeText").mockResolvedValue();
  render(<Identifier value={value} label="请求标识" />);
  const summary = screen.getByText(`请求标识 · ${compactIdentifier(value)}`);
  expect(summary.closest("details")?.open).toBe(false);
  await user.click(summary);
  expect(summary.closest("details")?.open).toBe(true);
  expect(screen.getByText(value).tagName).toBe("CODE");
  await user.click(screen.getByRole("button", { name: "复制完整请求标识" }));
  expect(write).toHaveBeenCalledWith(value);
  expect(screen.getByRole("status").textContent).toBe("已复制");
});

it("keeps the complete value available when clipboard access fails", async () => {
  const user = userEvent.setup();
  vi.spyOn(navigator.clipboard, "writeText").mockRejectedValue(new Error("unavailable"));
  render(<Identifier value="request-short" />);
  await user.click(screen.getByText("标识 · request-short"));
  await user.click(screen.getByRole("button", { name: "复制完整标识" }));
  expect(screen.getByRole("status").textContent).toContain("手动复制");
  expect(screen.getByText("request-short").tagName).toBe("CODE");
});
