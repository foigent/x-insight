-- Admit a separate metadata-only resource-action version for bounded test and
-- development evidence. Existing synthetic v1 records and grants are unchanged.
DO $$
BEGIN
 IF NOT EXISTS (
  SELECT 1 FROM pg_constraint
  WHERE conrelid='public.resource_access_records'::regclass
   AND conname='resource_access_records_record_json_check'
   AND contype='c'
   AND pg_get_constraintdef(oid) LIKE '%xinsight.resource-access.internal.v1%'
   AND pg_get_constraintdef(oid) LIKE '%synthetic%'
 ) THEN
  RAISE EXCEPTION 'expected exact synthetic resource version guard is absent';
 END IF;
END $$;
ALTER TABLE resource_access_records DROP CONSTRAINT resource_access_records_record_json_check;
ALTER TABLE resource_access_records ADD CONSTRAINT resource_access_records_version_check CHECK ((
 (record_json->>'schema_version'='xinsight.resource-access.internal.v1' AND record_json->>'synthetic'='true')
 OR
 (record_json->>'schema_version'='xinsight.resource-action.v1' AND record_json->>'environment' IN ('test','development'))
) IS TRUE);

ALTER TABLE source_fact_offline_authorizations DROP CONSTRAINT source_fact_offline_authorizations_profile_check;
ALTER TABLE source_fact_offline_authorizations ADD CONSTRAINT source_fact_offline_authorizations_profile_check CHECK(profile IN (
 'unirouter.execution-context.v1','unirouter.execution-identity.v1','unirouter.cost-basis.v1',
 'unirouter.runtime-observation.v1','unirouter.attempt-observation.v1','unirouter.capability-snapshot.v1',
 'unirouter.policy-observation.v1','unirouter.destination-observation.v1',
 'xinsight.synthetic-resource-access.v1','xinsight.resource-action.v1'));
