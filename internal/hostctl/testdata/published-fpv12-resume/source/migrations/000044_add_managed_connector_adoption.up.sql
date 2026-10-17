-- Add managed v3 while preserving the frozen v1/v2 and original checkpoint authority.
CREATE TABLE connector_installation_source_bindings (
 installation_id text PRIMARY KEY REFERENCES connector_installations(id) ON DELETE RESTRICT,
 bootstrap jsonb, bootstrap_sha256 text, redeem_request_sha256 text,
 descriptor jsonb, descriptor_sha256 text,
 source_binding_sha256 text NOT NULL CHECK(source_binding_sha256 ~ '^sha256:[a-f0-9]{64}$'),
 CHECK((bootstrap IS NULL AND bootstrap_sha256 IS NULL AND redeem_request_sha256 IS NULL)
 OR (bootstrap IS NOT NULL AND jsonb_typeof(bootstrap)='object'
 AND bootstrap_sha256 IS NOT NULL AND bootstrap_sha256 ~ '^sha256:[a-f0-9]{64}$'
 AND redeem_request_sha256 IS NOT NULL AND redeem_request_sha256 ~ '^sha256:[a-f0-9]{64}$')),
 CHECK((descriptor IS NULL AND descriptor_sha256 IS NULL) OR
 (bootstrap IS NULL AND descriptor IS NOT NULL AND jsonb_typeof(descriptor)='object'
 AND descriptor_sha256 IS NOT NULL AND descriptor_sha256 ~ '^sha256:[a-f0-9]{64}$'))
);
REVOKE ALL ON connector_installation_source_bindings FROM PUBLIC;
DO $$ BEGIN IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='xinsight_workspace') THEN GRANT SELECT ON connector_installation_source_bindings TO xinsight_workspace; END IF; END $$;
CREATE FUNCTION public.xinsight_record_connector_source_binding(p_installation text,p_acknowledgement text)
RETURNS void LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE v_ack jsonb; v_stored text; v_source text;
BEGIN
 v_ack:=p_acknowledgement::jsonb;
 SELECT installation_acknowledgement_sha256 INTO v_stored FROM public.connector_installations
  WHERE id=p_installation FOR UPDATE;
 IF v_stored IS NULL OR v_stored <> 'sha256:'||encode(public.digest(convert_to(p_acknowledgement,'UTF8'),'sha256'),'hex')
  OR v_ack->>'installation_id' IS DISTINCT FROM p_installation
  OR v_ack->>'consent_sha256' !~ '^sha256:[a-f0-9]{64}$' THEN
  RAISE EXCEPTION 'source binding acknowledgement conflicts' USING ERRCODE='check_violation';
 END IF;
 v_source:=v_ack->>'consent_sha256';
 INSERT INTO public.connector_installation_source_bindings(installation_id,source_binding_sha256) VALUES(p_installation,v_source) ON CONFLICT DO NOTHING;
 IF NOT EXISTS(SELECT 1 FROM public.connector_installation_source_bindings
  WHERE installation_id=p_installation AND source_binding_sha256=v_source) THEN
  RAISE EXCEPTION 'source binding is immutable' USING ERRCODE='check_violation';
 END IF;
END $$;
REVOKE ALL ON FUNCTION public.xinsight_record_connector_source_binding(text,text) FROM PUBLIC;
DO $$ BEGIN IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='xinsight_workspace') THEN GRANT EXECUTE ON FUNCTION public.xinsight_record_connector_source_binding(text,text) TO xinsight_workspace; END IF; END $$;
CREATE TABLE connector_receiver_adoptions (
 installation_id text PRIMARY KEY REFERENCES connector_installations(id) ON DELETE RESTRICT,
 mode text NOT NULL CHECK(mode='adopt_existing'),
 candidate_sha256 text NOT NULL CHECK(candidate_sha256 ~ '^sha256:[a-f0-9]{64}$'),
 checkpoint_state_id uuid NOT NULL REFERENCES adapter_import_states(id) ON DELETE RESTRICT,
 consumer_epoch bigint NOT NULL CHECK(consumer_epoch>0),
 epoch_operation_id text NOT NULL CHECK(epoch_operation_id ~ '^[a-f0-9]{32}$'),
 status text NOT NULL CHECK(status IN ('selected','bound','confirmed')),
 bind_operation_id text,
 bind_result_sha256 text,
 local_receipt jsonb,
 local_receipt_sha256 text,
 confirm_operation_id text,
 confirm_result_sha256 text,
 created_at timestamptz NOT NULL,
 updated_at timestamptz NOT NULL,
 CHECK((status='selected' AND bind_operation_id IS NULL AND bind_result_sha256 IS NULL AND local_receipt IS NULL AND local_receipt_sha256 IS NULL)
   OR (status IN ('bound','confirmed') AND bind_operation_id IS NOT NULL
     AND bind_result_sha256 IS NOT NULL AND local_receipt IS NOT NULL AND local_receipt_sha256 IS NOT NULL
     AND bind_result_sha256 ~ '^sha256:[a-f0-9]{64}$' AND jsonb_typeof(local_receipt)='object'
     AND local_receipt_sha256 ~ '^sha256:[a-f0-9]{64}$')),
 CHECK((status='confirmed' AND confirm_operation_id IS NOT NULL AND confirm_result_sha256 IS NOT NULL AND confirm_result_sha256 ~ '^sha256:[a-f0-9]{64}$')
   OR (status<>'confirmed' AND confirm_operation_id IS NULL AND confirm_result_sha256 IS NULL))
);
REVOKE ALL ON connector_receiver_adoptions FROM PUBLIC;
DO $$ BEGIN IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='xinsight_workspace') THEN GRANT SELECT ON connector_receiver_adoptions TO xinsight_workspace; END IF; END $$;

CREATE TABLE connector_source_enrollments (
 installation_id text NOT NULL REFERENCES connector_installations(id) ON DELETE RESTRICT,
 enrollment_id text NOT NULL,
 source_instance text NOT NULL,
 org_id text NOT NULL,
 project_id text NOT NULL,
 revision text NOT NULL,
 configuration_sha256 text,
 content_collection_id uuid REFERENCES source_content_collections(id) ON DELETE RESTRICT,
 legacy_c_copy_pending boolean NOT NULL DEFAULT false,
 status text NOT NULL CHECK(status IN ('pending','active','revoked','requires_migration')),
 updated_at timestamptz NOT NULL,
 PRIMARY KEY(installation_id,enrollment_id,project_id),
 UNIQUE(content_collection_id),
 CHECK(NOT legacy_c_copy_pending OR (content_collection_id IS NOT NULL AND status IN ('requires_migration','revoked')))
);
REVOKE ALL ON connector_source_enrollments FROM PUBLIC;
DO $$ BEGIN IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='xinsight_workspace') THEN GRANT SELECT ON connector_source_enrollments TO xinsight_workspace; END IF; END $$;

ALTER TABLE connector_control_operations
 DROP CONSTRAINT connector_control_operations_kind_check,
 DROP CONSTRAINT connector_control_operations_phase_check,
 DROP CONSTRAINT connector_control_operations_predecessor_shape_check,
 DROP CONSTRAINT connector_control_operations_result_check;
ALTER TABLE connector_control_operations
 ADD CONSTRAINT connector_control_operations_kind_check CHECK(operation_kind IN
 ('connect','pause','resume','rotate','replay','adopt_existing_receiver','pair_machine')),
 ADD CONSTRAINT connector_control_operations_phase_check CHECK(phase IS NULL OR phase IN
 ('prepare_paused_source','register_receiver','resume_source','bind_existing_receiver','confirm_existing_receiver','deliver_machine_candidate')),
 ADD CONSTRAINT connector_control_operations_predecessor_shape_check CHECK(
 (phase IS NULL AND predecessor_operation_id IS NULL AND predecessor_result_sha256 IS NULL)
 OR (phase IN ('prepare_paused_source','bind_existing_receiver') AND predecessor_operation_id IS NULL AND predecessor_result_sha256 IS NULL)
 OR (phase IN ('register_receiver','resume_source','confirm_existing_receiver','deliver_machine_candidate')
  AND predecessor_operation_id IS NOT NULL AND predecessor_result_sha256 IS NOT NULL
  AND predecessor_operation_id ~ '^xco_[A-Za-z0-9_-]{22}$' AND predecessor_result_sha256 ~ '^sha256:[a-f0-9]{64}$')),
 ADD CONSTRAINT connector_control_operations_result_check CHECK(
 (status IN ('pending','running') AND result_sha256 IS NULL AND result_payload IS NULL AND reason_code IS NULL AND completed_at IS NULL)
 OR (status IN ('completed','failed') AND result_sha256 IS NOT NULL AND result_payload IS NOT NULL
  AND result_sha256 ~ '^sha256:[a-f0-9]{64}$'
  AND jsonb_typeof(result_payload)='object' AND octet_length(result_payload::text)<=16384 AND completed_at IS NOT NULL
  AND ((status='completed' AND reason_code IS NULL)
  OR (status='failed' AND ((request_payload->>'schema_version' IN ('xinsight.connector-control-operation.v1','xinsight.connector-control-operation.v2')
     AND reason_code IN ('authorization_denied','scope_rejected','source_unavailable','permission_denied','source_identity_invalid','transport_unavailable','contract_invalid','internal_error'))
   OR (request_payload->>'schema_version'='xinsight.connector-control-operation.v3'
     AND ((operation_kind='adopt_existing_receiver' AND reason_code IN ('adoption_identity_conflict','adoption_binding_conflict','receiver_read_unavailable','installation_authority_changed'))
       OR (operation_kind='pair_machine' AND reason_code IN ('machine_custody_unavailable','machine_purpose_conflict','installation_authority_changed')))))))));
DROP INDEX connector_control_operations_installation_phase_unique;
CREATE UNIQUE INDEX connector_control_operations_installation_phase_unique
 ON connector_control_operations(installation_id,phase) WHERE phase IS NOT NULL AND phase<>'deliver_machine_candidate';
CREATE UNIQUE INDEX connector_control_machine_identity_unique ON connector_control_operations
 (installation_id,(request_payload->'request'->'registration'->>'purpose'),(request_payload->'request'->'registration'->>'operation_id'))
 WHERE phase='deliver_machine_candidate';

CREATE OR REPLACE FUNCTION public.xinsight_validate_connector_control_phase()
RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog
AS $$
DECLARE
  v_predecessor_phase text;
  v_predecessor_status text;
  v_predecessor_result_sha256 text;
  v_predecessor_completed_at timestamptz;
  v_expected_predecessor_phase text;
  v_expected_desired_state text;
BEGIN
  IF TG_OP = 'UPDATE' AND (
    NEW.operation_id IS DISTINCT FROM OLD.operation_id
    OR NEW.installation_id IS DISTINCT FROM OLD.installation_id
    OR NEW.operation_kind IS DISTINCT FROM OLD.operation_kind
    OR NEW.phase IS DISTINCT FROM OLD.phase
    OR NEW.desired_state IS DISTINCT FROM OLD.desired_state
    OR NEW.predecessor_operation_id IS DISTINCT FROM OLD.predecessor_operation_id
    OR NEW.predecessor_result_sha256 IS DISTINCT FROM OLD.predecessor_result_sha256
    OR NEW.request_sha256 IS DISTINCT FROM OLD.request_sha256
    OR NEW.request_payload IS DISTINCT FROM OLD.request_payload
    OR NEW.created_at IS DISTINCT FROM OLD.created_at
  ) THEN
    RAISE EXCEPTION 'connector control operation request identity is immutable'
      USING ERRCODE = 'check_violation';
  END IF;

  IF NEW.request_payload ->> 'schema_version' = 'xinsight.connector-control-operation.v3' THEN
    IF NEW.request_payload ->> 'operation_id' IS DISTINCT FROM NEW.operation_id
      OR NEW.request_payload ->> 'installation_id' IS DISTINCT FROM NEW.installation_id
      OR NEW.request_payload ->> 'kind' IS DISTINCT FROM NEW.operation_kind
      OR NEW.request_payload ->> 'phase' IS DISTINCT FROM NEW.phase
      OR NEW.phase IS NULL
      OR NEW.desired_state IS DISTINCT FROM 'active'
      OR NEW.request_payload ->> 'desired_state' IS DISTINCT FROM NEW.desired_state
      OR jsonb_typeof(NEW.request_payload -> 'request') IS DISTINCT FROM 'object' THEN
      RAISE EXCEPTION 'managed connector request identity conflicts' USING ERRCODE='check_violation';
    END IF;
    IF NEW.phase='bind_existing_receiver' THEN
      IF NEW.operation_kind <> 'adopt_existing_receiver'
        OR NEW.predecessor_operation_id IS NOT NULL
        OR jsonb_typeof(NEW.request_payload->'request'->'candidate') IS DISTINCT FROM 'object'
        OR NOT EXISTS(SELECT 1 FROM public.connector_receiver_adoptions a
          WHERE a.installation_id=NEW.installation_id AND a.mode='adopt_existing'
            AND a.candidate_sha256=NEW.request_payload->'request'->>'candidate_sha256') THEN
        RAISE EXCEPTION 'managed adoption requires explicit local choice' USING ERRCODE='check_violation';
      END IF;
    ELSE
      SELECT phase,status,result_sha256,completed_at INTO v_predecessor_phase,
        v_predecessor_status,v_predecessor_result_sha256,v_predecessor_completed_at
      FROM public.connector_control_operations WHERE installation_id=NEW.installation_id
        AND operation_id=NEW.predecessor_operation_id;
      IF v_predecessor_status IS DISTINCT FROM 'completed'
        OR v_predecessor_result_sha256 IS DISTINCT FROM NEW.predecessor_result_sha256
        OR v_predecessor_completed_at IS NULL
        OR NEW.created_at < v_predecessor_completed_at
        OR (NEW.phase='confirm_existing_receiver' AND
          (NEW.operation_kind<>'adopt_existing_receiver' OR v_predecessor_phase IS DISTINCT FROM 'bind_existing_receiver'))
        OR (NEW.phase='deliver_machine_candidate' AND
          (NEW.operation_kind<>'pair_machine' OR (v_predecessor_phase IS NULL OR v_predecessor_phase NOT IN ('resume_source','confirm_existing_receiver'))))
        OR NEW.phase NOT IN ('confirm_existing_receiver','deliver_machine_candidate') THEN
        RAISE EXCEPTION 'managed connector predecessor conflicts' USING ERRCODE='check_violation';
      END IF;
    END IF;
    IF NEW.predecessor_operation_id IS DISTINCT FROM NEW.request_payload->>'predecessor_operation_id'
      OR NEW.predecessor_result_sha256 IS DISTINCT FROM NEW.request_payload->>'predecessor_result_sha256' THEN
      RAISE EXCEPTION 'managed predecessor differs from request' USING ERRCODE='check_violation';
    END IF;
    IF NEW.phase='deliver_machine_candidate' AND (
      jsonb_typeof(NEW.request_payload->'request'->'registration') IS DISTINCT FROM 'object'
      OR jsonb_typeof(NEW.request_payload->'request'->'custody_receipt') IS DISTINCT FROM 'object'
      OR COALESCE(NEW.request_payload->'request'->>'source_binding_sha256','') !~ '^sha256:[a-f0-9]{64}$'
      OR COALESCE(NEW.request_payload->'request'->>'registration_sha256','') !~ '^sha256:[a-f0-9]{64}$'
      OR COALESCE(NEW.request_payload->'request'->>'custody_receipt_sha256','') !~ '^sha256:[a-f0-9]{64}$'
      OR
      NEW.request_payload->'request'->'registration'->>'installation_id' IS DISTINCT FROM NEW.installation_id
      OR COALESCE(NEW.request_payload->'request'->'registration'->>'purpose','') NOT IN ('xinsight.policy.manage','xinsight.source.enroll')
      OR COALESCE(NEW.request_payload->'request'->'registration'->>'operation_id','') !~ '^[a-f0-9]{32}$'
      OR NEW.request_payload->'request'->'custody_receipt'->>'purpose' IS DISTINCT FROM NEW.request_payload->'request'->'registration'->>'purpose'
      OR NEW.request_payload->'request'->'custody_receipt'->>'operation_id' IS DISTINCT FROM NEW.request_payload->'request'->'registration'->>'operation_id') THEN
      RAISE EXCEPTION 'machine candidate identity is invalid' USING ERRCODE='check_violation';
    END IF;
    IF NEW.phase='confirm_existing_receiver' AND (
      jsonb_typeof(NEW.request_payload->'request'->'xi_adoption_receipt') IS DISTINCT FROM 'object'
      OR jsonb_typeof(NEW.request_payload->'request'->'feed_read_receipt') IS DISTINCT FROM 'object'
      OR COALESCE(NEW.request_payload->'request'->>'adoption_binding_sha256','') !~ '^sha256:[a-f0-9]{64}$'
      OR COALESCE(NEW.request_payload->'request'->>'xi_adoption_receipt_sha256','') !~ '^sha256:[a-f0-9]{64}$'
      OR COALESCE(NEW.request_payload->'request'->>'feed_read_receipt_sha256','') !~ '^sha256:[a-f0-9]{64}$') THEN
      RAISE EXCEPTION 'managed confirm receipts are invalid' USING ERRCODE='check_violation';
    END IF;
    RETURN NEW;
  END IF;

  IF NEW.phase IS NULL THEN
    IF TG_OP = 'INSERT'
      OR NEW.predecessor_operation_id IS NOT NULL
      OR NEW.predecessor_result_sha256 IS NOT NULL
      OR NEW.request_payload ->> 'schema_version' IS DISTINCT FROM 'xinsight.connector-control-operation.v1' THEN
      RAISE EXCEPTION 'legacy connector control operations cannot be created or rebound'
        USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
  END IF;

  IF jsonb_typeof(NEW.request_payload) IS DISTINCT FROM 'object'
    OR jsonb_typeof(NEW.request_payload -> 'request') IS DISTINCT FROM 'object'
    OR NEW.operation_kind IS DISTINCT FROM 'connect'
    OR NEW.request_payload ->> 'schema_version' IS DISTINCT FROM 'xinsight.connector-control-operation.v2'
    OR NEW.request_payload ->> 'operation_id' IS DISTINCT FROM NEW.operation_id
    OR NEW.request_payload ->> 'installation_id' IS DISTINCT FROM NEW.installation_id
    OR NEW.request_payload ->> 'kind' IS DISTINCT FROM NEW.operation_kind
    OR NEW.request_payload ->> 'phase' IS DISTINCT FROM NEW.phase
    OR NEW.request_payload ->> 'desired_state' IS DISTINCT FROM NEW.desired_state THEN
    RAISE EXCEPTION 'connector control phase differs from its canonical request'
      USING ERRCODE = 'check_violation';
  END IF;

  CASE NEW.phase
    WHEN 'prepare_paused_source' THEN
      v_expected_desired_state := 'active';
      IF NEW.predecessor_operation_id IS NOT NULL
        OR NEW.predecessor_result_sha256 IS NOT NULL
        OR NEW.request_payload ? 'predecessor_operation_id'
        OR NEW.request_payload ? 'predecessor_result_sha256' THEN
        RAISE EXCEPTION 'initial connector control phase cannot bind a predecessor'
          USING ERRCODE = 'check_violation';
      END IF;
    WHEN 'register_receiver' THEN
      v_expected_desired_state := 'active';
      v_expected_predecessor_phase := 'prepare_paused_source';
    WHEN 'resume_source' THEN
      v_expected_desired_state := 'active';
      v_expected_predecessor_phase := 'register_receiver';
    ELSE
      RAISE EXCEPTION 'connector control phase is unsupported'
        USING ERRCODE = 'check_violation';
  END CASE;

  IF NEW.desired_state IS DISTINCT FROM v_expected_desired_state THEN
    RAISE EXCEPTION 'connector control phase has an invalid desired state'
      USING ERRCODE = 'check_violation';
  END IF;

  IF v_expected_predecessor_phase IS NOT NULL THEN
    IF NEW.request_payload ->> 'predecessor_operation_id' IS DISTINCT FROM NEW.predecessor_operation_id
      OR NEW.request_payload ->> 'predecessor_result_sha256' IS DISTINCT FROM NEW.predecessor_result_sha256 THEN
      RAISE EXCEPTION 'connector control predecessor differs from its canonical request'
        USING ERRCODE = 'check_violation';
    END IF;

    SELECT phase, status, result_sha256, completed_at
    INTO v_predecessor_phase, v_predecessor_status, v_predecessor_result_sha256,
         v_predecessor_completed_at
    FROM public.connector_control_operations
    WHERE installation_id = NEW.installation_id
      AND operation_id = NEW.predecessor_operation_id;

    IF NOT FOUND
      OR v_predecessor_phase IS DISTINCT FROM v_expected_predecessor_phase
      OR v_predecessor_status IS DISTINCT FROM 'completed'
      OR v_predecessor_result_sha256 IS DISTINCT FROM NEW.predecessor_result_sha256
      OR v_predecessor_completed_at IS NULL
      OR NEW.created_at < v_predecessor_completed_at THEN
      RAISE EXCEPTION 'connector control predecessor is not the exact completed prior phase'
        USING ERRCODE = 'check_violation';
    END IF;
  END IF;

  RETURN NEW;
END;
$$;


CREATE OR REPLACE FUNCTION public.xinsight_redeem_connector_invitation(
  p_invitation_token text,
  p_acknowledgement_payload text,
  p_control_token text,
  p_control_token_expires_at timestamptz
) RETURNS TABLE (installation_id text, operation_id text)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog
AS $$
DECLARE
  v_now timestamptz := clock_timestamp();
  v_invitation_digest text;
  v_control_token_digest text;
  v_invitation_id text;
  v_installation_id text;
  v_deployment_id text;
  v_invitation_status text;
  v_invitation_created_at timestamptz;
  v_invitation_expires_at timestamptz;
  v_invitation_acknowledgement_sha256 text;
  v_installation_status text;
  v_observed_state text;
  v_existing_project_id text;
  v_existing_final_scope text[];
  v_existing_identity_id text;
  v_existing_acknowledgement_sha256 text;
  v_acknowledgement jsonb;
  v_acknowledgement_sha256 text;
  v_project_id text;
  v_confirmed_at timestamptz;
  v_identity_id text;
  v_operation_id text;
  v_request_payload text;
  v_request_sha256 text;
  v_stored_token_digest text;
  v_stored_token_expires_at timestamptz;
  v_stored_request_payload jsonb;
  v_stored_request_sha256 text;
  v_stored_phase text;
  v_replay_found boolean;
BEGIN
  IF p_invitation_token !~ '^xiiv1_[A-Za-z0-9_-]{43}$'
    OR p_control_token !~ '^xib_[A-Za-z0-9_-]{43}$'
    OR octet_length(p_acknowledgement_payload) NOT BETWEEN 1 AND 16384
    OR p_control_token_expires_at <= v_now
    OR p_control_token_expires_at > v_now + interval '365 days' THEN
    RAISE EXCEPTION 'connector invitation redemption input is invalid'
      USING ERRCODE = 'check_violation';
  END IF;

  v_invitation_digest := 'sha256:' || encode(
    public.digest(convert_to(p_invitation_token, 'UTF8'), 'sha256'),
    'hex'
  );
  v_control_token_digest := 'sha256:' || encode(
    public.digest(convert_to(p_control_token, 'UTF8'), 'sha256'),
    'hex'
  );

  SELECT
    invitation.id,
    invitation.installation_id,
    installation.deployment_id,
    invitation.status,
    invitation.created_at,
    invitation.expires_at,
    invitation.acknowledgement_sha256,
    installation.status,
    installation.observed_state,
    installation.project_id,
    installation.final_event_scope,
    installation.control_service_identity_id,
    installation.installation_acknowledgement_sha256
  INTO
    v_invitation_id,
    v_installation_id,
    v_deployment_id,
    v_invitation_status,
    v_invitation_created_at,
    v_invitation_expires_at,
    v_invitation_acknowledgement_sha256,
    v_installation_status,
    v_observed_state,
    v_existing_project_id,
    v_existing_final_scope,
    v_existing_identity_id,
    v_existing_acknowledgement_sha256
  FROM public.connector_installation_invitations invitation
  JOIN public.connector_installations installation
    ON installation.id = invitation.installation_id
  WHERE invitation.nonce_digest = v_invitation_digest
  FOR UPDATE OF invitation, installation;

  IF NOT FOUND THEN
    RAISE EXCEPTION 'connector invitation is not available'
      USING ERRCODE = 'check_violation';
  END IF;

  BEGIN
    v_acknowledgement := p_acknowledgement_payload::jsonb;
    v_confirmed_at := (v_acknowledgement ->> 'confirmed_at')::timestamptz;
  EXCEPTION WHEN OTHERS THEN
    RAISE EXCEPTION 'connector acknowledgement is invalid'
      USING ERRCODE = 'check_violation';
  END;

  IF jsonb_typeof(v_acknowledgement) <> 'object'
    OR (SELECT count(*) FROM jsonb_object_keys(v_acknowledgement)) <> 7
    OR jsonb_typeof(v_acknowledgement -> 'schema_version') <> 'string'
    OR v_acknowledgement ->> 'schema_version' <> 'xinsight.connector-installation-acknowledgement.v1'
    OR jsonb_typeof(v_acknowledgement -> 'installation_id') <> 'string'
    OR v_acknowledgement ->> 'installation_id' <> v_installation_id
    OR jsonb_typeof(v_acknowledgement -> 'deployment_id') <> 'string'
    OR v_acknowledgement ->> 'deployment_id' <> v_deployment_id
    OR jsonb_typeof(v_acknowledgement -> 'project_id') <> 'string'
    OR (v_acknowledgement ->> 'project_id') !~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'
    OR v_acknowledgement -> 'final_event_scope' <> '["model.request.completed"]'::jsonb
    OR jsonb_typeof(v_acknowledgement -> 'consent_sha256') <> 'string'
    OR (v_acknowledgement ->> 'consent_sha256') !~ '^sha256:[a-f0-9]{64}$'
    OR jsonb_typeof(v_acknowledgement -> 'confirmed_at') <> 'string'
    OR v_confirmed_at < v_invitation_created_at
    OR v_confirmed_at >= v_invitation_expires_at
    OR v_confirmed_at > v_now + interval '1 minute' THEN
    RAISE EXCEPTION 'connector acknowledgement does not match the live invitation'
      USING ERRCODE = 'check_violation';
  END IF;

  v_project_id := v_acknowledgement ->> 'project_id';
  v_acknowledgement_sha256 := 'sha256:' || encode(
    public.digest(convert_to(p_acknowledgement_payload, 'UTF8'), 'sha256'),
    'hex'
  );

  IF EXISTS(SELECT 1 FROM public.connector_installation_source_bindings b WHERE b.installation_id=v_installation_id AND b.bootstrap IS NOT NULL) THEN
    RAISE EXCEPTION 'adoption requires explicit redeem v2' USING ERRCODE='check_violation';
  END IF;

  IF v_invitation_status = 'consumed' THEN
    SELECT
      token.token_digest,
      token.expires_at,
      operation.operation_id,
      operation.phase,
      operation.request_sha256,
      operation.request_payload
    INTO
      v_stored_token_digest,
      v_stored_token_expires_at,
      v_operation_id,
      v_stored_phase,
      v_stored_request_sha256,
      v_stored_request_payload
    FROM public.service_identity_tokens token
    JOIN public.connector_control_operations operation
      ON operation.installation_id = v_installation_id
     AND operation.operation_kind = 'connect'
     AND (operation.phase IS NULL OR operation.phase = 'prepare_paused_source')
    WHERE token.service_identity_id = v_existing_identity_id
      AND token.token_digest = v_control_token_digest
    ORDER BY operation.created_at, operation.operation_id
    LIMIT 1;
    v_replay_found := FOUND;

    IF v_stored_phase IS NULL THEN
      v_request_payload :=
        '{"schema_version":"xinsight.connector-control-operation.v1","operation_id":"' || v_operation_id ||
        '","installation_id":"' || v_installation_id ||
        '","kind":"connect","project_id":"' || v_project_id ||
        '","event_scope":["model.request.completed"],"desired_state":"active"}';
    ELSIF v_stored_phase = 'prepare_paused_source' THEN
      v_request_payload :=
        '{"schema_version":"xinsight.connector-control-operation.v2","operation_id":"' || v_operation_id ||
        '","installation_id":"' || v_installation_id ||
        '","project_id":"' || v_project_id ||
        '","event_scope":["model.request.completed"],"desired_state":"active","kind":"connect",' ||
        '"phase":"prepare_paused_source","request":{"feed_schema":"unirouter.completed-execution.v1",' ||
        '"cursor_key":"unirouter.completed-execution.v1","required_source_state":"paused"}}';
    ELSE
      RAISE EXCEPTION 'connector invitation replay operation is not the initial phase'
        USING ERRCODE = 'check_violation';
    END IF;
    v_request_sha256 := 'sha256:' || encode(
      public.digest(convert_to(v_request_payload, 'UTF8'), 'sha256'),
      'hex'
    );

    IF NOT v_replay_found
      OR v_existing_identity_id IS NULL
      OR v_existing_project_id <> v_project_id
      OR v_existing_final_scope <> ARRAY['model.request.completed']::text[]
      OR v_existing_acknowledgement_sha256 <> v_acknowledgement_sha256
      OR v_invitation_acknowledgement_sha256 <> v_acknowledgement_sha256
      OR v_stored_token_digest <> v_control_token_digest
      OR v_stored_token_expires_at <> p_control_token_expires_at
      OR v_stored_request_sha256 <> v_request_sha256
      OR v_stored_request_payload <> v_request_payload::jsonb THEN
      RAISE EXCEPTION 'connector invitation replay conflicts with durable state'
        USING ERRCODE = 'check_violation';
    END IF;

    RETURN QUERY SELECT v_installation_id, v_operation_id;
    RETURN;
  END IF;

  IF v_invitation_status <> 'active'
    OR v_now < v_invitation_created_at
    OR v_now >= v_invitation_expires_at
    OR v_installation_status <> 'pending'
    OR v_observed_state <> 'awaiting_consent'
    OR v_existing_identity_id IS NOT NULL
    OR v_existing_project_id IS NOT NULL
    OR v_existing_final_scope IS NOT NULL
    OR v_existing_acknowledgement_sha256 IS NOT NULL THEN
    RAISE EXCEPTION 'connector installation is not awaiting consent'
      USING ERRCODE = 'check_violation';
  END IF;

  v_identity_id := 'xsi_' || translate(
    rtrim(encode(public.gen_random_bytes(16), 'base64'), '='),
    '+/',
    '-_'
  );
  v_operation_id := 'xco_' || translate(
    rtrim(encode(public.gen_random_bytes(16), 'base64'), '='),
    '+/',
    '-_'
  );
  v_request_payload :=
    '{"schema_version":"xinsight.connector-control-operation.v2","operation_id":"' || v_operation_id ||
    '","installation_id":"' || v_installation_id ||
    '","project_id":"' || v_project_id ||
    '","event_scope":["model.request.completed"],"desired_state":"active","kind":"connect",' ||
    '"phase":"prepare_paused_source","request":{"feed_schema":"unirouter.completed-execution.v1",' ||
    '"cursor_key":"unirouter.completed-execution.v1","required_source_state":"paused"}}';
  v_request_sha256 := 'sha256:' || encode(
    public.digest(convert_to(v_request_payload, 'UTF8'), 'sha256'),
    'hex'
  );

  INSERT INTO public.service_identities (
    id, display_name, status, permissions, created_by_subject,
    created_at, updated_at, expires_at
  ) VALUES (
    v_identity_id,
    'UniRouter Connector ' || v_installation_id,
    'active',
    ARRAY['connector.control.poll', 'connector.control.ack']::text[],
    'system',
    v_now,
    v_now,
    p_control_token_expires_at
  );

  INSERT INTO public.service_identity_tokens (
    token_digest, service_identity_id, token_prefix, created_at, expires_at
  ) VALUES (
    v_control_token_digest,
    v_identity_id,
    substring(p_control_token from 1 for 12),
    v_now,
    p_control_token_expires_at
  );

  UPDATE public.connector_installations
  SET project_id = v_project_id,
      final_event_scope = ARRAY['model.request.completed']::text[],
      control_service_identity_id = v_identity_id,
      installation_acknowledgement_sha256 = v_acknowledgement_sha256,
      observed_state = 'agent_registered',
      updated_at = v_now,
      last_agent_seen_at = v_now,
      version = version + 1
  WHERE id = v_installation_id;

  UPDATE public.connector_installation_invitations
  SET status = 'consumed',
      consumed_at = v_now,
      acknowledgement_sha256 = v_acknowledgement_sha256
  WHERE id = v_invitation_id;

  INSERT INTO public.connector_control_operations (
    operation_id, installation_id, operation_kind, phase, desired_state,
    request_sha256, request_payload, status, available_at, created_at, updated_at
  ) VALUES (
    v_operation_id, v_installation_id, 'connect', 'prepare_paused_source', 'active',
    v_request_sha256, v_request_payload::jsonb, 'pending', v_now, v_now, v_now
  );

  INSERT INTO public.access_audit_records (
    id, occurred_at, actor_kind, actor_ref, action, resource_kind,
    resource_ref, result, correlation_id, details
  ) VALUES (
    'xaa_' || substring(v_operation_id from 5),
    v_now,
    'system',
    'system',
    'connector_installation.redeem',
    'connector_installation',
    v_installation_id,
    'accepted',
    'xa_' || substring(v_operation_id from 5),
    '{}'::jsonb
  );

  RETURN QUERY SELECT v_installation_id, v_operation_id;
END;
$$;


CREATE OR REPLACE FUNCTION public.xinsight_redeem_connector_adoption_invitation(
  p_invitation_token text,
  p_acknowledgement_payload text,
  p_control_token text,
  p_control_token_expires_at timestamptz,
  p_bootstrap_payload text,
  p_redeem_request_sha256 text
) RETURNS TABLE (installation_id text)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog
AS $$
DECLARE
  v_now timestamptz := clock_timestamp();
  v_bootstrap jsonb;
  v_source_payload text;
  v_source_digest text;
  v_allowed_network cidr;
  v_bootstrap_sha256 text;
  v_invitation_digest text;
  v_control_token_digest text;
  v_invitation_id text;
  v_installation_id text;
  v_deployment_id text;
  v_invitation_status text;
  v_invitation_created_at timestamptz;
  v_invitation_expires_at timestamptz;
  v_invitation_acknowledgement_sha256 text;
  v_installation_status text;
  v_observed_state text;
  v_existing_project_id text;
  v_existing_final_scope text[];
  v_existing_identity_id text;
  v_existing_acknowledgement_sha256 text;
  v_acknowledgement jsonb;
  v_acknowledgement_sha256 text;
  v_project_id text;
  v_confirmed_at timestamptz;
  v_identity_id text;
  v_operation_id text;
  v_request_payload text;
  v_request_sha256 text;
  v_stored_token_digest text;
  v_stored_token_expires_at timestamptz;
  v_stored_request_payload jsonb;
  v_stored_request_sha256 text;
  v_stored_phase text;
  v_replay_found boolean;
BEGIN
  IF p_invitation_token !~ '^xiiv1_[A-Za-z0-9_-]{43}$'
    OR p_control_token !~ '^xib_[A-Za-z0-9_-]{43}$'
    OR octet_length(p_acknowledgement_payload) NOT BETWEEN 1 AND 16384
    OR p_control_token_expires_at <= v_now
    OR p_control_token_expires_at > v_now + interval '365 days' THEN
    RAISE EXCEPTION 'connector invitation redemption input is invalid'
      USING ERRCODE = 'check_violation';
  END IF;

  v_invitation_digest := 'sha256:' || encode(
    public.digest(convert_to(p_invitation_token, 'UTF8'), 'sha256'),
    'hex'
  );
  v_control_token_digest := 'sha256:' || encode(
    public.digest(convert_to(p_control_token, 'UTF8'), 'sha256'),
    'hex'
  );

  SELECT
    invitation.id,
    invitation.installation_id,
    installation.deployment_id,
    invitation.status,
    invitation.created_at,
    invitation.expires_at,
    invitation.acknowledgement_sha256,
    installation.status,
    installation.observed_state,
    installation.project_id,
    installation.final_event_scope,
    installation.control_service_identity_id,
    installation.installation_acknowledgement_sha256
  INTO
    v_invitation_id,
    v_installation_id,
    v_deployment_id,
    v_invitation_status,
    v_invitation_created_at,
    v_invitation_expires_at,
    v_invitation_acknowledgement_sha256,
    v_installation_status,
    v_observed_state,
    v_existing_project_id,
    v_existing_final_scope,
    v_existing_identity_id,
    v_existing_acknowledgement_sha256
  FROM public.connector_installation_invitations invitation
  JOIN public.connector_installations installation
    ON installation.id = invitation.installation_id
  WHERE invitation.nonce_digest = v_invitation_digest
  FOR UPDATE OF invitation, installation;

  IF NOT FOUND THEN
    RAISE EXCEPTION 'connector invitation is not available'
      USING ERRCODE = 'check_violation';
  END IF;

  BEGIN
    v_acknowledgement := p_acknowledgement_payload::jsonb;
    v_confirmed_at := (v_acknowledgement ->> 'confirmed_at')::timestamptz;
  EXCEPTION WHEN OTHERS THEN
    RAISE EXCEPTION 'connector acknowledgement is invalid'
      USING ERRCODE = 'check_violation';
  END;

  IF jsonb_typeof(v_acknowledgement) <> 'object'
    OR (SELECT count(*) FROM jsonb_object_keys(v_acknowledgement)) <> 7
    OR jsonb_typeof(v_acknowledgement -> 'schema_version') <> 'string'
    OR v_acknowledgement ->> 'schema_version' <> 'xinsight.connector-installation-acknowledgement.v1'
    OR jsonb_typeof(v_acknowledgement -> 'installation_id') <> 'string'
    OR v_acknowledgement ->> 'installation_id' <> v_installation_id
    OR jsonb_typeof(v_acknowledgement -> 'deployment_id') <> 'string'
    OR v_acknowledgement ->> 'deployment_id' <> v_deployment_id
    OR jsonb_typeof(v_acknowledgement -> 'project_id') <> 'string'
    OR (v_acknowledgement ->> 'project_id') !~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'
    OR v_acknowledgement -> 'final_event_scope' <> '["model.request.completed"]'::jsonb
    OR jsonb_typeof(v_acknowledgement -> 'consent_sha256') <> 'string'
    OR (v_acknowledgement ->> 'consent_sha256') !~ '^sha256:[a-f0-9]{64}$'
    OR jsonb_typeof(v_acknowledgement -> 'confirmed_at') <> 'string'
    OR v_confirmed_at < v_invitation_created_at
    OR v_confirmed_at >= v_invitation_expires_at
    OR v_confirmed_at > v_now + interval '1 minute' THEN
    RAISE EXCEPTION 'connector acknowledgement does not match the live invitation'
      USING ERRCODE = 'check_violation';
  END IF;

  v_project_id := v_acknowledgement ->> 'project_id';
  v_acknowledgement_sha256 := 'sha256:' || encode(
    public.digest(convert_to(p_acknowledgement_payload, 'UTF8'), 'sha256'),
    'hex'
  );

  v_bootstrap:=p_bootstrap_payload::jsonb;
  v_bootstrap_sha256:='sha256:'||encode(public.digest(convert_to(p_bootstrap_payload,'UTF8'),'sha256'),'hex');
  IF jsonb_typeof(v_bootstrap) IS DISTINCT FROM 'object'
    OR (SELECT count(*) FROM jsonb_object_keys(v_bootstrap)) <> 10
    OR v_bootstrap->>'installation_id' IS DISTINCT FROM v_installation_id
    OR v_bootstrap->>'source_binding_sha256' IS DISTINCT FROM v_acknowledgement->>'consent_sha256'
    OR COALESCE(p_redeem_request_sha256,'') !~ '^sha256:[a-f0-9]{64}$'
    OR COALESCE(v_bootstrap->>'org_id','') !~ '^[1-9][0-9]*$'
    OR COALESCE(v_bootstrap->>'project_id','') !~ '^[1-9][0-9]*$'
    OR COALESCE(v_bootstrap->>'feed_key_id','') !~ '^[1-9][0-9]*$'
    OR COALESCE(v_bootstrap->>'registration_operation_id','') !~ '^[a-f0-9]{32}$'
    OR COALESCE(v_bootstrap->>'key_prefix','') !~ '^ur-xf1-[a-f0-9]{13}$'
    OR jsonb_typeof(v_bootstrap->'org_id') IS DISTINCT FROM 'number'
    OR jsonb_typeof(v_bootstrap->'project_id') IS DISTINCT FROM 'number'
    OR COALESCE(v_bootstrap->>'source_instance','') !~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'
    OR jsonb_typeof(v_bootstrap->'allowed_ip') IS DISTINCT FROM 'string'
    OR v_bootstrap->>'verifier_scheme' IS DISTINCT FROM 'receiver_sha256_v1' THEN
    RAISE EXCEPTION 'adoption bootstrap conflicts' USING ERRCODE='check_violation';
  END IF;
  BEGIN
    PERFORM (v_bootstrap->>'org_id')::bigint,(v_bootstrap->>'project_id')::bigint;
    v_allowed_network:=(v_bootstrap->>'allowed_ip')::cidr;
    IF masklen(v_allowed_network)<>(CASE WHEN family(v_allowed_network)=4 THEN 32 ELSE 128 END)
      OR v_allowed_network::text IS DISTINCT FROM v_bootstrap->>'allowed_ip' THEN
      RAISE EXCEPTION 'invalid network';
    END IF;
  EXCEPTION WHEN OTHERS THEN
    RAISE EXCEPTION 'adoption bootstrap scope is invalid' USING ERRCODE='check_violation';
  END;
  v_source_payload:='{"schema_version":"unirouter.xinsight.connector-source-scope.v1","installation_id":'
    || to_json(v_installation_id)::text || ',"x_deployment_id":' || to_json(v_deployment_id)::text
    || ',"org_id":' || (v_bootstrap->>'org_id') || ',"project_id":' || (v_bootstrap->>'project_id')
    || ',"source_instance":' || to_json(v_bootstrap->>'source_instance')::text
    || ',"event_scope":"model.request.completed","feed_schema":"xinsight.completed-execution-feed.v1"}';
  v_source_digest:='sha256:'||encode(public.digest(convert_to(v_source_payload,'UTF8'),'sha256'),'hex');
  IF v_source_digest IS DISTINCT FROM v_bootstrap->>'source_binding_sha256' THEN
    RAISE EXCEPTION 'adoption source binding conflicts' USING ERRCODE='check_violation';
  END IF;
  IF v_invitation_status = 'consumed' THEN
    IF v_existing_project_id IS DISTINCT FROM v_project_id
      OR v_existing_final_scope IS DISTINCT FROM ARRAY['model.request.completed']::text[]
      OR v_existing_acknowledgement_sha256 IS DISTINCT FROM v_acknowledgement_sha256
      OR v_invitation_acknowledgement_sha256 IS DISTINCT FROM v_acknowledgement_sha256
      OR NOT EXISTS(SELECT 1 FROM public.service_identity_tokens t WHERE t.service_identity_id=v_existing_identity_id
        AND t.token_digest=v_control_token_digest AND t.expires_at=p_control_token_expires_at)
      OR NOT EXISTS(SELECT 1 FROM public.connector_installation_source_bindings b WHERE b.installation_id=v_installation_id
        AND b.bootstrap=v_bootstrap AND b.bootstrap_sha256=v_bootstrap_sha256 AND b.redeem_request_sha256=p_redeem_request_sha256) THEN
      RAISE EXCEPTION 'adoption replay conflicts' USING ERRCODE='check_violation';
    END IF;
    RETURN QUERY SELECT v_installation_id;
    RETURN;
  END IF;
  IF v_invitation_status <> 'active'
    OR v_now < v_invitation_created_at
    OR v_now >= v_invitation_expires_at
    OR v_installation_status <> 'pending'
    OR v_observed_state <> 'awaiting_consent'
    OR v_existing_identity_id IS NOT NULL
    OR v_existing_project_id IS NOT NULL
    OR v_existing_final_scope IS NOT NULL
    OR v_existing_acknowledgement_sha256 IS NOT NULL THEN
    RAISE EXCEPTION 'connector installation is not awaiting consent'
      USING ERRCODE = 'check_violation';
  END IF;

  v_identity_id := 'xsi_' || translate(
    rtrim(encode(public.gen_random_bytes(16), 'base64'), '='),
    '+/',
    '-_'
  );
  INSERT INTO public.service_identities (
    id, display_name, status, permissions, created_by_subject,
    created_at, updated_at, expires_at
  ) VALUES (
    v_identity_id,
    'UniRouter Connector ' || v_installation_id,
    'active',
    ARRAY['connector.control.poll', 'connector.control.ack']::text[],
    'system',
    v_now,
    v_now,
    p_control_token_expires_at
  );

  INSERT INTO public.service_identity_tokens (
    token_digest, service_identity_id, token_prefix, created_at, expires_at
  ) VALUES (
    v_control_token_digest,
    v_identity_id,
    substring(p_control_token from 1 for 12),
    v_now,
    p_control_token_expires_at
  );

  UPDATE public.connector_installations
  SET project_id = v_project_id,
      final_event_scope = ARRAY['model.request.completed']::text[],
      control_service_identity_id = v_identity_id,
      installation_acknowledgement_sha256 = v_acknowledgement_sha256,
      observed_state = 'agent_registered',
      updated_at = v_now,
      last_agent_seen_at = v_now,
      version = version + 1
  WHERE id = v_installation_id;

  UPDATE public.connector_installation_invitations
  SET status = 'consumed',
      consumed_at = v_now,
      acknowledgement_sha256 = v_acknowledgement_sha256
  WHERE id = v_invitation_id;

  INSERT INTO public.connector_installation_source_bindings(installation_id,source_binding_sha256,bootstrap,bootstrap_sha256,redeem_request_sha256)
    VALUES(v_installation_id,v_acknowledgement->>'consent_sha256',v_bootstrap,v_bootstrap_sha256,p_redeem_request_sha256);

  INSERT INTO public.access_audit_records (
    id, occurred_at, actor_kind, actor_ref, action, resource_kind,
    resource_ref, result, correlation_id, details
  ) VALUES (
    'xaa_' || substring(v_identity_id from 5),
    v_now,
    'system',
    'system',
    'connector_installation.redeem',
    'connector_installation',
    v_installation_id,
    'accepted',
    'xa_' || substring(v_identity_id from 5),
    '{}'::jsonb
  );

  RETURN QUERY SELECT v_installation_id;
END;
$$;


REVOKE ALL ON FUNCTION public.xinsight_redeem_connector_adoption_invitation(text,text,text,timestamptz,text,text) FROM PUBLIC;
DO $$ BEGIN IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='xinsight_workspace') THEN GRANT EXECUTE ON FUNCTION public.xinsight_redeem_connector_adoption_invitation(text,text,text,timestamptz,text,text) TO xinsight_workspace; END IF; END $$;

-- Same unpublished managed batch: consent is part of its original installation.
ALTER TABLE connector_installations
 ADD COLUMN setup_request_id uuid UNIQUE,
 ADD COLUMN setup_actor_subject text,
 ADD COLUMN setup_request_sha256 text,
 ADD COLUMN setup_consent jsonb,
 ADD COLUMN setup_reuses_policy boolean,
 ADD COLUMN setup_policy_revision bigint,
 ADD COLUMN setup_policy_applied_at timestamptz,
 ADD COLUMN setup_error_code text,
 ADD CONSTRAINT connector_setup_consent_shape CHECK(
  (setup_request_id IS NULL AND setup_actor_subject IS NULL AND setup_request_sha256 IS NULL AND setup_consent IS NULL AND setup_reuses_policy IS NULL AND setup_policy_revision IS NULL AND setup_policy_applied_at IS NULL AND setup_error_code IS NULL)
  OR (setup_request_id IS NOT NULL AND setup_actor_subject IS NOT NULL AND setup_request_sha256 IS NOT NULL AND setup_consent IS NOT NULL AND setup_actor_subject ~ '^xs_[a-z0-9]{16,64}$' AND setup_request_sha256 ~ '^sha256:[a-f0-9]{64}$' AND jsonb_typeof(setup_consent)='object' AND setup_consent ?& ARRAY['schema_version','installation_id','deployment_id','xi_origin','ur_origin','consent_revision','consent_sha256','expires_at','custody','expected_custody_revision'] AND (setup_consent->>'schema_version') IS NOT NULL AND (setup_consent->>'installation_id') IS NOT NULL AND (setup_consent->>'deployment_id') IS NOT NULL AND setup_consent->>'schema_version'='xinsight.connector-setup-consent-view.v1' AND setup_consent->>'installation_id'=id AND setup_consent->>'deployment_id'=deployment_id AND setup_reuses_policy IS NOT NULL AND (setup_policy_revision IS NULL OR setup_policy_revision>0))) ;
CREATE FUNCTION enforce_connector_setup_consent_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='UPDATE' AND (NEW.setup_request_id,NEW.setup_actor_subject,NEW.setup_request_sha256,NEW.setup_consent,NEW.setup_reuses_policy) IS DISTINCT FROM (OLD.setup_request_id,OLD.setup_actor_subject,OLD.setup_request_sha256,OLD.setup_consent,OLD.setup_reuses_policy)
 AND OLD.setup_request_id IS NOT NULL THEN RAISE EXCEPTION 'connector setup consent is immutable' USING ERRCODE='23514'; END IF;
 RETURN NEW;
END; $$;
CREATE TRIGGER connector_setup_consent_immutable BEFORE UPDATE ON connector_installations FOR EACH ROW EXECUTE FUNCTION enforce_connector_setup_consent_immutable();
ALTER TABLE connector_installations ENABLE ALWAYS TRIGGER connector_setup_consent_immutable;
REVOKE ALL ON FUNCTION enforce_connector_setup_consent_immutable() FROM PUBLIC;
