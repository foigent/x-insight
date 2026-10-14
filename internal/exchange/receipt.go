package exchange

const (
	ImportReceiptSchemaVersion = "xinsight.import-receipt.v1"

	ReceiptStatusValidated = "validated"
	ReceiptStatusPersisted = "persisted"
	ReceiptStatusRejected  = "rejected"

	WarningApprovalManifestHashMissing = "approval_manifest_hash_missing"
	WarningPersistenceNotRequested     = "persistence_not_requested"
	WarningSourceCaptureGapsPresent    = "source_capture_gaps_present"
	WarningSourceExchangeIDMissing     = "source_exchange_id_missing"
)

type ImportReceipt struct {
	SchemaVersion string          `json:"schema_version"`
	ExchangeID    string          `json:"exchange_id,omitempty"`
	Source        ReceiptSource   `json:"source"`
	Artifact      ReceiptArtifact `json:"artifact"`
	Result        ReceiptResult   `json:"result"`
	CheckedAt     string          `json:"checked_at"`
	WarningCodes  []string        `json:"warning_codes,omitempty"`
}

type ReceiptSource struct {
	System   string `json:"system"`
	Instance string `json:"instance"`
}

type ReceiptArtifact struct {
	SchemaVersion          string `json:"schema_version"`
	SHA256                 string `json:"sha256"`
	ApprovalManifestSHA256 string `json:"approval_manifest_sha256,omitempty"`
	GeneratedAt            string `json:"generated_at"`
}

type ReceiptResult struct {
	Status                 string `json:"status"`
	RequestIDs             int    `json:"request_ids"`
	InputEvents            int    `json:"input_events"`
	Accepted               int    `json:"accepted"`
	DuplicateNoop          int    `json:"duplicate_noop"`
	Rejected               int    `json:"rejected"`
	Quarantined            int    `json:"quarantined"`
	RerunDuplicateNoop     int    `json:"rerun_duplicate_noop"`
	CaptureGapEvents       int    `json:"capture_gap_events"`
	PersistedInserted      *int   `json:"persisted_inserted,omitempty"`
	PersistedDuplicateNoop *int   `json:"persisted_duplicate_noop,omitempty"`
	PersistenceConflicts   *int   `json:"persistence_conflicts,omitempty"`
	ReadbackEvents         *int   `json:"readback_events,omitempty"`
	ReadbackCaptureGaps    *int   `json:"readback_capture_gaps,omitempty"`
}
