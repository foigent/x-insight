-- A one-time, owner-controlled OpenClaw onboarding exchange. Neither the
-- invitation bearer nor the host-generated ingest bearer is stored in clear.
CREATE TABLE work_event_install_invitations (
  invitation_id text PRIMARY KEY CHECK(invitation_id ~ '^xiei_[a-f0-9]{32}$'),
  invitation_digest text NOT NULL UNIQUE CHECK(invitation_digest ~ '^sha256:[a-f0-9]{64}$'),
  actor_subject text NOT NULL CHECK(length(actor_subject) BETWEEN 1 AND 128),
  display_name text NOT NULL CHECK(length(display_name) BETWEEN 1 AND 128),
  source_instance text NOT NULL UNIQUE CHECK(source_instance ~ '^openclaw-[a-f0-9]{32}$'),
  state text NOT NULL CHECK(state IN ('invited','candidates','approved')),
  candidate_sha256 text CHECK(candidate_sha256 ~ '^sha256:[a-f0-9]{64}$'),
  candidates jsonb,
  host_token_digest text CHECK(host_token_digest ~ '^sha256:[a-f0-9]{64}$'),
  host_token_prefix text,
  approved_source_system_id uuid REFERENCES source_systems(id) ON DELETE RESTRICT,
  approved_service_identity_id text REFERENCES service_identities(id) ON DELETE RESTRICT,
  approved_bindings jsonb,
  future_agents_allowed boolean NOT NULL DEFAULT false,
  future_candidates jsonb NOT NULL DEFAULT '[]'::jsonb,
  excluded_future_project_keys jsonb NOT NULL DEFAULT '[]'::jsonb,
  latest_status jsonb,
  status_received_at timestamptz,
  renewal_old_digest text CHECK(renewal_old_digest ~ '^sha256:[a-f0-9]{64}$'),
  renewal_new_digest text CHECK(renewal_new_digest ~ '^sha256:[a-f0-9]{64}$'),
  renewal_expires_at timestamptz,
  expires_at timestamptz NOT NULL,
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  CHECK((state='invited')=(candidates IS NULL)),
  CHECK((candidates IS NULL)=(candidate_sha256 IS NULL)),
  CHECK((candidates IS NULL)=(host_token_digest IS NULL)),
  CHECK((candidates IS NULL)=(host_token_prefix IS NULL)),
  CHECK((state='approved')=(approved_source_system_id IS NOT NULL)),
  CHECK((state='approved')=(approved_service_identity_id IS NOT NULL)),
  CHECK((state='approved')=(approved_bindings IS NOT NULL)),
  CHECK((renewal_old_digest IS NULL)=(renewal_new_digest IS NULL)),
  CHECK((renewal_old_digest IS NULL)=(renewal_expires_at IS NULL)),
  CHECK((latest_status IS NULL)=(status_received_at IS NULL)),
  CHECK(latest_status IS NULL OR (state='approved' AND jsonb_typeof(latest_status)='object'))
);
CREATE INDEX work_event_install_invitations_state ON work_event_install_invitations(state,expires_at);
CREATE UNIQUE INDEX work_event_install_invitations_identity ON work_event_install_invitations(approved_service_identity_id)
WHERE approved_service_identity_id IS NOT NULL;
REVOKE ALL ON TABLE work_event_install_invitations FROM PUBLIC;
DO $$ BEGIN
  IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'xinsight_workspace') THEN
    REVOKE ALL ON TABLE work_event_install_invitations FROM xinsight_workspace;
  END IF;
  IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'xinsight_importer') THEN
    REVOKE ALL ON TABLE work_event_install_invitations FROM xinsight_importer;
  END IF;
  IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'xinsight_reader') THEN
    REVOKE ALL ON TABLE work_event_install_invitations FROM xinsight_reader;
  END IF;
END $$;

CREATE FUNCTION public.xinsight_managed_work_install_id(p_identity_id text)
RETURNS text LANGUAGE sql STABLE SECURITY DEFINER SET search_path = pg_catalog AS $$
  SELECT invitation_id FROM public.work_event_install_invitations
    WHERE approved_service_identity_id = p_identity_id AND state = 'approved';
$$;
REVOKE ALL ON FUNCTION public.xinsight_managed_work_install_id(text) FROM PUBLIC;
DO $$ BEGIN
  IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'xinsight_workspace') THEN
    GRANT EXECUTE ON FUNCTION public.xinsight_managed_work_install_id(text) TO xinsight_workspace;
  END IF;
END $$;
