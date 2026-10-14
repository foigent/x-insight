DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM connector_control_operations)
    OR EXISTS (SELECT 1 FROM connector_installation_invitations)
    OR EXISTS (SELECT 1 FROM connector_installations)
    OR EXISTS (
      SELECT 1
      FROM service_identities
      WHERE permissions && ARRAY['connector.control.poll', 'connector.control.ack']::text[]
    ) THEN
    RAISE EXCEPTION 'cannot remove connector installation support while connector state exists';
  END IF;
END $$;

DROP TRIGGER service_identity_tokens_terminal_update_guard ON service_identity_tokens;
DROP TRIGGER service_identities_terminal_update_guard ON service_identities;
DROP FUNCTION public.xinsight_guard_service_credential_terminal_update();

DROP TABLE connector_control_operations;
DROP TABLE connector_installation_invitations;
DROP FUNCTION public.xinsight_redeem_connector_invitation(
  text, text, text, timestamptz
);
DROP TABLE connector_installations;

ALTER TABLE service_identities
  DROP CONSTRAINT service_identities_permissions_check,
  DROP CONSTRAINT service_identities_creator_check;

ALTER TABLE service_identities
  ADD CONSTRAINT service_identities_permissions_check CHECK (
    cardinality(permissions) BETWEEN 1 AND 4
    AND permissions <@ ARRAY['evidence.read', 'evidence.cite']::text[]
  ),
  ADD CONSTRAINT service_identities_creator_check CHECK (
    created_by_subject = 'root'
    OR created_by_subject ~ '^xs_[a-z0-9]{16,64}$'
  );
