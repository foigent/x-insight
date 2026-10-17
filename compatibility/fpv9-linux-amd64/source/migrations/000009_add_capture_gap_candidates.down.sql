DROP INDEX IF EXISTS idx_capture_gaps_candidate_gap_types;

ALTER TABLE capture_gaps
  DROP COLUMN IF EXISTS candidate_gap_types;
