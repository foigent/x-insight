DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM review_body_copy_attempts) OR EXISTS(SELECT 1 FROM review_body_objects) OR EXISTS(SELECT 1 FROM review_body_operations) OR
    EXISTS(SELECT 1 FROM review_body_project_usage) OR EXISTS(SELECT 1 FROM review_body_ingest_bindings) OR
    EXISTS(SELECT 1 FROM review_capture_defaults) OR EXISTS(SELECT 1 FROM review_capture_exceptions) OR
    EXISTS(SELECT 1 FROM review_capture_projects) OR EXISTS(SELECT 1 FROM review_capture_project_bindings) OR
    EXISTS(SELECT 1 FROM review_capture_ur_operations) THEN
   RAISE EXCEPTION 'review body custody or policy history requires explicit retention decision';
 END IF;
END $$;
DROP TABLE review_body_copy_attempts;
DROP TABLE review_body_objects;
DROP TABLE review_body_operations;
DROP TABLE review_body_project_usage;
DROP TABLE review_body_ingest_bindings;
DROP TABLE review_capture_ur_operations;
DROP TABLE review_capture_project_bindings;
DROP TABLE review_capture_projects;
DROP TABLE review_capture_exceptions;
DROP TABLE review_capture_defaults;
DROP FUNCTION guard_review_body_authority();
