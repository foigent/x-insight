CREATE TABLE investigation_queue_filters (
 id text PRIMARY KEY CHECK (id ~ '^xqf_[A-Za-z0-9_-]{22}$'),
 owner_subject text NOT NULL CHECK (owner_subject ~ '^xs_[a-z0-9]{16,64}$'),
 revision bigint NOT NULL CHECK (revision > 0),
 record jsonb NOT NULL,
 CHECK (COALESCE(jsonb_typeof(record)='object' AND record->>'id'=id AND record->>'owner_subject'=owner_subject
   AND record->>'schema_version'='xinsight.investigation-queue-filter.v1'
   AND (record->>'revision')::bigint=revision AND jsonb_typeof(record->'deleted')='boolean'
   AND jsonb_typeof(record->'filters')='object' AND jsonb_typeof(record->'name')='string' AND length(record->>'name') BETWEEN 1 AND 120, false))
);
CREATE UNIQUE INDEX investigation_queue_filters_owner_name ON investigation_queue_filters(owner_subject,(record->>'name')) WHERE record->>'deleted'='false';
CREATE TABLE investigation_queue_filter_operations (
 owner_subject text NOT NULL,
 operation_id uuid NOT NULL,
 filter_id text NOT NULL REFERENCES investigation_queue_filters(id) ON DELETE RESTRICT,
 result_revision bigint NOT NULL CHECK (result_revision > 0),
 request_sha256 text NOT NULL CHECK (request_sha256 ~ '^[a-f0-9]{64}$'),
 receipt jsonb NOT NULL CHECK (jsonb_typeof(receipt)='object'),
 PRIMARY KEY(owner_subject,operation_id),
 UNIQUE(filter_id,result_revision)
);
