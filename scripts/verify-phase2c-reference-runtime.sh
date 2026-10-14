#!/usr/bin/env bash
set -euo pipefail

list_production_go_files_matching() {
  local pattern="$1"
  shift
  if command -v rg >/dev/null 2>&1; then
    rg -l "${pattern}" "$@" --glob '*.go' --glob '!**/*_test.go' || true
    return
  fi
  find "$@" -type f -name '*.go' ! -name '*_test.go' -print0 |
    xargs -0 grep -l -E -- "${pattern}" || true
}

production_go_lines_match() {
  local pattern="$1"
  shift
  if command -v rg >/dev/null 2>&1; then
    rg -n "${pattern}" "$@" --glob '*.go' --glob '!**/*_test.go'
    return
  fi
  find "$@" -type f -name '*.go' ! -name '*_test.go' -print0 |
    xargs -0 grep -n -E -- "${pattern}"
}

lines_match() {
  local pattern="$1"
  shift
  if command -v rg >/dev/null 2>&1; then
    rg -n "${pattern}" "$@"
    return
  fi
  grep -n -E -- "${pattern}" "$@"
}

tree_contains() {
  local pattern="$1"
  shift
  if command -v rg >/dev/null 2>&1; then
    rg -q "${pattern}" "$@"
    return
  fi
  grep -R -q -E -- "${pattern}" "$@"
}

# Whole-file digests are advanced only after an intentional review of every
# protected diff. The rc.2 candidate adds one exact bounded-offline
# OpenInference importer, durable request binding and completed receipt while
# keeping protocol adoption and runtime activation fail-closed. It does not
# add a listener, Cursor/Checkpoint, traffic, credentials, or change either
# existing pull worker's data result. The rc.3 release-controller correction
# advances only the reviewed hostctl database digest for its bounded legacy
# credential bridge; Connector/Event/M0 semantics remain unchanged.
# The historical rc.4 fixed-snapshot correction advanced only the reviewed
# Investigation API/OpenAPI digests so execution search accepts and binds its
# existing `as_of` contract; Connector/Event/M0 and migration semantics remained
# unchanged.
# The rc.5 product slice adds a source-neutral cross-execution Trace Journey
# read model and an exact root-owned expiring non-production bounded-offline
# authorization. Shared protocol mappings and existing M0 identity/hash remain
# unchanged; no listener, Feed, Cursor or Checkpoint is introduced.
# The Posture workspace advances only the reviewed read-only OpenAPI/access/server
# surface and adds one protected query model/store path. It aggregates existing
# evidence without changing Event/M0 identity, hash, provenance, Capture Gap,
# persistence, Feed, Cursor or Checkpoint semantics.
# The rc.7 second-integration candidate adds an operator-mediated checkpoint
# epoch transition. It archives and atomically rebinds mutable delivery state
# while keeping both workers dormant and preserving every Event/M0 identity,
# hash, provenance, Capture Gap and existing Feed wire result.
# The rc.9 schema-contract candidate advances only the reviewed server manifest
# readback, lifecycle CLI/database and checkpoint control digests. DATA_RESET
# creates a strict-empty fresh database and seals the historical epoch floor in
# its receipt; a separately authorized BOOTSTRAP may then create one dormant
# state at historical max+1. Shared protocol pins, mappings, Event/M0 identity,
# provenance, Capture Gap, Feed wire results and worker activation semantics
# remain unchanged.
# The rc.10 admin-credential rotation candidate advances only the reviewed
# xinsightctl command surface for a root-only database credential lifecycle.
# It does not change protocol pins, Connector/Event/M0 semantics, delivery
# state, workers, credentials used by connectors, polling or traffic.
# The rc.11 signed campaign consumer advances only the reviewed product-local
# UniRouter evidence adapter, bounded-offline operation-store reuse and sealed
# completed-import authority paths, plus the accepted offline importer
# dispatch/usage entry. Shared protocol pins and compatibility bytes remain
# unchanged; no listener, worker, Cursor/Checkpoint, credential, polling or
# traffic path is introduced.
# Connector CP2A advances only the reviewed product-local operation/result v2
# OpenAPI, the minimum workspace grant boundary, Schema 22 phase migrations,
# and the normal-install checkpoint-epoch authority. CP2B/CP2C add only the
# reviewed worker-last composition, durable-result-before-wake API and fixed
# root Unix-controller runtime wiring. Event Connector Protocol v2.0.2 and its
# shared event wire remain unchanged.
# The target-config delivery correction advances only the reviewed server,
# lifecycle CLI and managed-service digests. It keeps TLS/external-origin-only
# deployments Connector-disabled and changes no protocol bytes or traffic.
# The rc.12 source-privilege compatibility hotfix advances only the reviewed
# hostctl database digest. Preflight validates the exact rc.10 source policy;
# journaled ApplyGrants and target-policy verification remain fail-closed, and
# no protocol, schema, migration, worker or traffic byte changes.
# The rc.13 maintenance hardening advances only the reviewed product server
# composition, API handler, hostctl database digests and additive Schema 23
# function-privilege migration. Production wiring now requires its complete
# store capabilities at compile time, readiness fails closed when migration
# readback is unavailable, and function privileges converge before the schema
# contract is accepted. No protocol, worker or traffic bytes are changed.
# The reviewed FPV9/schema40 work-chain integration advances the protected
# product API, authorization, persistence and host-control digests plus
# additive migrations 24-40. Shared protocol pins and prior migrations remain
# unchanged; every listed file and tree digest is checked below.
# The exact direct-run FPV9 upgrade adds the reviewed lifecycle CLI dispatch
# and usage entry plus read-only PostgreSQL system-ID inspection. Its source
# proof uses the existing strict Connector JSON decoder; this one new caller
# is enumerated in the importer inventory below. Existing
# protocol, Connector, Event, M0 and checkpoint paths remain unchanged.
# The reviewed FPV12/schema44 P3 slice adds managed existing-receiver setup,
# exact deployment-bound control and original-key custody/private recovery.
# Public V2 projects the existing strict UR request authority, excluding XI
# storage/lease metadata that the released consumer already rejects. V3 and
# custody-spool imports reuse strict JSON/canonical/integrity kernel functions.
# Migrations 1-43 and shared protocol pins/mappings remain byte-identical;
# only new unpublished M44 and the reviewed read-only original activation proof
# plus its test-package bridge advance the two complete tree digests.
# FPV15 advances only the reviewed lifecycle CLI digest: the existing root
# target-config command prepares exact target egress before paused auth, and
# the existing owner maintenance loop completes copied legacy C custody.
# No shared protocol, Event/M0, mapping, migration or checkpoint bytes change.
# FPV19 advances three reviewed presentation digests: the optional HTML-export
# language parameter, its authenticated handler, and the localized HTML template.
# Sealed JSON bytes/digests, authorization and all source identities are unchanged.
# The API/render regressions verify English, Chinese, fallback and denied access.
protocol_workflow=".github/workflows/foigent-protocol-conformance.yml"
expected_protocol_action='        uses: foigent/event-connector-spec/actions/source-neutral-v2@ad8fe7a38c9d7335657630baa8670dd07556b98c'
if [[ "$(grep -F -x -c -- "${expected_protocol_action}" "${protocol_workflow}" || true)" -ne 1 ]]; then
  echo "Phase 2C runtime gate: protocol workflow does not use the exact immutable v2.0.2 composite Action" >&2
  exit 1
fi
if grep -F -q -- 'repository: foigent/event-connector-spec' "${protocol_workflow}" ||
  grep -F -q -- '.foigent-event-connector-spec-' "${protocol_workflow}"; then
  echo "Phase 2C runtime gate: protocol workflow reintroduced a private authority checkout" >&2
  exit 1
fi
while read -r expected path; do
  actual="$(sha256sum "${path}" | awk '{print $1}')"
  if [[ "${actual}" != "${expected}" ]]; then
    echo "Phase 2C runtime gate: protected ${path} changed (${actual}, want ${expected})" >&2
    exit 1
  fi
  workflow_path_count="$(grep -F -x -c -- "      - ${path}" "${protocol_workflow}" || true)"
  if [[ "${workflow_path_count}" -ne 2 ]]; then
    echo "Phase 2C runtime gate: protected ${path} is not covered by both workflow triggers" >&2
    exit 1
  fi
done <<'BASELINE'
5ded68ef163872d94db83349a5c818ee4e93cee3e9715478f4d038022b421f47 api/openapi/prd2-v1.yaml
7147f90de8c5686f5575ee8ed313ed1425c7051b59143240d35a006421adb58d cmd/xinsight-launcher/main.go
130e8a83c06513a40f3e2d256e9dd0b41aa504281861025d433489f9e85ac456 cmd/xinsight-server/main.go
88bf7f65a159e173089364436f70d06fa022ec700913e57629719c65cfd8f019 cmd/xinsightctl/main.go
7a7cc7e621bc3e24f104624038010475eb498226b32455fb27fceb641d77d661 cmd/xinsight-importer/main.go
192ae40d401262e625db1769d9d22d365f53a3a5255ab72d127b8f0ffc484173 deploy/xinsight.service
7924a8b75f4f49a995227d51bfdee60b83bac43c64715e33165bb48f2492090e contracts/foigent/event-connector-protocol.pin.json
c3f99a441beb813cf2c3f25091cc61309d095993bc347ccc5f9a921f47ed14a4 contracts/openinference/shared-protocol-compatibility.v1.json
4f1173d656e350b4bf88a86ef4ed0850e186eb2514efaacfe7aff0ed0982119c contracts/openinference/shared-protocol-compatibility.v2.json
95bc661ff5fc407043823fe12e736d4ea993ae0e4be9d96218db8d68634804c0 contracts/openinference/trace-journey-study.v1.json
edb9e69baac748bce103e7b16ee477eb8e1ffc6a40939c337903cb25861cd519 contracts/openinference/xinsight-projection-binding.v2.json
3845ce975d7b0da8c8d294aa210c5ec6741ebcf1260bb5d4a635568077f3081e internal/access/accounts.go
40d82164f5bd0ee85b11b26144f81fc44f5afedda6359b3b8e9fa68c7a1a8651 internal/access/oidc.go
2735e9f736ecb3c59910e448721bdffa579cd9244b44461e173f8e040bd93980 internal/adapters/openinference/compatibility.go
e1c167f8eb41ec136beaa31964817e4493dea7202c14f9edddf246bc586b0705 internal/adapters/unirouter/completed_execution.go
811af109962686bb21ca12dd182e1e9adc3b9927bcc152359434a8d6a236250f internal/adapters/unirouter/execution_evidence_bundle.go
a3187aa368b36610a1f19227351b38ad975152dea47510a4a7c67855e055ab2a internal/api/access.go
a8b5f767701a52189b7980a1d7a711cef6d08eb926feb99a7aba579ffb6fe7fb internal/api/investigation.go
b4190c2d059ec131bc45e81dcb6de50b361b1e2b49499ae1537e39259b5d2324 internal/api/problem.go
d0c1b307bb4d9a4f7de169cf0916d0c325b0a4d4374a71ed7ce9fbaf0a0e7d69 internal/api/posture.go
07aec78c2fadf1f4ee5220b992d52ad98e7f5d9be6910b3806897a6ac537f1a2 internal/api/server.go
3f963125a30bd43e55c8a6ec2d9d9ee7f2e07a9f7fbb4dd3abcc52e3ae8db8d0 internal/api/transport_security.go
38bb2caa1d105f45ce268d8d2b724a691062e60e36411d2c3e889f29831e1e4b internal/api/workspace.go
a8b77d02b1c5b287ee1aa404b14fdbbb7d1a6b85d94ba0488fb61c43d2e27048 internal/config/postgres.go
6c6298f2fcc8e8bf12b987665b604250bac8f288a1bd99339f0bd8023a3cc5db internal/connector/operational_receipt_v2.go
b685f2a729b48cd29b77a5b0d7b006485377025cffa901116e7c7ce23efb48ff internal/db/agent_access_store.go
b232e80cbe68667b7ce18a2667b54654c320f88d71656018b49c31524b634f75 internal/db/analysis_extension_store.go
79153b36b059fc4461a842f9d028c27f5bbc7fc3e359da4a7d5dc7eb20463110 internal/db/connector_trust_store.go
34961627ef923eea45c57ce4832150cce7897e06dc5cd24b08ecf8d1e67dbd26 internal/db/discovery_store.go
d9844cd9656a0236ef9343a35314627e3cfe2456a8a8a65a964ef92ece0a149c internal/db/evidence_store.go
d11dc416deb387a21491ea9dd86bbdeab739cfac739c3ae04a3d50575d314b68 internal/db/investigation_store.go
c1050e58f6a5c90194769972777b05dcaa91f1b25509e514356193cdef13f94a internal/db/posture_store.go
be634cdc1b59cca9a5d11f2726da88bd9a27e6d6f0b8a8768dee33fb02aa2e60 internal/db/trace_journey_store.go
013a7e12596f3b0be311b92b43bc43e94f5c6df77856ec2501ad4e56f7a94b07 internal/db/migrate.go
98a3c2e2aed842f872feb5987a28627bab40e8eb191e80dd3409eca56b88d2c9 internal/db/session_store.go
09472adadf129404b511276fc4be281db48ea46e4248fe0cbb1265352e5259b7 internal/db/workspace_store.go
a961198cf1d13c2d940520bbfdf657c6629a8ad88561141e25c75b473ab807e8 internal/discovery/engine.go
16ed9fe9902e2bcade35bf476f0c6cd7698941495a5164a6a2c0319877b11955 internal/hostctl/database.go
fccad7130b2a155ff170accd20b16835fa9f820c9a1c0a36cbe5f3615491bb8c internal/hostctl/checkpoint_epoch.go
f84f1e42a6afbb9afdda7690dffda7f7a3f06abd4bf3cd76efecca832499292d internal/investigation/compare.go
6fc8ae27ad843f6baff188ff5fe62a2bd8f4fe6517ce667d56b3a41ef151b41d internal/investigation/model.go
47d36073bacefb08364d7a004f0790e1fc2c43776cc85c3cead71c8c284c8591 internal/investigation/projection.go
941a2a8b7a3df85b822d9af2b5397cbf76a2a9451b24e3510c418ea4b6fa8a92 internal/investigation/posture.go
5c2b0743efb7aa9ae5b6cdb35fea4b9899a43c09fe4874c2ab789520c59b5587 internal/integration/openinferenceoffline/evidence_committer.go
3918cd7955eae22672b12511751555dbcfb12773ffc1e1a5ff1e5bc564e3d0fd internal/integration/openinferenceoffline/authorization.go
8a5ea6e4f759ae7fc0c11d400b41f1865f6d800738b994bc298e7771ec43fb10 internal/integration/openinferenceoffline/importer.go
f5fabc7a48add889e749e56678de9b8b636755596f9995ffa3a79f56c7f951a2 internal/integration/openinferenceoffline/operation_binding.go
255da6e035e6788f821a572efc97861ab3d55a097d746da42e008e7508ebf216 internal/integration/openinferenceoffline/postgres_store.go
2c419155ff946abbda336f00d2532b07c7f53f0451f5a9c2f310f8adf3841780 internal/integration/pullfeed/feed.go
5c3df1c5b8fb156760d714c1e71860ae79417bfcc20d9d6ec63679ef226b08a4 internal/integration/pullfeed/http_source.go
77e2e4ceb4194237ef5c4f8e39e356ee4ce9f778997ff6235f58fdeba74c8985 internal/integration/pullfeed/postgres_state.go
3d4987c86c07b3820f6fb1018605ab5aec7c9517f143b507f9a36b537f8eaa8c internal/workspace/model.go
d6b94fd94e744a3732f759f00e7c9fa7ac46d173ac71f11f59244945ce3028a8 internal/workspace/pack.go
ea4443e9f641b94d9bb61dafb99766d47a8c72aef964071012bd89898ca2bae0 internal/workspace/service_identity.go
3924a2bb634c53ed4829dfaad3b1e280c7d7d9173319705b937d1b7177cb6f92 schemas/integration/openinference-bounded-offline-authorization-v1.schema.json
e83f8936eef72d4842752120601181108a4f893a1f6a4d9810ee61343b7ce479 schemas/integration/connector-checkpoint-epoch-prepare-v1.schema.json
BASELINE

if [[ "$(grep -F -x -c -- '      - migrations/**' "${protocol_workflow}" || true)" -ne 2 ]]; then
  echo "Phase 2C runtime gate: migration tree is not covered by both workflow triggers" >&2
  exit 1
fi

migration_digest="$(
  find migrations -type f -print |
    LC_ALL=C sort |
    xargs sha256sum |
    sha256sum |
    awk '{print $1}'
)"
if [[ "${migration_digest}" != "d233dc7e847ce6992179830c444716e4af9f4e19e2af9823f8f663f4b027bee9" ]]; then
  echo "Phase 2C runtime gate: migration tree changed (${migration_digest})" >&2
  exit 1
fi

if [[ "$(grep -F -x -c -- '      - internal/checkpointepoch/**' "${protocol_workflow}" || true)" -ne 2 ]]; then
  echo "Phase 2C runtime gate: checkpoint epoch package is not covered by both workflow triggers" >&2
  exit 1
fi
if [[ "$(grep -F -x -c -- '      - internal/hostctl/checkpoint_epoch_test.go' "${protocol_workflow}" || true)" -ne 2 ]]; then
  echo "Phase 2C runtime gate: checkpoint epoch host control and tests are not covered by both workflow triggers" >&2
  exit 1
fi
epoch_control_digest="$(
  {
    find internal/checkpointepoch -type f -name '*.go' -print
    printf '%s\n' \
      internal/hostctl/checkpoint_epoch.go \
      internal/hostctl/checkpoint_epoch_test.go \
      internal/integration/pullfeed/postgres_state.go \
      internal/integration/litellmfeed/postgres_state.go \
      migrations/000020_create_connector_checkpoint_epochs.up.sql \
      migrations/000020_create_connector_checkpoint_epochs.down.sql \
      schemas/integration/connector-checkpoint-epoch-prepare-v1.schema.json
  } |
    LC_ALL=C sort |
    xargs sha256sum |
    sha256sum |
    awk '{print $1}'
)"
if [[ "${epoch_control_digest}" != "684d597cf4e8ada5a1176fa325071b99e95a0e47bf0811cd18b55b144026c6ea" ]]; then
  echo "Phase 2C runtime gate: checkpoint epoch control tree changed (${epoch_control_digest})" >&2
  exit 1
fi

connector_importers="$(
  list_production_go_files_matching \
    '"github.com/foigent/x-insight/internal/connector"' \
    cmd internal |
    LC_ALL=C sort
)"
expected_connector_importers="$(
  printf '%s\n' \
    cmd/xinsightctl/query.go \
    internal/adapters/litellm/binding.go \
    internal/adapters/litellm/profile.go \
    internal/adapters/litellm/runtime.go \
    internal/adapters/litellm/source.go \
    internal/adapters/openinference/binding.go \
    internal/adapters/openinference/envelope.go \
    internal/adapters/openinference/profile.go \
    internal/adapters/openinference/runtime.go \
    internal/adapters/openinference/source.go \
    internal/adapters/unirouter/campaign_package_receipt.go \
    internal/adapters/unirouter/foigent_neutral_profile.go \
    internal/adapters/unirouter/foigent_runtime.go \
    internal/api/review_capture_admin.go \
    internal/api/review_capture_exceptions.go \
    internal/api/review_capture_ur_admin.go \
    internal/api/work_event_install.go \
    internal/connectorcontrol/control_v3.go \
    internal/db/resource_access.go \
    internal/db/resource_action_offline.go \
    internal/db/resource_offline.go \
    internal/db/source_budgets.go \
    internal/db/source_budgets_restore.go \
    internal/db/source_contents.go \
    internal/db/source_facts_managed.go \
    internal/hostctl/direct_fpv9.go \
    internal/integration/boundedoffline/operation_store.go \
    internal/integration/litellmfeed/worker.go \
    internal/integration/openinferenceoffline/evidence_committer.go \
    internal/integration/openinferenceoffline/importer.go \
    internal/integration/openinferenceoffline/operation_binding.go \
    internal/integration/openinferenceoffline/postgres_store.go \
    internal/integration/pullfeed/content_source.go \
    internal/integration/pullfeed/custody_content.go \
    internal/integration/pullfeed/reference_committer.go \
    internal/integration/uniroutercampaign/import_authority.go \
    internal/integration/uniroutercampaign/importer.go \
    internal/integration/uniroutercampaign/operation_binding.go \
    internal/sourcefacts/budget_contract.go \
    internal/sourcefacts/budget_offline.go \
    internal/sourcefacts/config.go \
    internal/sourcefacts/consumer.go \
    internal/sourcefacts/content_contract.go \
    internal/sourcefacts/content_control.go \
    internal/sourcefacts/contract.go \
    internal/sourcefacts/observation_contract.go \
    internal/sourcefacts/observation_envelope.go \
    internal/sourcefacts/offline.go \
    internal/workbench/action_witness.go \
    internal/workbench/openclaw_trajectory.go \
    internal/workbench/resource_access.go \
    internal/workevent/contract.go \
    internal/workspace/execution_context.go \
    internal/workspace/resource_basis.go |
    LC_ALL=C sort
)"
if [[ "${connector_importers}" != "${expected_connector_importers}" ]]; then
  echo "Phase 2C runtime gate: neutral kernel production import surface drifted" >&2
  printf 'actual:\n%s\nexpected:\n%s\n' "${connector_importers}" "${expected_connector_importers}" >&2
  exit 1
fi

if find internal/connector -type f -name '*.go' ! -name '*_test.go' -print0 |
  xargs -0 grep -E -i -q 'unirouter|litellm|openinference|otel|completed[-_.]?execution|spend[-_.]?log'; then
  echo "Phase 2C runtime gate: neutral kernel contains a product registration name" >&2
  exit 1
fi

openinference_runtime_importers="$(
  list_production_go_files_matching \
    '"github.com/foigent/x-insight/internal/adapters/openinference"' \
    cmd internal |
    LC_ALL=C sort
)"
expected_openinference_runtime_importers="$(
  printf '%s\n' \
    cmd/xinsight-importer/main.go \
    internal/integration/openinferenceoffline/importer.go \
    internal/integration/openinferenceoffline/postgres_store.go |
    LC_ALL=C sort
)"
if [[ "${openinference_runtime_importers}" != "${expected_openinference_runtime_importers}" ]]; then
  echo "Phase 2C runtime gate: dormant OpenInference importer surface drifted" >&2
  printf 'actual:\n%s\nexpected:\n%s\n' \
    "${openinference_runtime_importers}" \
    "${expected_openinference_runtime_importers}" >&2
  exit 1
fi

openinference_forbidden_runtime_lines="$(
  production_go_lines_match \
    'net/http|ListenAndServe|ServeTLS|grpc\.NewServer|Cursor|Checkpoint|HighWatermark' \
    internal/integration/openinferenceoffline |
    grep -F -v -- 'internal/integration/openinferenceoffline/authorization.go:' || true
)"
if [[ -n "${openinference_forbidden_runtime_lines}" ]]; then
  echo "Phase 2C runtime gate: bounded-offline OpenInference crossed listener or delivery-state boundary" >&2
  printf '%s\n' "${openinference_forbidden_runtime_lines}" >&2
  exit 1
fi

if ! grep -F -q -- 'TestOpenInferenceImportBundleFailsClosedBeforeBundleCredentialsOrDatabase' \
  cmd/xinsight-importer/main_test.go ||
  ! grep -F -q -- 'LoadBoundedOfflineAuthorization(' cmd/xinsight-importer/main.go ||
  ! grep -F -q -- 'ValidateMetadataOnlyOTLP(payload)' cmd/xinsight-importer/main.go ||
  ! grep -F -q -- 'authorization.ValidateImport(authorizedRequest)' cmd/xinsight-importer/main.go; then
  echo "Phase 2C runtime gate: OpenInference exact authorization-first command proof is missing" >&2
  exit 1
fi

if ! grep -F -q -- 'SharedProtocolCompatibilityStatus = "protocol_v2_product_pin_adopted_bounded_offline_authorization_required"' \
  internal/adapters/openinference/compatibility.go ||
  ! grep -F -q -- 'return ErrSharedProtocolActivationBlocked' \
    internal/adapters/openinference/compatibility.go ||
  ! grep -F -q -- '"status": "protocol_v2_product_pin_adopted_bounded_offline_authorization_required"' \
    contracts/openinference/shared-protocol-compatibility.v2.json ||
  ! grep -F -q -- '"activation_eligible": false' \
    contracts/openinference/shared-protocol-compatibility.v2.json ||
  ! grep -F -q -- '"bounded_offline_one_operation_eligible": true' \
    contracts/openinference/shared-protocol-compatibility.v2.json ||
  ! grep -F -q -- '"durable_bounded_import_receipt_implemented": true' \
    contracts/openinference/shared-protocol-compatibility.v2.json ||
  ! grep -F -q -- '"runtime_authorized": false' \
    contracts/openinference/shared-protocol-compatibility.v2.json ||
  ! grep -F -q -- '"network_listener": false' \
    contracts/openinference/shared-protocol-compatibility.v2.json ||
  ! grep -F -q -- '"feed_activation": false' \
    contracts/openinference/shared-protocol-compatibility.v2.json ||
  ! grep -F -q -- '"cursor_checkpoint": false' \
    contracts/openinference/shared-protocol-compatibility.v2.json; then
  echo "Phase 2C runtime gate: OpenInference v2 product adoption no longer fails closed at runtime" >&2
  exit 1
fi

projector_callers="$(
  list_production_go_files_matching \
    'NewCompletedExecutionReferenceProjector\(' \
    cmd internal |
    LC_ALL=C sort
)"
if [[ "${projector_callers}" != $'cmd/xinsight-importer/main.go\ninternal/adapters/unirouter/foigent_runtime.go' ]]; then
  echo "Phase 2C runtime gate: Reference projector caller surface drifted" >&2
  echo "${projector_callers}" >&2
  exit 1
fi

committer_callers="$(
  list_production_go_files_matching \
    'NewModeAwareEvidenceCommitter\(' \
    cmd internal |
    LC_ALL=C sort
)"
if [[ "${committer_callers}" != $'cmd/xinsight-importer/main.go\ninternal/integration/pullfeed/reference_committer.go' ]]; then
  echo "Phase 2C runtime gate: mode-aware committer caller surface drifted" >&2
  echo "${committer_callers}" >&2
  exit 1
fi

if production_go_lines_match \
  'pullfeed\.NewEvidenceCommitter\(' \
  cmd/xinsight-importer; then
  echo "Phase 2C runtime gate: production bootstrap bypasses the mode-aware committer" >&2
  exit 1
fi

if lines_match \
  'PersistEventsWithProvenance|CommitPage|PostgresStateStore|EvidenceStore' \
  internal/adapters/unirouter/foigent_runtime.go; then
  echo "Phase 2C runtime gate: projector crossed persistence or Checkpoint boundary" >&2
  exit 1
fi

for required in \
  'ReferenceMappingLegacy' \
  'ReferenceMappingShadow' \
  'ReferenceMappingNeutralCanary' \
  'CompatibilityCompared' \
  'CompatibilityEligible' \
  'reference_projection_comparison'; do
  if ! tree_contains "${required}" internal/integration/pullfeed; then
    echo "Phase 2C runtime gate: missing runtime invariant ${required}" >&2
    exit 1
  fi
done

if ! tree_contains \
  'XINSIGHT_UNIROUTER_FEED_MAPPING_MODE=legacy' \
  deploy/xinsight-feed.env.example; then
  echo "Phase 2C runtime gate: managed feed default is not explicit legacy" >&2
  exit 1
fi

echo "Phase 2C runtime gate: single writer, source-neutral worker outcome, protected read/persistence surfaces, and migration tree verified"
