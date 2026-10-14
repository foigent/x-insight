import { t, useLocale } from "./i18n";
import type { ExecutionView } from "./executionReading";

export function ViewTabs({
  active,
  onChange, contents = false, contentOnly = false,
}: {
  contents?: boolean; contentOnly?: boolean; active: ExecutionView;
  onChange: (view: ExecutionView) => void;
}) {
  useLocale();
  const tabs = [
    ["story", t("m0436"), t("m0437")],
    ["graph", t("m0438"), t("m0439")],
    ["chronology", t("m0440"), t("m0441")],
    ["proof", t("m0442"), t("m0443")],
    ["systems", t("m0444"), t("m0445")],
  ] as const;
  const visible: readonly (readonly [ExecutionView,string,string])[] = [...(contentOnly?[]:tabs),...(contents?[["contents",t("m0383"),t("m0446")] as const]:[])];
  return (
    <nav className="execution-view-tabs" aria-label={t("m0447")}>
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
