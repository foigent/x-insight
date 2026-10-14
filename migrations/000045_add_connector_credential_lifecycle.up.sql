-- Same-installation control credentials are owner-controlled. The web role
-- never writes current/pending state or recovery tickets directly.
CREATE TABLE connector_control_credentials (
 installation_id text PRIMARY KEY REFERENCES connector_installations(id) ON DELETE RESTRICT,
 service_identity_id text NOT NULL UNIQUE REFERENCES service_identities(id) ON DELETE RESTRICT,
 current_token_digest text NOT NULL REFERENCES service_identity_tokens(token_digest) ON DELETE RESTRICT,
 created_at timestamptz NOT NULL,
 updated_at timestamptz NOT NULL,
 CHECK(current_token_digest ~ '^sha256:[a-f0-9]{64}$')
);
REVOKE ALL ON connector_control_credentials FROM PUBLIC;

-- The pre-lifecycle connector has exactly one token per service identity.
-- Ambiguous histories remain without a current pointer and must be diagnosed,
-- never guessed or replaced by a newly generated identity.
INSERT INTO connector_control_credentials(installation_id,service_identity_id,current_token_digest,created_at,updated_at)
SELECT c.id,c.control_service_identity_id,min(t.token_digest),clock_timestamp(),clock_timestamp()
FROM connector_installations c
JOIN connector_installation_source_bindings b ON b.installation_id=c.id
JOIN connector_installation_invitations inv ON inv.installation_id=c.id
 AND inv.status='consumed' AND inv.acknowledgement_sha256=c.installation_acknowledgement_sha256
JOIN service_identity_tokens t ON t.service_identity_id=c.control_service_identity_id
WHERE c.setup_request_id IS NOT NULL AND c.control_service_identity_id IS NOT NULL
GROUP BY c.id,c.control_service_identity_id
HAVING count(DISTINCT t.token_digest)=1;

CREATE TABLE connector_control_credential_operations (
 operation_id text PRIMARY KEY CHECK(operation_id ~ '^xcr_[A-Za-z0-9_-]{22}$'),
 installation_id text NOT NULL REFERENCES connector_control_credentials(installation_id) ON DELETE RESTRICT,
 kind text NOT NULL CHECK(kind IN ('rotate','recover')),
 state text NOT NULL CHECK(state IN ('prepared','committed','superseded')),
 request_sha256 text NOT NULL CHECK(request_sha256 ~ '^sha256:[a-f0-9]{64}$'),
 previous_token_sha256 text NOT NULL REFERENCES service_identity_tokens(token_digest) ON DELETE RESTRICT,
 replacement_token_sha256 text NOT NULL UNIQUE REFERENCES service_identity_tokens(token_digest) ON DELETE RESTRICT,
 replacement_expires_at timestamptz NOT NULL,
 recovery_id text,
 superseded_by_operation_id text,
 created_at timestamptz NOT NULL,
 updated_at timestamptz NOT NULL,
 CHECK(previous_token_sha256<>replacement_token_sha256),
 CHECK((kind='rotate' AND recovery_id IS NULL) OR (kind='recover' AND recovery_id ~ '^xrr_[A-Za-z0-9_-]{22}$')),
 CHECK((state='superseded')=(superseded_by_operation_id IS NOT NULL))
);
CREATE UNIQUE INDEX connector_control_credential_one_prepared
 ON connector_control_credential_operations(installation_id) WHERE state='prepared';
CREATE UNIQUE INDEX connector_control_credential_recovery_once
 ON connector_control_credential_operations(recovery_id) WHERE recovery_id IS NOT NULL;
REVOKE ALL ON connector_control_credential_operations FROM PUBLIC;

CREATE TABLE connector_control_recovery_tickets (
 recovery_id text PRIMARY KEY CHECK(recovery_id ~ '^xrr_[A-Za-z0-9_-]{22}$'),
 installation_id text NOT NULL REFERENCES connector_control_credentials(installation_id) ON DELETE RESTRICT,
 request_id uuid NOT NULL,
 actor_subject text NOT NULL CHECK(actor_subject ~ '^xs_[a-z0-9]{16,64}$'),
 ticket_digest text NOT NULL UNIQUE CHECK(ticket_digest ~ '^sha256:[a-f0-9]{64}$'),
 previous_token_sha256 text NOT NULL REFERENCES service_identity_tokens(token_digest) ON DELETE RESTRICT,
 superseded_operation_id text,
 superseded_replacement_token_sha256 text,
 state text NOT NULL CHECK(state IN ('issued','consumed','expired')),
 consumed_operation_id text,
 expires_at timestamptz NOT NULL,
 created_at timestamptz NOT NULL,
 updated_at timestamptz NOT NULL,
 UNIQUE(installation_id,request_id),
 CHECK((superseded_operation_id IS NULL)=(superseded_replacement_token_sha256 IS NULL)),
 CHECK(superseded_operation_id IS NULL OR superseded_operation_id ~ '^xcr_[A-Za-z0-9_-]{22}$'),
 CHECK(superseded_replacement_token_sha256 IS NULL OR superseded_replacement_token_sha256 ~ '^sha256:[a-f0-9]{64}$'),
 CHECK((state='consumed')=(consumed_operation_id IS NOT NULL)),
 CHECK(consumed_operation_id IS NULL OR consumed_operation_id ~ '^xcr_[A-Za-z0-9_-]{22}$'),
 CHECK(expires_at>created_at)
);
CREATE UNIQUE INDEX connector_control_recovery_one_issued
 ON connector_control_recovery_tickets(installation_id) WHERE state='issued';
REVOKE ALL ON connector_control_recovery_tickets FROM PUBLIC;

-- Redeem v2 still runs under its existing narrow security-definer function.
-- Initialize the current lineage in that same caller transaction after the
-- original function has committed its identity/token/source binding rows.
CREATE FUNCTION public.xinsight_initialize_connector_control_credential(p_installation_id text,p_control_token text)
RETURNS void LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE v_identity text; v_digest text; v_count bigint; v_now timestamptz := clock_timestamp();
BEGIN
 IF p_installation_id !~ '^xci_[A-Za-z0-9_-]{22}$' OR p_control_token !~ '^xib_[A-Za-z0-9_-]{43}$' THEN
  RAISE EXCEPTION 'connector credential initialization invalid' USING ERRCODE='check_violation';
 END IF;
 v_digest := 'sha256:' || encode(public.digest(convert_to(p_control_token,'UTF8'),'sha256'),'hex');
 SELECT c.control_service_identity_id INTO v_identity FROM public.connector_installations c
 JOIN public.connector_installation_source_bindings b ON b.installation_id=c.id
 JOIN public.connector_installation_invitations inv ON inv.installation_id=c.id
  AND inv.status='consumed' AND inv.acknowledgement_sha256=c.installation_acknowledgement_sha256
 WHERE c.id=p_installation_id AND c.control_service_identity_id IS NOT NULL
 FOR UPDATE OF c;
 IF v_identity IS NULL THEN
  RAISE EXCEPTION 'connector credential initialization conflict' USING ERRCODE='check_violation';
 END IF;
 IF EXISTS(SELECT 1 FROM public.connector_control_credentials
   WHERE installation_id=p_installation_id AND service_identity_id=v_identity) THEN
  RETURN;
 END IF;
 SELECT count(*) INTO v_count FROM public.service_identity_tokens
 WHERE service_identity_id=v_identity AND token_digest=v_digest AND revoked_at IS NULL;
 IF v_count<>1 THEN
  RAISE EXCEPTION 'connector credential token conflict' USING ERRCODE='check_violation';
 END IF;
 INSERT INTO public.connector_control_credentials(installation_id,service_identity_id,current_token_digest,created_at,updated_at)
 VALUES(p_installation_id,v_identity,v_digest,v_now,v_now)
 ON CONFLICT(installation_id) DO NOTHING;
 IF NOT EXISTS(SELECT 1 FROM public.connector_control_credentials
  WHERE installation_id=p_installation_id AND service_identity_id=v_identity AND current_token_digest=v_digest) THEN
  RAISE EXCEPTION 'connector credential lineage conflict' USING ERRCODE='check_violation';
 END IF;
END;
$$;
REVOKE ALL ON FUNCTION public.xinsight_initialize_connector_control_credential(text,text) FROM PUBLIC;
DO $$ BEGIN IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='xinsight_workspace') THEN
 GRANT EXECUTE ON FUNCTION public.xinsight_initialize_connector_control_credential(text,text) TO xinsight_workspace;
END IF; END $$;
