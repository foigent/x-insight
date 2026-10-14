ALTER TABLE service_identities
  DROP CONSTRAINT service_identities_permissions_check,
  DROP CONSTRAINT service_identities_creator_check;

ALTER TABLE service_identities
  ADD CONSTRAINT service_identities_permissions_check CHECK (
    (
      created_by_subject = 'system'
      AND permissions = ARRAY['connector.control.poll', 'connector.control.ack']::text[]
    )
    OR (
      created_by_subject <> 'system'
      AND cardinality(permissions) BETWEEN 1 AND 2
      AND permissions <@ ARRAY['evidence.read', 'evidence.cite']::text[]
    )
  ),
  ADD CONSTRAINT service_identities_creator_check CHECK (
    created_by_subject IN ('root', 'system')
    OR created_by_subject ~ '^xs_[a-z0-9]{16,64}$'
  );

CREATE TABLE connector_installations (
  id text PRIMARY KEY,
  deployment_id text NOT NULL,
  connector_type text NOT NULL,
  requested_event_scope text[] NOT NULL,
  final_event_scope text[],
  project_id text,
  source_instance text,
  source_instance_sha256 text,
  control_service_identity_id text UNIQUE
    REFERENCES service_identities(id) ON DELETE RESTRICT,
  installation_acknowledgement_sha256 text,
  status text NOT NULL,
  desired_state text NOT NULL,
  observed_state text NOT NULL,
  status_reason_code text,
  last_agent_seen_at timestamptz,
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL,
  version bigint NOT NULL DEFAULT 1,
  CONSTRAINT connector_installations_id_check CHECK (id ~ '^xci_[A-Za-z0-9_-]{22}$'),
  CONSTRAINT connector_installations_deployment_check CHECK (deployment_id ~ '^xid_[A-Za-z0-9_-]{22}$'),
  CONSTRAINT connector_installations_type_check CHECK (connector_type = 'unirouter'),
  CONSTRAINT connector_installations_requested_scope_check CHECK (
    requested_event_scope = ARRAY['model.request.completed']::text[]
  ),
  CONSTRAINT connector_installations_authority_check CHECK (
    (
      project_id IS NULL
      AND final_event_scope IS NULL
      AND control_service_identity_id IS NULL
      AND installation_acknowledgement_sha256 IS NULL
      AND source_instance IS NULL
      AND source_instance_sha256 IS NULL
    )
    OR (
      project_id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'
      AND final_event_scope = requested_event_scope
      AND control_service_identity_id IS NOT NULL
      AND installation_acknowledgement_sha256 ~ '^sha256:[a-f0-9]{64}$'
      AND (
        (source_instance IS NULL AND source_instance_sha256 IS NULL)
        OR (
          source_instance ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'
          AND source_instance_sha256 ~ '^sha256:[a-f0-9]{64}$'
        )
      )
    )
  ),
  CONSTRAINT connector_installations_status_check CHECK (
    status IN ('pending', 'connected', 'paused', 'abnormal', 'disconnected')
  ),
  CONSTRAINT connector_installations_desired_check CHECK (
    desired_state IN ('active', 'paused', 'disconnected')
  ),
  CONSTRAINT connector_installations_observed_check CHECK (
    observed_state IN (
      'awaiting_consent', 'agent_registered', 'source_ready', 'controller_pending',
      'connected', 'paused', 'abnormal', 'disconnected'
    )
  ),
  CONSTRAINT connector_installations_reason_check CHECK (
    (
      status = 'abnormal'
      AND observed_state = 'abnormal'
      AND status_reason_code IN (
        'authorization_denied', 'scope_rejected', 'source_unavailable',
        'permission_denied', 'source_identity_invalid', 'transport_unavailable',
        'contract_invalid', 'internal_error'
      )
    )
    OR (status <> 'abnormal' AND status_reason_code IS NULL)
  ),
  CONSTRAINT connector_installations_state_check CHECK (
    (status = 'pending' AND observed_state IN ('awaiting_consent', 'agent_registered', 'source_ready', 'controller_pending'))
    OR (status = 'connected' AND observed_state = 'connected')
    OR (status = 'paused' AND observed_state = 'paused')
    OR (status = 'abnormal' AND observed_state = 'abnormal')
    OR (status = 'disconnected' AND observed_state = 'disconnected')
  ),
  CONSTRAINT connector_installations_time_check CHECK (
    updated_at >= created_at
    AND (last_agent_seen_at IS NULL OR last_agent_seen_at >= created_at)
  ),
  CONSTRAINT connector_installations_version_check CHECK (version > 0)
);

CREATE INDEX connector_installations_status
  ON connector_installations (status, updated_at DESC, id);

CREATE FUNCTION public.xinsight_redeem_connector_invitation(
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
      operation.request_sha256,
      operation.request_payload
    INTO
      v_stored_token_digest,
      v_stored_token_expires_at,
      v_operation_id,
      v_stored_request_sha256,
      v_stored_request_payload
    FROM public.service_identity_tokens token
    JOIN public.connector_control_operations operation
      ON operation.installation_id = v_installation_id
     AND operation.operation_kind = 'connect'
    WHERE token.service_identity_id = v_existing_identity_id
      AND token.token_digest = v_control_token_digest
    ORDER BY operation.created_at, operation.operation_id
    LIMIT 1;
    v_replay_found := FOUND;

    v_request_payload :=
      '{"schema_version":"xinsight.connector-control-operation.v1","operation_id":"' || v_operation_id ||
      '","installation_id":"' || v_installation_id ||
      '","kind":"connect","project_id":"' || v_project_id ||
      '","event_scope":["model.request.completed"],"desired_state":"active"}';
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
    '{"schema_version":"xinsight.connector-control-operation.v1","operation_id":"' || v_operation_id ||
    '","installation_id":"' || v_installation_id ||
    '","kind":"connect","project_id":"' || v_project_id ||
    '","event_scope":["model.request.completed"],"desired_state":"active"}';
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
    operation_id, installation_id, operation_kind, desired_state,
    request_sha256, request_payload, status, available_at, created_at, updated_at
  ) VALUES (
    v_operation_id, v_installation_id, 'connect', 'active',
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
  text, text, text, timestamptz
) FROM PUBLIC;

CREATE TABLE connector_installation_invitations (
  id text PRIMARY KEY,
  installation_id text NOT NULL REFERENCES connector_installations(id) ON DELETE RESTRICT,
  nonce_digest text NOT NULL UNIQUE,
  token_prefix text NOT NULL,
  audience text NOT NULL,
  xinsight_origin text NOT NULL,
  unirouter_install_origin text NOT NULL,
  requested_event_scope text[] NOT NULL,
  status text NOT NULL,
  created_by_subject text NOT NULL,
  created_at timestamptz NOT NULL,
  expires_at timestamptz NOT NULL,
  consumed_at timestamptz,
  acknowledgement_sha256 text,
  CONSTRAINT connector_installation_invitations_id_check CHECK (id ~ '^xii_[A-Za-z0-9_-]{22}$'),
  CONSTRAINT connector_installation_invitations_nonce_check CHECK (nonce_digest ~ '^sha256:[a-f0-9]{64}$'),
  CONSTRAINT connector_installation_invitations_prefix_check CHECK (token_prefix ~ '^xiiv1_[A-Za-z0-9_-]{6}$'),
  CONSTRAINT connector_installation_invitations_audience_check CHECK (audience = 'unirouter'),
  CONSTRAINT connector_installation_invitations_origins_check CHECK (
    octet_length(xinsight_origin) BETWEEN 9 AND 2048
    AND octet_length(unirouter_install_origin) BETWEEN 9 AND 2048
  ),
  CONSTRAINT connector_installation_invitations_scope_check CHECK (
    requested_event_scope = ARRAY['model.request.completed']::text[]
  ),
  CONSTRAINT connector_installation_invitations_status_check CHECK (
    status IN ('active', 'consumed', 'expired', 'revoked')
  ),
  CONSTRAINT connector_installation_invitations_creator_check CHECK (
    created_by_subject ~ '^xs_[a-z0-9]{16,64}$'
  ),
  CONSTRAINT connector_installation_invitations_expiry_check CHECK (expires_at > created_at),
  CONSTRAINT connector_installation_invitations_terminal_check CHECK (
    (status = 'active' AND consumed_at IS NULL AND acknowledgement_sha256 IS NULL)
    OR (
      status = 'consumed'
      AND consumed_at IS NOT NULL
      AND acknowledgement_sha256 ~ '^sha256:[a-f0-9]{64}$'
    )
    OR (status IN ('expired', 'revoked') AND consumed_at IS NULL AND acknowledgement_sha256 IS NULL)
  )
);

CREATE INDEX connector_installation_invitations_installation
  ON connector_installation_invitations (installation_id, created_at DESC, id);

CREATE TABLE connector_control_operations (
  operation_id text PRIMARY KEY,
  installation_id text NOT NULL REFERENCES connector_installations(id) ON DELETE RESTRICT,
  operation_kind text NOT NULL,
  desired_state text NOT NULL,
  request_sha256 text NOT NULL,
  request_payload jsonb NOT NULL,
  status text NOT NULL,
  attempt_count integer NOT NULL DEFAULT 0,
  available_at timestamptz NOT NULL,
  lease_expires_at timestamptz,
  result_sha256 text,
  result_payload jsonb,
  reason_code text,
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL,
  completed_at timestamptz,
  CONSTRAINT connector_control_operations_id_check CHECK (operation_id ~ '^xco_[A-Za-z0-9_-]{22}$'),
  CONSTRAINT connector_control_operations_kind_check CHECK (
    operation_kind IN ('connect', 'pause', 'resume', 'rotate', 'replay')
  ),
  CONSTRAINT connector_control_operations_desired_check CHECK (
    desired_state IN ('active', 'paused', 'disconnected')
  ),
  CONSTRAINT connector_control_operations_request_check CHECK (
    request_sha256 ~ '^sha256:[a-f0-9]{64}$'
    AND jsonb_typeof(request_payload) = 'object'
    AND octet_length(request_payload::text) <= 16384
  ),
  CONSTRAINT connector_control_operations_status_check CHECK (
    status IN ('pending', 'running', 'completed', 'failed')
  ),
  CONSTRAINT connector_control_operations_attempt_check CHECK (attempt_count >= 0),
  CONSTRAINT connector_control_operations_result_check CHECK (
    (
      status IN ('pending', 'running')
      AND result_sha256 IS NULL
      AND result_payload IS NULL
      AND reason_code IS NULL
      AND completed_at IS NULL
    )
    OR (
      status = 'completed'
      AND result_sha256 ~ '^sha256:[a-f0-9]{64}$'
      AND jsonb_typeof(result_payload) = 'object'
      AND octet_length(result_payload::text) <= 16384
      AND reason_code IS NULL
      AND completed_at IS NOT NULL
    )
    OR (
      status = 'failed'
      AND result_sha256 ~ '^sha256:[a-f0-9]{64}$'
      AND jsonb_typeof(result_payload) = 'object'
      AND octet_length(result_payload::text) <= 16384
      AND reason_code IN (
        'authorization_denied', 'scope_rejected', 'source_unavailable',
        'permission_denied', 'source_identity_invalid', 'transport_unavailable',
        'contract_invalid', 'internal_error'
      )
      AND completed_at IS NOT NULL
    )
  ),
  CONSTRAINT connector_control_operations_lease_check CHECK (
    (status = 'running' AND lease_expires_at IS NOT NULL)
    OR (status <> 'running' AND lease_expires_at IS NULL)
  ),
  CONSTRAINT connector_control_operations_time_check CHECK (
    updated_at >= created_at AND available_at >= created_at
  ),
  CONSTRAINT connector_control_operations_request_unique UNIQUE (installation_id, request_sha256)
);

CREATE INDEX connector_control_operations_poll
  ON connector_control_operations (installation_id, status, available_at, created_at, operation_id);

CREATE FUNCTION public.xinsight_guard_service_credential_terminal_update()
RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog
AS $$
BEGIN
  IF current_user <> 'xinsight_workspace' THEN
    RETURN NEW;
  END IF;

  IF TG_TABLE_NAME = 'service_identities' THEN
    IF OLD.status IN ('revoked', 'expired')
      AND (
        NEW.status IS DISTINCT FROM OLD.status
        OR NEW.revoked_at IS DISTINCT FROM OLD.revoked_at
        OR NEW.revoke_reason IS DISTINCT FROM OLD.revoke_reason
      ) THEN
      RAISE EXCEPTION 'terminal service identity state is immutable'
        USING ERRCODE = 'check_violation';
    END IF;
  ELSIF TG_TABLE_NAME = 'service_identity_tokens' THEN
    IF OLD.revoked_at IS NOT NULL
      AND NEW.revoked_at IS DISTINCT FROM OLD.revoked_at THEN
      RAISE EXCEPTION 'revoked service token state is immutable'
        USING ERRCODE = 'check_violation';
    END IF;
  ELSE
    RAISE EXCEPTION 'service credential terminal guard is misconfigured'
      USING ERRCODE = 'check_violation';
  END IF;

  RETURN NEW;
END;
$$;

REVOKE ALL PRIVILEGES ON FUNCTION public.xinsight_guard_service_credential_terminal_update()
FROM PUBLIC;

CREATE TRIGGER service_identities_terminal_update_guard
BEFORE UPDATE ON service_identities
FOR EACH ROW
EXECUTE FUNCTION public.xinsight_guard_service_credential_terminal_update();

CREATE TRIGGER service_identity_tokens_terminal_update_guard
BEFORE UPDATE ON service_identity_tokens
FOR EACH ROW
EXECUTE FUNCTION public.xinsight_guard_service_credential_terminal_update();
