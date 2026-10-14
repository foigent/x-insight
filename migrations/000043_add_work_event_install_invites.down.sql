DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM work_event_install_invitations) THEN
   RAISE EXCEPTION 'work event installation invitations require explicit retention decision';
 END IF;
END $$;
DROP FUNCTION IF EXISTS public.xinsight_managed_work_install_id(text);
DROP TABLE work_event_install_invitations;
