import { t, tx, useLocale, useMessageState } from "./i18n";
import { useState } from "react";
import "./identifier.css";

export function compactIdentifier(value: string) {
  return value.length > 32 ? `${value.slice(0, 12)}…${value.slice(-8)}` : value;
}

/** Presentation only: callers must supply an already authorized identifier. */
export function Identifier({ value, label = t("m0813") }: { value: string; label?: string }) {
  useLocale();
  const [notice, setNotice] = useMessageState("");
  async function copy() {
    try {
      await navigator.clipboard.writeText(value);
      setNotice(() => t("m0814"));
    } catch {
      setNotice(() => t("m0815"));
    }
  }
  return <details className="readable-identifier">
    <summary title={value}>{label} · {compactIdentifier(value)}</summary>
    <div className="identifier-content"><code>{value}</code><button type="button" onClick={() => void copy()}>{tx("m0816", { value0: label })}</button><span role="status">{notice}</span></div>
  </details>;
}
