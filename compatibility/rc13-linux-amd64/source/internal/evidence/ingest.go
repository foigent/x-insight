package evidence

import "fmt"

type IngestResult struct {
	Outcome Outcome
	Reason  string
	Hash    string
}

type InMemoryIngester struct {
	validator *SchemaValidator
	seen      map[string]string
}

func NewInMemoryIngester(validator *SchemaValidator) *InMemoryIngester {
	return &InMemoryIngester{
		validator: validator,
		seen:      map[string]string{},
	}
}

func (i *InMemoryIngester) Ingest(event map[string]any) IngestResult {
	if event == nil {
		return IngestResult{Outcome: OutcomeRejected, Reason: "schema_validation_failed"}
	}

	sourceRef := stringField(event, "source_ref")
	if sourceRef == "" {
		return IngestResult{Outcome: OutcomeRejected, Reason: "missing_source_ref"}
	}
	idempotencyKey := stringField(event, "idempotency_key")
	if idempotencyKey == "" {
		return IngestResult{Outcome: OutcomeRejected, Reason: "missing_idempotency_key"}
	}

	hash, err := GenerateEventHash(event)
	if err != nil {
		return IngestResult{Outcome: OutcomeRejected, Reason: fmt.Sprintf("hash_failed: %v", err)}
	}
	event["event_hash"] = hash

	if err := i.validator.Validate(event); err != nil {
		return IngestResult{Outcome: OutcomeRejected, Reason: "schema_validation_failed: " + err.Error()}
	}
	if err := ValidateProjectionConsistency(event); err != nil {
		return IngestResult{Outcome: OutcomeRejected, Reason: "projection_mismatch: " + err.Error()}
	}
	if findings := DetectSensitive(event["raw_source_excerpt"]); len(findings) > 0 {
		return IngestResult{Outcome: OutcomeQuarantined, Reason: findings[0].Reason, Hash: hash}
	}

	if previousHash, ok := i.seen[idempotencyKey]; ok {
		if previousHash == hash {
			return IngestResult{Outcome: OutcomeDuplicate, Reason: "duplicate_noop", Hash: hash}
		}
		return IngestResult{Outcome: OutcomeQuarantined, Reason: "idempotency_conflict", Hash: hash}
	}
	i.seen[idempotencyKey] = hash
	return IngestResult{Outcome: OutcomeAccepted, Reason: "accepted", Hash: hash}
}
