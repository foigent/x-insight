-- A rollback cannot silently forget user communication preferences or inflight work.
DO $$ BEGIN IF EXISTS(SELECT 1 FROM connector_sync_controls WHERE request_id<>'')
 OR EXISTS(SELECT 1 FROM connector_sync_slots WHERE busy)
 OR EXISTS(SELECT 1 FROM connector_sync_runs) THEN
 RAISE EXCEPTION 'connector sync state requires explicit preservation before rollback'; END IF; END $$;
DROP TRIGGER connector_sync_default ON connector_installations;
DROP FUNCTION public.xinsight_init_connector_sync();
DROP FUNCTION public.xinsight_sync_units(text);
DROP FUNCTION public.xinsight_sync_receiver(text);
DROP FUNCTION public.xinsight_sync_scope(text,text,text,boolean);
DROP TABLE connector_sync_runs;
DROP TABLE connector_sync_slots;
DROP TABLE connector_sync_controls;
