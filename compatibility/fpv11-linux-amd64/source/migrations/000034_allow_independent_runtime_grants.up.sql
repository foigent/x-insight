-- A runtime-only grant must not need an unrelated economic, coverage or
-- preference capability. Preserve the existing meaning of every old grant.
ALTER TABLE workbench_access_grants
  DROP CONSTRAINT workbench_access_grants_has_capability_check;
ALTER TABLE workbench_access_grants
  ADD CONSTRAINT workbench_access_grants_has_capability_check CHECK (
    allow_summary OR allow_details OR allow_coverage OR allow_preferences
    OR allow_source_runtime OR allow_source_attempt OR allow_source_capability
    OR allow_source_policy OR allow_source_destination
  );
