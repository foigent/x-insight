-- Keep the existing root-managed authorization, revisions, audit and restore pause.
-- Only the explicit offline internal synthetic profile is added; live UR profiles stay closed.
ALTER TABLE source_fact_offline_authorizations DROP CONSTRAINT source_fact_offline_authorizations_profile_check;
ALTER TABLE source_fact_offline_authorizations ADD CONSTRAINT source_fact_offline_authorizations_profile_check CHECK(profile IN (
 'unirouter.execution-context.v1','unirouter.execution-identity.v1','unirouter.cost-basis.v1',
 'unirouter.runtime-observation.v1','unirouter.attempt-observation.v1','unirouter.capability-snapshot.v1',
 'unirouter.policy-observation.v1','unirouter.destination-observation.v1','xinsight.synthetic-resource-access.v1'));

-- Expose only the immutable cluster identity to the importer, not the broader
-- pg_control_system record or monitoring role. No caller-supplied SQL/objects.
CREATE FUNCTION synthetic_resource_cluster_id() RETURNS text
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog AS $$
 SELECT system_identifier::text FROM pg_catalog.pg_control_system()
$$;
REVOKE ALL ON FUNCTION synthetic_resource_cluster_id() FROM PUBLIC;
