ALTER TABLE connector_control_operations
  ADD COLUMN phase text,
  ADD COLUMN predecessor_operation_id text,
  ADD COLUMN predecessor_result_sha256 text;

ALTER TABLE connector_control_operations
  ADD CONSTRAINT connector_control_operations_installation_operation_unique
    UNIQUE (installation_id, operation_id),
  ADD CONSTRAINT connector_control_operations_phase_check CHECK (
    phase IS NULL
    OR phase IN ('prepare_paused_source', 'register_receiver', 'resume_source')
  ),
  ADD CONSTRAINT connector_control_operations_predecessor_shape_check CHECK (
    (
      phase IS NULL
      AND predecessor_operation_id IS NULL
      AND predecessor_result_sha256 IS NULL
    )
    OR (
      phase = 'prepare_paused_source'
      AND predecessor_operation_id IS NULL
      AND predecessor_result_sha256 IS NULL
    )
    OR (
      phase IN ('register_receiver', 'resume_source')
      AND predecessor_operation_id ~ '^xco_[A-Za-z0-9_-]{22}$'
      AND predecessor_result_sha256 ~ '^sha256:[a-f0-9]{64}$'
    )
  ),
  ADD CONSTRAINT connector_control_operations_predecessor_fk
    FOREIGN KEY (installation_id, predecessor_operation_id)
    REFERENCES connector_control_operations (installation_id, operation_id)
    ON DELETE RESTRICT;

CREATE UNIQUE INDEX connector_control_operations_installation_phase_unique
  ON connector_control_operations (installation_id, phase)
  WHERE phase IS NOT NULL;

CREATE FUNCTION public.xinsight_validate_connector_control_phase()
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

REVOKE ALL PRIVILEGES ON FUNCTION public.xinsight_validate_connector_control_phase()
FROM PUBLIC;

CREATE TRIGGER connector_control_operations_phase_guard
BEFORE INSERT OR UPDATE ON connector_control_operations
FOR EACH ROW
EXECUTE FUNCTION public.xinsight_validate_connector_control_phase();

-- Preserve the accepted atomic CP1 invitation/identity/token boundary while
-- advancing only its initial durable operation to the v2 phase contract.
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

REVOKE ALL PRIVILEGES ON FUNCTION public.xinsight_redeem_connector_invitation(
  text,
  text,
  text,
  timestamptz
) FROM PUBLIC;
