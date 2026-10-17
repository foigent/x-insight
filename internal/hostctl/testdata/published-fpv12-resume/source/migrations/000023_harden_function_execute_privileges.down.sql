ALTER DEFAULT PRIVILEGES FOR ROLE xinsight_admin IN SCHEMA public
  GRANT EXECUTE ON FUNCTIONS TO PUBLIC;

GRANT EXECUTE ON FUNCTION public.reject_connector_checkpoint_epoch_history_mutation()
TO PUBLIC;
