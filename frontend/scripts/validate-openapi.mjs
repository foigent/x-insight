import SwaggerParser from "@apidevtools/swagger-parser";
import { fileURLToPath } from "node:url";

const contractPath = fileURLToPath(new URL("../../api/openapi/prd2-v1.yaml", import.meta.url));
const contract = await SwaggerParser.validate(contractPath);

if (contract.openapi !== "3.1.0") {
  throw new Error(`unexpected OpenAPI version: ${contract.openapi}`);
}

const expectedPaths = [
 "/api/v1/connector-control/installations/{installation_id}/sync",
 "/api/v1/connector-control/installations/{installation_id}/sync-once",
  "/api/v1/investigation/queue",
  "/api/v1/investigation/queue-filters",
  "/api/v1/investigation/queue-filters/{filter_id}",
  "/api/v1/access/login",
  "/api/v1/access/providers",
  "/api/v1/access/session",
  "/api/v1/access/logout",
  "/api/v1/access/sessions",
  "/api/v1/access/sessions/{session_ref}/revoke",
  "/api/v1/posture",
  "/api/v1/executions",
  "/api/v1/executions/{execution_id}",
  "/api/v1/executions/{execution_id}/investigation",
  "/api/v1/executions/{execution_id}/timeline",
  "/api/v1/executions/{execution_id}/events/{event_id}",
  "/api/v1/executions/{execution_id}/gaps",
  "/api/v1/executions/{execution_id}/feedback",
  "/api/v1/executions/{execution_id}/versions",
  "/api/v1/sources",
  "/api/v1/connectors",
  "/api/v1/connectors/invitations",
  "/api/v1/connector-setups/preview",
  "/api/v1/connector-setups",
  "/api/v1/connector-setups/resolve",
  "/api/v1/connector-setups/{installation_id}",
  "/api/v1/connector-control/installations/{installation_id}/setup",
  "/api/v1/connector-control/invitations/redeem",
  "/api/v1/connector-control/operations",
  "/api/v1/connector-control/operations/{operation_id}",
  "/api/v1/sessions",
  "/api/v1/sessions/{session_id}",
  "/api/v1/compare",
  "/api/v1/alerts",
  "/api/v1/alerts/{alert_id}/case",
  "/api/v1/discovery/hypotheses",
  "/api/v1/cases",
  "/api/v1/cases/{case_id}",
  "/api/v1/cases/{case_id}/executions",
  "/api/v1/cases/{case_id}/findings",
  "/api/v1/cases/{case_id}/findings/{finding_id}/review",
  "/api/v1/cases/{case_id}/findings/{finding_id}/review-actions",
  "/api/v1/cases/{case_id}/findings/{finding_id}/review/history",
  "/api/v1/security/assignee-candidates",
  "/api/v1/security/reviews",
  "/api/v1/security/reviews/preview",
  "/api/v1/cases/{case_id}/findings/{finding_id}/assessment",
  "/api/v1/cases/{case_id}/findings/{finding_id}/assessment/history",
  "/api/v1/cases/{case_id}/findings/{finding_id}/assessment/preview",
  "/api/v1/cases/{case_id}/findings/{finding_id}/assessment-actions",
  "/api/v1/cases/{case_id}/findings/{finding_id}/continuations",
  "/api/v1/cases/{case_id}/findings/{finding_id}/continuations/preview",
  "/api/v1/cases/{case_id}/evidence",
  "/api/v1/cases/{case_id}/gaps",
  "/api/v1/cases/{case_id}/annotations",
  "/api/v1/cases/{case_id}/seal",
  "/api/v1/cases/{case_id}/evidence-pack.json",
  "/api/v1/cases/{case_id}/evidence-pack.html",
  "/api/v1/saved-views",
  "/api/v1/service-identities",
  "/api/v1/service-identities/{service_identity_id}/revoke",
  "/api/v1/gaps/{gap_id}/resolution",
  "/api/v1/workbench/readings",
  "/api/v1/workbench/readings/{reading_id}",
  "/api/v1/workbench/readings/{reading_id}/summary",
  "/api/v1/workbench/readings/{reading_id}/source-facts",
  "/api/v1/workbench/readings/{reading_id}/contributions",
  "/api/v1/workbench/readings/{reading_id}/details",
  "/api/v1/workbench/readings/{reading_id}/recent-requests",
  "/api/v1/workbench/readings/{reading_id}/series",
  "/api/v1/workbench/readings/{reading_id}/sources",
  "/api/v1/workbench/saved-filters",
  "/api/v1/workbench/saved-filters/{saved_filter_id}",
  "/api/v1/runtime/readings",
  "/api/v1/runtime/readings/{reading_id}",
  "/api/v1/runtime/readings/{reading_id}/requests",
  "/api/v1/runtime/readings/{reading_id}/sources",
  "/api/v1/runtime/readings/{reading_id}/requests/{request_ref}",
  "/api/v1/runtime/readings/{reading_id}/requests/{request_ref}/associations",
  "/api/v1/runtime/readings/{reading_id}/requests/{request_ref}/resources",
  "/api/v1/runtime/readings/{reading_id}/requests/{request_ref}/contents",
  "/api/v1/runtime/readings/{reading_id}/requests/{request_ref}/contents/{content_ref}",
  "/api/v1/runtime/readings/{reading_id}/executions/{execution_id}",
  "/api/v1/runtime/readings/{reading_id}/observations",
  "/api/v1/runtime/readings/{reading_id}/runtime",
  "/api/v1/runtime/readings/{reading_id}/attempts",
  "/api/v1/runtime/readings/{reading_id}/capabilities",
  "/api/v1/runtime/readings/{reading_id}/policies",
  "/api/v1/runtime/readings/{reading_id}/destinations",
  "/api/v1/runtime/readings/{reading_id}/measurements",
  "/api/v1/runtime/readings/{reading_id}/summary",
  "/api/v1/runtime/readings/{reading_id}/dimensions/{dimension}",
  "/api/v1/runtime/current",
  "/api/v1/work-event-ingest/config",
  "/api/v1/work-event-ingest/items",
  "/api/v1/work-event-ingest/contents",
  "/api/v1/work-activity/scopes",
  "/api/v1/work-activity",
  "/api/v1/work-activity/summary",
  "/api/v1/work-activity/events/{source_id}/{source_instance}/{org_id}/{project_id}/{event_ref}",
  "/api/v1/work-activity/contents/{content_ref}",
];

for (const path of expectedPaths) {
  if (!contract.paths?.[path]) {
    throw new Error(`missing required PRD2 path: ${path}`);
  }
}

for (const schemaName of [
 "ConnectorSyncStatus","ConnectorSyncSlot","ConnectorSyncRun","ConnectorSyncMutation",
  "InvestigationQueueFilters", "InvestigationQueueItem", "InvestigationQueueStatistics", "InvestigationQueuePage", "InvestigationQueueFilterCommand", "InvestigationQueueFilter", "InvestigationQueueFilterList",
  "FindingResponsibility",
  "FindingResponsibilityCommand",
  "FindingResponsibilityAction",
  "FindingResponsibilityHistory",
  "FindingResponsibilityReview",
  "FindingAssessment",
  "FindingAssessmentCommand",
  "FindingAssessmentAction",
  "FindingAssessmentHistory",
  "FindingAssessmentView",
  "ReviewBasis",
  "ReviewCreationCommand",
  "ReviewCreationReceipt",
  "WorkbenchReading",
  "WorkbenchSummary",
  "WorkbenchContributions",
  "WorkbenchDetails",
  "OverviewRecentRuntime",
  "OverviewRecentRequest",
  "OverviewRecentRequests",
  "WorkbenchSeries",
  "WorkbenchSourceStatuses",
  "WorkbenchSavedFilter",
  "WorkbenchSavedFilterList",
  "RuntimeReadingRequest",
  "RuntimeReading",
  "RuntimeCoverage",
  "RuntimeStreamObservation",
  "RuntimeRequest",
  "RuntimeRequests",
  "RuntimeObservation",
  "RuntimeObservations",
  "RuntimeMeasurementExclusions",
  "RuntimePercentiles",
  "RuntimeMeasurementGroup",
  "RuntimeMeasurements",
  "RuntimeSummary",
  "RuntimeDimensionGroup",
  "RuntimeDimensions",
  "RuntimeAssociations",
  "ResourceAccessRecord",
  "RuntimeResourceItem",
  "RuntimeResources",
  "RuntimeExecutionRequest",
  "RuntimeEconomicLink",
  "RuntimeCurrentItem",
  "RuntimeCurrentInventory",
  "RuntimeOutcomeCount",
  "RuntimeDiagnosticFailure",
  "WorkActivitySummary",
  "WorkEventReviewBodyMetadata",
  "WorkEventReviewBodyReceipt",
  "WorkActivityDetail",
]) {
  const schema = contract.components?.schemas?.[schemaName];
  if (!schema || schema.type !== "object" || schema.additionalProperties !== false || !Array.isArray(schema.required)) {
    throw new Error(`response DTO is not closed and required-field explicit: ${schemaName}`);
  }
}

for (const path of ["/api/v1/work-event-ingest/config", "/api/v1/work-event-ingest/items", "/api/v1/work-event-ingest/contents"]) {
  const operation = contract.paths[path].get ?? contract.paths[path].post;
  if (operation.security?.length !== 1 || !Object.hasOwn(operation.security[0], "serviceBearer")) {
    throw new Error(`work event ingest requires exact service Bearer security: ${path}`);
  }
}

const bodyRead = contract.paths["/api/v1/work-activity/contents/{content_ref}"].get;
if (!bodyRead.parameters?.some((parameter) => parameter.name === "X-XInsight-Scope-Revision" && parameter.required) ||
    !bodyRead.responses?.["200"]?.content?.["application/octet-stream"] ||
    !bodyRead.responses?.["200"]?.headers?.["X-XInsight-Content-SHA256"] ||
    !bodyRead.responses?.["409"]) {
  throw new Error("review body read must declare current scope, exact bytes, digest, and scope conflict");
}

for (const field of ["review_bodies", "run_items", "run_items_more"]) {
  if (!contract.components.schemas.WorkActivityDetail.required.includes(field)) {
    throw new Error(`work activity detail lost ${field}`);
  }
}

const associationVariants = contract.components?.schemas?.RuntimeAssociation?.oneOf;
if (!Array.isArray(associationVariants) || associationVariants.length !== 2 || associationVariants.some(v => v.type !== "object" || v.additionalProperties !== false || !v.required?.includes("kind") || v.required.length !== 2)) {
  throw new Error("runtime association variants must remain closed and explicit");
}

const contributionRequired = contract.components?.schemas?.WorkbenchContributions?.required ?? [];
for (const field of ["mode", "unassigned_scope", "denominator_micros", "authorized_group_count"]) {
  if (!contributionRequired.includes(field)) {
    throw new Error(`workbench contribution DTO lost ${field}`);
  }
}

const coverageRequired = contract.components?.schemas?.WorkbenchCoverage?.required ?? [];
for (const field of ["period_completeness", "projection_complete_for_members", "known_items", "gaps"]) {
  if (!coverageRequired.includes(field)) {
    throw new Error(`workbench coverage DTO lost ${field}`);
  }
}

if (contract.components?.securitySchemes?.sessionCookie?.name !== "__Host-xinsight_session") {
  throw new Error("missing exact host-only Session cookie security scheme");
}

if (
  contract.components?.securitySchemes?.serviceBearer?.type !== "http" ||
  contract.components?.securitySchemes?.serviceBearer?.scheme !== "bearer"
) {
  throw new Error("missing read-only service bearer security scheme");
}

for (const path of [
  "/api/v1/executions",
  "/api/v1/executions/{execution_id}",
  "/api/v1/executions/{execution_id}/investigation",
  "/api/v1/executions/{execution_id}/timeline",
  "/api/v1/executions/{execution_id}/events/{event_id}",
  "/api/v1/executions/{execution_id}/gaps",
  "/api/v1/sources",
]) {
  const security = contract.paths?.[path]?.get?.security ?? [];
  if (
    !security.some((entry) => Object.hasOwn(entry, "sessionCookie")) ||
    !security.some((entry) => Object.hasOwn(entry, "serviceBearer"))
  ) {
    throw new Error(`human/Agent same-truth security is incomplete for ${path}`);
  }
}

console.log(`validated ${contract.info.title} with ${expectedPaths.length} required paths`);

// Observation metadata extends the existing C route; immutable refs remain unchanged.
const contentSchemas=contract.components.schemas;
if(contentSchemas.FrozenContentReference.properties.observation || contentSchemas.RuntimeContentItem.required.includes("observation") || !contentSchemas.RuntimeContentItem.properties.observation) throw new Error("content observation changed the frozen/legacy reference contract");
const observation=contentSchemas.ContentObservation;
for(const field of ["protocol","observation_point","content_item_id","occurred_at","capture_reason"]) if(!observation.required.includes(field)) throw new Error(`missing observation field: ${field}`);
if(observation.required.includes("attempt_id") || observation.allOf.some(rule=>rule.else)) throw new Error("client attempt compatibility narrowed");

const resolvedContent=contentSchemas.ReviewResolvedExecutionContext;
if(resolvedContent.required.includes("contents") || resolvedContent.properties.resources.minItems || !resolvedContent.properties.contents || resolvedContent.anyOf.length!==2) throw new Error("optional content execution origin compatibility changed");
for(const name of ["ReviewBasisSelectionInput","ReviewBasisSelection","ReviewKnownReferences"]) if(!contentSchemas[name].properties.contents) throw new Error(`missing content references in ${name}`);
if(!contentSchemas.ReviewBasis.properties.schema.enum.includes("xinsight.review-basis.v3") || !contentSchemas.CaseRecord.properties.contents || !contentSchemas.EvidencePack.properties.schema_version.enum.includes("xinsight.evidence-pack.v5")) throw new Error("content review/history wire contract incomplete");
