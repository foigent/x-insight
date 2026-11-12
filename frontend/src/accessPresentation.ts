import type { AccessRole, AccessSessionResponse } from "./api";
import { t, type MessageKey } from "./i18n";

export function readingAccess(current: AccessSessionResponse) {
  const permissions = current.session.permissions;
  const hints = current.reading_capabilities;
  const scoped = current.scoped_data_access === true || permissions.includes("investigation.scoped.read");
  const legacyDefault = current.scoped_data_access !== true;
  const summary = permissions.includes("workbench.summary.read") && (hints?.summary ?? legacyDefault);
  const runtime = permissions.includes("workbench.summary.read") && permissions.includes("workbench.details.read") && (hints?.runtime ?? legacyDefault);
  return {
    scoped,
    summary,
    overview: summary || (!scoped && !hints && permissions.includes("evidence.read")),
    runtime,
    activity: runtime && (hints?.work_activity ?? legacyDefault),
    coverage: permissions.includes("workbench.coverage.read") && (hints?.coverage ?? legacyDefault),
  };
}

const roleKeys: Record<AccessRole, MessageKey> = {
  investigator: "session.role.investigator", operator: "session.role.operator",
  access_operator: "session.role.access_operator", platform_admin: "accounts.owner",
  workbench_reader: "session.role.workbench_reader", investigation_reader: "session.role.investigation_reader",
  workbench_investigation_reader: "accounts.reader", workbench_investigation_reviewer: "accounts.reviewer",
  workbench_purpose_reviewer: "session.role.workbench_purpose_reviewer",
};
export function roleLabel(role: AccessRole) { return t(roleKeys[role]); }
