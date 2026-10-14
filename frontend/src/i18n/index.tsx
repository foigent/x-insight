import { Fragment, useCallback, useEffect, useState, useSyncExternalStore, type ReactNode } from "react";
import enUS from "./en-US.json";
import zhCN from "./zh-CN.json";

export type Locale = "en-US" | "zh-CN";
export type MessageKey = keyof typeof enUS;
export const localeStorageKey = "xinsight.locale.v1";
const catalogs = { "en-US": enUS, "zh-CN": zhCN };
const listeners = new Set<() => void>();

export function readLocalePreference(): Locale {
  try {
    const saved = globalThis.localStorage?.getItem(localeStorageKey);
    return saved === "zh-CN" ? "zh-CN" : "en-US";
  } catch {
    return "en-US";
  }
}

let locale = readLocalePreference();
export function getLocale(): Locale { return locale; }

function applyLocale(next: Locale) {
  if (locale === next) return;
  locale = next;
  listeners.forEach(listener => listener());
}

export function setLocale(next: Locale) {
  const valid = next === "zh-CN" ? "zh-CN" : "en-US";
  try { globalThis.localStorage?.setItem(localeStorageKey, valid); } catch { /* The current tab still supports switching. */ }
  applyLocale(valid);
}

function subscribe(listener: () => void) {
  listeners.add(listener);
  // Only installed while a view is mounted. No subscription changes data scope.
  if (listeners.size === 1) window.addEventListener("storage", handleStorage);
  return () => {
    listeners.delete(listener);
    if (!listeners.size) window.removeEventListener("storage", handleStorage);
  };
}

function handleStorage(event: StorageEvent) {
  if (event.key === localeStorageKey || event.key === null) applyLocale(readLocalePreference());
}

export function useLocale(): Locale {
  return useSyncExternalStore(subscribe, getLocale, () => "en-US");
}

/** Interpolation values are source data. They are never looked up or translated. */
export function t(key: MessageKey, values: Record<string, unknown> = {}): string {
  return catalogs[locale][key].replace(/\{(value\d+)\}/g, (token, name: string) =>
    Object.hasOwn(values, name) ? String(values[name]) : token);
}

/** Module-level label maps resolve on access, so importing a map never freezes
 * its language. Only explicitly listed UI properties are localized.
 */
export function localizedLabels<T extends object, K extends string>(fixed: T, keys: Record<K, MessageKey>): T & Record<K, string> {
  return Object.defineProperties(fixed, Object.fromEntries(Object.entries<MessageKey>(keys).map(([name, key]) =>
    [name, { enumerable: true, get: () => t(key) }]))) as T & Record<K, string>;
}

/** Keeps React elements as elements; does not parse HTML or rewrite DOM text. */
export function tx(key: MessageKey, values: Record<string, ReactNode> = {}): ReactNode {
  return catalogs[locale][key].split(/(\{value\d+\})/g).map((part, index) => {
    const name = /^\{(value\d+)\}$/.exec(part)?.[1];
    return <Fragment key={name ?? `text-${index}`}>{name && Object.hasOwn(values, name) ? values[name] : part}</Fragment>;
  });
}

export type MessageResolver = () => string;

/** Store the message recipe, so an already-visible status follows a language change.
 * The caller's event-time closure preserves its result and interpolation data.
 */
export function useMessageState(initial: string): [string, (next: string | MessageResolver) => void, MessageResolver] {
  useLocale();
  const [resolve, save] = useState<MessageResolver>(() => () => initial);
  const setMessage = useCallback((next: string | MessageResolver) => {
    save(() => typeof next === "function" ? next : () => next);
  }, []);
  return [resolve(), setMessage, resolve];
}

export class LocalizedError extends Error {
  constructor(resolve: MessageResolver) {
    super();
    Object.defineProperty(this, "message", { configurable: true, get: resolve });
  }
}

export function LanguageSelector() {
  const current = useLocale();
  useEffect(() => {
    document.documentElement.lang = current;
    document.title = t("m3055");
    document.querySelector('meta[name="description"]')?.setAttribute("content",
      t("m3056"));
  }, [current]);
  return <label className="language-selector">
    <span>{t("m3057")}</span>
    <select aria-label={t("m3057")} value={current} onChange={event => setLocale(event.target.value as Locale)}>
      <option value="en-US" lang="en-US">English</option>
      <option value="zh-CN" lang="zh-CN">简体中文</option>
    </select>
  </label>;
}
