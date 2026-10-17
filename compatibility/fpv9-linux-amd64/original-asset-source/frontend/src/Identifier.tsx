import { useState } from "react";
import "./identifier.css";

export function compactIdentifier(value: string) {
  return value.length > 32 ? `${value.slice(0, 12)}…${value.slice(-8)}` : value;
}

/** Presentation only: callers must supply an already authorized identifier. */
export function Identifier({ value, label = "标识" }: { value: string; label?: string }) {
  const [notice, setNotice] = useState("");
  async function copy() {
    try {
      await navigator.clipboard.writeText(value);
      setNotice("已复制");
    } catch {
      setNotice("复制不可用，请选择完整标识手动复制。");
    }
  }
  return <details className="readable-identifier">
    <summary title={value}>{label} · {compactIdentifier(value)}</summary>
    <div className="identifier-content"><code>{value}</code><button type="button" onClick={() => void copy()}>复制完整{label}</button><span role="status">{notice}</span></div>
  </details>;
}
