ALTER TABLE capture_gaps
  ADD COLUMN IF NOT EXISTS candidate_gap_types text[] NOT NULL DEFAULT ARRAY[]::text[];

CREATE INDEX IF NOT EXISTS idx_capture_gaps_candidate_gap_types
  ON capture_gaps USING GIN (candidate_gap_types);
