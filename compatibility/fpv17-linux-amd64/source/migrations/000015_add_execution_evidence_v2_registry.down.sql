BEGIN;

DELETE FROM adapter_manifests
WHERE id = '22222222-2222-4222-8222-222222222225'
  AND adapter_id = 'adapter-unirouter:22222222-2222-4222-8222-222222222225'
  AND adapter_version = 'unirouter.execution-evidence.bundle.v2';

UPDATE source_systems
SET source_version = 'unirouter.completed-execution.v1'
WHERE id = '11111111-1111-1111-1111-111111111111'
  AND source_key = 'source-system:11111111-1111-1111-1111-111111111111'
  AND source_type = 'unirouter'
  AND source_version = 'unirouter.execution-evidence.bundle.v2';

COMMIT;
