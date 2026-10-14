import { t } from "./i18n";
export interface BudgetRequest {
  contract_version: "xinsight.budget-reading.v1";
  filters: { source_ids: string[]; source_projects: string[]; cost_projects: string[] };
}
export interface BudgetReading {
  reading_id: string;
  contract_version: string;
  cutoff: string;
  expires_at: string;
  membership_ref: string;
  budget_count: number;
  calculated_budget_source_state: string;
  independent_invoice_source_state: string;
}
export type BudgetBody = Record<string, unknown>;
export interface BudgetProfile {
  state: string;
  body?: BudgetBody;
  source_hash?: string;
}
export interface BudgetItem {
  ref: string;
  budget_ref: string;
  source_system_id: string;
  source_instance: string;
  org_id: string;
  project_id: string;
  source_reading_id: string;
  source_reading_cutoff: string;
  source_observed_at: string;
  received_at: string;
  definition_revision: string;
  binding_revision?: string;
  source_state: string;
  current_retrieval_state?: string;
  definition: BudgetProfile;
  summary: BudgetProfile;
  alerts: BudgetProfile;
  alerts_page?: "first_page" | "continuation_page";
  detail_relationship: string;
  runtime_effect: string;
}
export interface BudgetItems {
  reading_id: string;
  items: BudgetItem[];
  next_cursor?: string;
}

export function budgetObject(value: unknown): BudgetBody {
  return value !== null && typeof value === "object" && !Array.isArray(value) ? value as BudgetBody : {};
}
export function budgetText(value: unknown): string { return typeof value === "string" ? value : ""; }

// No Number conversion for money or thresholds. Non-decimal scales retain
// their exact fraction; display formatting cannot change a source comparison.
export function formatBudgetMoney(value: unknown): string {
  const money = budgetObject(value);
  const minor = budgetText(money.value_minor), scale = budgetText(money.scale), currency = budgetText(money.currency);
  if (!/^(0|-?[1-9][0-9]{0,77})$/.test(minor) || !/^[1-9][0-9]{0,18}$/.test(scale) || !/^[A-Z]{3}$/.test(currency)) return t("m0100");
  const amount = BigInt(minor), denominator = BigInt(scale);
  if (!/^10*$/.test(scale)) return `${minor} / ${scale} ${currency}`;
  const absolute = amount < 0n ? -amount : amount;
  const integer = (absolute / denominator).toString().replace(/\B(?=(\d{3})+(?!\d))/g, ",");
  const remainder = absolute % denominator;
  const decimal = remainder === 0n ? "" : `.${remainder.toString().padStart(scale.length - 1, "0").replace(/0+$/, "")}`;
  return `${amount < 0n ? "−" : ""}${integer}${decimal} ${currency}`;
}

export function budgetPercentage(comparison: BudgetBody): string {
  const n = budgetText(comparison.utilization_numerator), d = budgetText(comparison.utilization_denominator);
  if (!/^(0|[1-9][0-9]{0,77})$/.test(n) || !/^[1-9][0-9]{0,77}$/.test(d)) return t("m2846");
  const numerator = BigInt(n) * 10000n, denominator = BigInt(d);
  const hundredths = numerator / denominator;
  const decimal = (hundredths % 100n).toString().padStart(2, "0").replace(/0+$/, "");
  return `${numerator % denominator === 0n ? "" : t("m2847")}${hundredths / 100n}${decimal ? `.${decimal}` : ""}%`;
}
