package unirouter

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"time"
	"unicode/utf16"
	"unicode/utf8"
)

const (
	foigentEnvelopeSpecVersion = "foigent.event-envelope.v1"
	foigentEventType           = "model.request.completed"
	foigentEventSchemaRef      = "foigent://events/model/request-completed/v1"
	foigentConnectorID         = "foigent.unirouter.completed-execution"
	foigentConnectorVersion    = "1.0.0"
	foigentIdempotencyDomain   = "foigent.event-idempotency.v1"
	foigentEnvelopeMaxBytes    = 128 << 10
)

var (
	foigentDigestPattern  = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	foigentCounterPattern = regexp.MustCompile(
		`^(0|[1-9][0-9]{0,18})$`,
	)
	foigentSharedGapOrder = [...]string{
		"call_log",
		"client_ip_presence",
		"request_trace",
		"upstream_error_presence",
	}
	foigentFieldGroups = map[string]string{
		"actor":                     "declared",
		"correlation":               "direct",
		"data_policy":               "declared",
		"identity":                  "direct",
		"payload.capture_gaps":      "direct",
		"payload.money":             "direct",
		"payload.terminal":          "direct",
		"payload.timeline_complete": "direct",
		"payload.usage":             "direct",
		"scope":                     "direct",
		"subject":                   "derived",
		"time.occurred_at":          "derived",
		"time.terminal_at":          "direct",
	}
	foigentGapFieldProvenance = map[string]string{
		"affected_claim":       "direct",
		"reason_code":          "direct",
		"gap_id":               "derived",
		"parent_event_id":      "derived",
		"scope":                "derived",
		"correlation":          "derived",
		"observed_at":          "derived",
		"gap_type":             "declared",
		"severity":             "declared",
		"payload_capture_mode": "declared",
	}
)

// FoigentReferenceEnvelopeResult is the non-runtime Phase 2A compatibility
// output for the existing completed-execution Source DTO.
type FoigentReferenceEnvelopeResult struct {
	HashInput      map[string]any
	CanonicalBytes []byte
	Envelope       map[string]any
	EnvelopeBytes  []byte
	EnvelopeHash   string
	IdempotencyKey string
	SourceHash     string
}

// FoigentProjectionResult retains shared identity for compatibility evidence
// while returning the exact existing M0 projection.
type FoigentProjectionResult struct {
	CompletedExecution CompletedExecutionMapResult
	EnvelopeHash       string
	SharedIdempotency  string
}

type foigentEnvelopeV1 struct {
	SpecVersion   string                    `json:"spec_version"`
	EventID       string                    `json:"event_id"`
	EventType     string                    `json:"event_type"`
	SchemaRef     string                    `json:"schema_ref"`
	SchemaVersion string                    `json:"schema_version"`
	Producer      foigentProducerV1         `json:"producer"`
	Scope         foigentScopeV1            `json:"scope"`
	Subject       foigentSubjectV1          `json:"subject"`
	Actor         foigentActorV1            `json:"actor"`
	Time          foigentTimeV1             `json:"time"`
	Correlation   foigentCorrelationV1      `json:"correlation"`
	Identity      foigentIdentityV1         `json:"identity"`
	Integrity     foigentIntegrityV1        `json:"integrity"`
	DataPolicy    foigentDataPolicyV1       `json:"data_policy"`
	Provenance    foigentProvenanceV1       `json:"provenance"`
	Payload       foigentCompletedPayloadV1 `json:"payload"`
}

type foigentProducerV1 struct {
	Product          string `json:"product"`
	Instance         string `json:"instance"`
	ConnectorID      string `json:"connector_id"`
	ConnectorVersion string `json:"connector_version"`
}

type foigentScopeV1 struct {
	OrgRef     string `json:"org_ref"`
	ProjectRef string `json:"project_ref"`
}

type foigentSubjectV1 struct {
	Type string `json:"type"`
	Ref  string `json:"ref"`
}

type foigentActorV1 struct {
	Type string `json:"type"`
}

type foigentTimeV1 struct {
	OccurredAt string `json:"occurred_at"`
	TerminalAt string `json:"terminal_at"`
}

type foigentCorrelationV1 struct {
	RequestRef string `json:"request_ref"`
}

type foigentIdentityV1 struct {
	IdempotencyKey string `json:"idempotency_key"`
	SourceRevision string `json:"source_revision"`
}

type foigentIntegrityV1 struct {
	SourceHash   string `json:"source_hash"`
	EnvelopeHash string `json:"envelope_hash"`
}

type foigentDataPolicyV1 struct {
	PayloadCaptureMode string `json:"payload_capture_mode"`
	RawPayloadPresent  bool   `json:"raw_payload_present"`
}

type foigentProvenanceV1 struct {
	AdapterFidelity string            `json:"adapter_fidelity"`
	FieldGroups     map[string]string `json:"field_groups"`
}

type foigentCompletedPayloadV1 struct {
	Terminal         foigentTerminalV1     `json:"terminal"`
	Usage            foigentUsageV1        `json:"usage"`
	Money            foigentMoneyV1        `json:"money"`
	TimelineComplete bool                  `json:"timeline_complete"`
	CaptureGaps      []foigentCaptureGapV1 `json:"capture_gaps"`
}

type foigentTerminalV1 struct {
	Status string `json:"status"`
}

type foigentUsageV1 struct {
	Model                string `json:"model"`
	PromptTokens         string `json:"prompt_tokens"`
	UncachedPromptTokens string `json:"uncached_prompt_tokens"`
	CacheReadTokens      string `json:"cache_read_tokens"`
	CacheCreationTokens  string `json:"cache_creation_tokens"`
	CompletionTokens     string `json:"completion_tokens"`
	Source               string `json:"source"`
	Authority            string `json:"authority"`
	Purpose              string `json:"purpose"`
	Finality             string `json:"finality"`
	ContractVersion      string `json:"contract_version"`
	EstimateVersion      string `json:"estimate_version,omitempty"`
	CacheUsageSchema     string `json:"cache_usage_schema"`
	CacheUsageStatus     string `json:"cache_usage_status"`
}

type foigentMoneyV1 struct {
	Schema               string `json:"schema"`
	Currency             string `json:"currency"`
	AmountScale          int64  `json:"amount_scale"`
	CalculatedCostMicros string `json:"calculated_cost_micros"`
	ChargedCostMicros    string `json:"charged_cost_micros"`
	PricingVersion       string `json:"pricing_version"`
}

type foigentCaptureGapV1 struct {
	GapID              string               `json:"gap_id"`
	ParentEventID      string               `json:"parent_event_id"`
	Scope              foigentScopeV1       `json:"scope"`
	Correlation        foigentCorrelationV1 `json:"correlation"`
	AffectedClaim      string               `json:"affected_claim"`
	GapType            string               `json:"gap_type"`
	ReasonCode         string               `json:"reason_code"`
	Severity           string               `json:"severity"`
	ObservedAt         string               `json:"observed_at"`
	PayloadCaptureMode string               `json:"payload_capture_mode"`
	FieldProvenance    map[string]string    `json:"field_provenance"`
}

// buildFoigentReferenceEnvelopeV1 independently reproduces the frozen Source
// bridge for dual-path CI. It is intentionally unexported and is not called by
// the live pull worker; UniRouter remains the Source projection owner.
func buildFoigentReferenceEnvelopeV1(
	sourceBytes []byte,
	sourceInstance string,
) (FoigentReferenceEnvelopeResult, error) {
	snapshot, sourceHash, err := DecodeCompletedExecutionV1(sourceBytes)
	if err != nil {
		return FoigentReferenceEnvelopeResult{}, err
	}
	if err := validateCompletedExecutionText("producer.instance", sourceInstance, 1, 128); err != nil {
		return FoigentReferenceEnvelopeResult{}, err
	}
	terminalAt := snapshot.Terminal.AvailableAt.UTC()
	if !terminalAt.Equal(terminalAt.Truncate(time.Microsecond)) {
		return FoigentReferenceEnvelopeResult{}, errors.New(
			"completed-execution terminal time loses sub-microsecond precision",
		)
	}
	terminalText := terminalAt.Format("2006-01-02T15:04:05.000000Z")
	orgRef := strconv.FormatInt(snapshot.OrgID, 10)
	projectRef := strconv.FormatInt(snapshot.ProjectID, 10)
	idempotency, err := foigentIdempotencyKey(
		"unirouter",
		sourceInstance,
		"",
		orgRef,
		projectRef,
		foigentEventType,
		snapshot.RequestID,
	)
	if err != nil {
		return FoigentReferenceEnvelopeResult{}, err
	}

	sourceGaps := make(map[string]CompletedExecutionCaptureGap, len(snapshot.CaptureGaps))
	for _, gap := range snapshot.CaptureGaps {
		sourceGaps[gap.Source] = gap
	}
	gaps := make([]any, 0, len(foigentSharedGapOrder))
	for _, claim := range foigentSharedGapOrder {
		gap, ok := sourceGaps[claim]
		if !ok {
			return FoigentReferenceEnvelopeResult{}, fmt.Errorf(
				"completed-execution source gap %q is missing",
				claim,
			)
		}
		gaps = append(gaps, foigentCaptureGapMap(
			snapshot.RequestID,
			orgRef,
			projectRef,
			terminalText,
			gap.Source,
			gap.Reason,
		))
	}
	usage := map[string]any{
		"model":                  snapshot.Usage.Model,
		"prompt_tokens":          strconv.FormatInt(snapshot.Usage.PromptTokens, 10),
		"uncached_prompt_tokens": strconv.FormatInt(snapshot.Usage.UncachedPromptTokens, 10),
		"cache_read_tokens":      strconv.FormatInt(snapshot.Usage.CacheReadTokens, 10),
		"cache_creation_tokens":  strconv.FormatInt(snapshot.Usage.CacheCreationTokens, 10),
		"completion_tokens":      strconv.FormatInt(snapshot.Usage.CompletionTokens, 10),
		"source":                 snapshot.Usage.Source,
		"authority":              snapshot.Usage.Authority,
		"purpose":                snapshot.Usage.Purpose,
		"finality":               snapshot.Usage.Finality,
		"contract_version":       snapshot.Usage.ContractVersion,
		"cache_usage_schema":     snapshot.Usage.CacheUsageSchema,
		"cache_usage_status":     snapshot.Usage.CacheUsageStatus,
	}
	if snapshot.Usage.EstimateVersion != "" {
		usage["estimate_version"] = snapshot.Usage.EstimateVersion
	}
	hashInput := map[string]any{
		"spec_version":   foigentEnvelopeSpecVersion,
		"event_id":       snapshot.RequestID,
		"event_type":     foigentEventType,
		"schema_ref":     foigentEventSchemaRef,
		"schema_version": "1",
		"producer": map[string]any{
			"product":           "unirouter",
			"instance":          sourceInstance,
			"connector_id":      foigentConnectorID,
			"connector_version": foigentConnectorVersion,
		},
		"scope": map[string]any{
			"org_ref":     orgRef,
			"project_ref": projectRef,
		},
		"subject": map[string]any{"type": "request", "ref": snapshot.RequestID},
		"actor":   map[string]any{"type": "system"},
		"time": map[string]any{
			"occurred_at": terminalText,
			"terminal_at": terminalText,
		},
		"correlation": map[string]any{"request_ref": snapshot.RequestID},
		"identity": map[string]any{
			"idempotency_key": idempotency,
			"source_revision": CompletedExecutionRevisionV1,
		},
		"integrity": map[string]any{"source_hash": sourceHash},
		"data_policy": map[string]any{
			"payload_capture_mode": "metadata_only",
			"raw_payload_present":  false,
		},
		"provenance": map[string]any{
			"adapter_fidelity": "F2",
			"field_groups":     stringMapToAny(foigentFieldGroups),
		},
		"payload": map[string]any{
			"terminal": map[string]any{"status": string(snapshot.Terminal.Status)},
			"usage":    usage,
			"money": map[string]any{
				"schema":                 snapshot.Money.Schema,
				"currency":               snapshot.Money.Currency,
				"amount_scale":           snapshot.Money.AmountScale,
				"calculated_cost_micros": snapshot.Money.CalculatedCostMicros,
				"charged_cost_micros":    snapshot.Money.ChargedCostMicros,
				"pricing_version":        snapshot.Money.PricingVersion,
			},
			"timeline_complete": true,
			"capture_gaps":      gaps,
		},
	}
	canonical, err := canonicalFoigentJSON(hashInput)
	if err != nil {
		return FoigentReferenceEnvelopeResult{}, err
	}
	digest := sha256.Sum256(canonical)
	envelopeHash := "sha256:" + hex.EncodeToString(digest[:])
	envelope := cloneFoigentMap(hashInput)
	envelope["integrity"].(map[string]any)["envelope_hash"] = envelopeHash
	envelopeBytes, err := canonicalFoigentJSON(envelope)
	if err != nil {
		return FoigentReferenceEnvelopeResult{}, err
	}
	return FoigentReferenceEnvelopeResult{
		HashInput:      hashInput,
		CanonicalBytes: canonical,
		Envelope:       envelope,
		EnvelopeBytes:  envelopeBytes,
		EnvelopeHash:   envelopeHash,
		IdempotencyKey: idempotency,
		SourceHash:     sourceHash,
	}, nil
}

// BuildFoigentReferenceEnvelopeV1 is the Phase 2C product-local runtime seam.
// It preserves the exact Phase 2A compatibility projection and performs no
// persistence, delivery, or Checkpoint mutation.
func BuildFoigentReferenceEnvelopeV1(
	sourceBytes []byte,
	sourceInstance string,
) (FoigentReferenceEnvelopeResult, error) {
	return buildFoigentReferenceEnvelopeV1(sourceBytes, sourceInstance)
}

// MapFoigentReferenceEnvelopeV1 validates a shared Envelope and projects it
// through the existing M0 builder. It does not persist and it does not own
// delivery or checkpoint state.
func MapFoigentReferenceEnvelopeV1(
	envelopeBytes []byte,
	options CompletedExecutionMapOptions,
) (FoigentProjectionResult, error) {
	envelope, err := decodeFoigentReferenceEnvelopeV1(envelopeBytes)
	if err != nil {
		return FoigentProjectionResult{}, err
	}
	if options.SourceInstance == "" {
		options.SourceInstance = envelope.Producer.Instance
	} else if options.SourceInstance != envelope.Producer.Instance {
		return FoigentProjectionResult{}, errors.New(
			"Foigent Envelope producer instance conflicts with projection context",
		)
	}
	snapshot, err := completedExecutionFromFoigentEnvelope(envelope)
	if err != nil {
		return FoigentProjectionResult{}, err
	}
	mapped, err := mapCompletedExecutionSnapshot(snapshot, envelope.Integrity.SourceHash, options)
	if err != nil {
		return FoigentProjectionResult{}, err
	}
	return FoigentProjectionResult{
		CompletedExecution: mapped,
		EnvelopeHash:       envelope.Integrity.EnvelopeHash,
		SharedIdempotency:  envelope.Identity.IdempotencyKey,
	}, nil
}

// mapCompletedExecutionViaFoigentBridgeV1 is the CI-only dual-path bridge used
// to prove that Source DTO -> shared Envelope -> current M0 is byte-compatible
// with the existing direct DTO -> M0 projection.
func mapCompletedExecutionViaFoigentBridgeV1(
	sourceBytes []byte,
	options CompletedExecutionMapOptions,
) (FoigentProjectionResult, error) {
	envelope, err := buildFoigentReferenceEnvelopeV1(sourceBytes, options.SourceInstance)
	if err != nil {
		return FoigentProjectionResult{}, err
	}
	return MapFoigentReferenceEnvelopeV1(envelope.EnvelopeBytes, options)
}

func decodeFoigentReferenceEnvelopeV1(payload []byte) (foigentEnvelopeV1, error) {
	if len(payload) == 0 || len(payload) > foigentEnvelopeMaxBytes {
		return foigentEnvelopeV1{}, errors.New("Foigent Envelope size is invalid")
	}
	if !utf8.Valid(payload) {
		return foigentEnvelopeV1{}, errors.New("Foigent Envelope must be valid UTF-8")
	}
	if err := scanCompletedExecutionJSON(payload); err != nil {
		return foigentEnvelopeV1{}, fmt.Errorf("scan Foigent Envelope: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var envelope foigentEnvelopeV1
	if err := decoder.Decode(&envelope); err != nil {
		return foigentEnvelopeV1{}, fmt.Errorf("decode Foigent Envelope: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return foigentEnvelopeV1{}, errors.New("Foigent Envelope contains trailing content")
	}
	if err := validateFoigentReferenceEnvelopeV1(envelope); err != nil {
		return foigentEnvelopeV1{}, err
	}

	rawDecoder := json.NewDecoder(bytes.NewReader(payload))
	rawDecoder.UseNumber()
	var raw map[string]any
	if err := rawDecoder.Decode(&raw); err != nil {
		return foigentEnvelopeV1{}, err
	}
	if err := validateFoigentReferenceRequiredShape(raw); err != nil {
		return foigentEnvelopeV1{}, err
	}
	integrity, ok := raw["integrity"].(map[string]any)
	if !ok {
		return foigentEnvelopeV1{}, errors.New("Foigent Envelope integrity must be an object")
	}
	delete(integrity, "envelope_hash")
	canonical, err := canonicalFoigentJSON(raw)
	if err != nil {
		return foigentEnvelopeV1{}, err
	}
	digest := sha256.Sum256(canonical)
	if actual := "sha256:" + hex.EncodeToString(digest[:]); actual != envelope.Integrity.EnvelopeHash {
		return foigentEnvelopeV1{}, errors.New("Foigent Envelope hash mismatch")
	}
	return envelope, nil
}

func validateFoigentReferenceEnvelopeV1(envelope foigentEnvelopeV1) error {
	if envelope.SpecVersion != foigentEnvelopeSpecVersion ||
		envelope.EventType != foigentEventType ||
		envelope.SchemaRef != foigentEventSchemaRef ||
		envelope.SchemaVersion != "1" {
		return errors.New("Foigent Envelope protocol identity is invalid")
	}
	if envelope.Producer.Product != "unirouter" ||
		envelope.Producer.ConnectorID != foigentConnectorID ||
		envelope.Producer.ConnectorVersion != foigentConnectorVersion {
		return errors.New("Foigent Envelope producer identity is invalid")
	}
	if err := validateCompletedExecutionText("event_id", envelope.EventID, 1, 64); err != nil {
		return err
	}
	if err := validateCompletedExecutionText("producer.instance", envelope.Producer.Instance, 1, 128); err != nil {
		return err
	}
	if envelope.Subject.Type != "request" ||
		envelope.Subject.Ref != envelope.EventID ||
		envelope.Actor.Type != "system" ||
		envelope.Correlation.RequestRef != envelope.EventID {
		return errors.New("Foigent Envelope subject, actor or correlation binding is invalid")
	}
	orgID, err := parsePositiveFoigentRef("scope.org_ref", envelope.Scope.OrgRef)
	if err != nil {
		return err
	}
	projectID, err := parsePositiveFoigentRef("scope.project_ref", envelope.Scope.ProjectRef)
	if err != nil {
		return err
	}
	if orgID == 0 || projectID == 0 {
		return errors.New("Foigent Envelope scope is invalid")
	}
	terminalAt, err := parseFoigentMicrosecondTime(envelope.Time.TerminalAt)
	if err != nil {
		return err
	}
	if envelope.Time.OccurredAt != envelope.Time.TerminalAt {
		return errors.New("Foigent Envelope occurred_at must equal terminal_at")
	}
	if terminalAt.IsZero() {
		return errors.New("Foigent Envelope terminal_at is required")
	}
	expectedIdempotency, err := foigentIdempotencyKey(
		envelope.Producer.Product,
		envelope.Producer.Instance,
		"",
		envelope.Scope.OrgRef,
		envelope.Scope.ProjectRef,
		envelope.EventType,
		envelope.EventID,
	)
	if err != nil {
		return err
	}
	if envelope.Identity.IdempotencyKey != expectedIdempotency ||
		envelope.Identity.SourceRevision != CompletedExecutionRevisionV1 {
		return errors.New("Foigent Envelope shared identity is invalid")
	}
	if !foigentDigestPattern.MatchString(envelope.Integrity.SourceHash) ||
		!foigentDigestPattern.MatchString(envelope.Integrity.EnvelopeHash) {
		return errors.New("Foigent Envelope integrity digest is invalid")
	}
	if envelope.DataPolicy.PayloadCaptureMode != "metadata_only" ||
		envelope.DataPolicy.RawPayloadPresent {
		return errors.New("Foigent Envelope data policy is invalid")
	}
	if envelope.Provenance.AdapterFidelity != "F2" ||
		!equalStringMap(envelope.Provenance.FieldGroups, foigentFieldGroups) {
		return errors.New("Foigent Envelope provenance is invalid")
	}
	if envelope.Payload.Terminal.Status != "settled" &&
		envelope.Payload.Terminal.Status != "cancelled" {
		return errors.New("Foigent Envelope terminal status is invalid")
	}
	if !envelope.Payload.TimelineComplete {
		return errors.New("Foigent Envelope timeline_complete must be true")
	}
	if err := validateFoigentUsage(envelope.Payload.Usage); err != nil {
		return err
	}
	if envelope.Payload.Money.Schema != MoneySchemaMicroCNY ||
		envelope.Payload.Money.Currency != MoneyCurrencyCNY ||
		envelope.Payload.Money.AmountScale != MoneyAmountScale ||
		!completedExecutionAmountPattern.MatchString(envelope.Payload.Money.CalculatedCostMicros) ||
		!completedExecutionAmountPattern.MatchString(envelope.Payload.Money.ChargedCostMicros) {
		return errors.New("Foigent Envelope money is invalid")
	}
	if len(envelope.Payload.CaptureGaps) != len(foigentSharedGapOrder) {
		return errors.New("Foigent Envelope Capture Gap count is invalid")
	}
	for index, claim := range foigentSharedGapOrder {
		gap := envelope.Payload.CaptureGaps[index]
		if gap.AffectedClaim != claim ||
			gap.GapID != envelope.EventID+":gap:"+claim ||
			gap.ParentEventID != envelope.EventID ||
			gap.Scope != envelope.Scope ||
			gap.Correlation.RequestRef != envelope.EventID ||
			gap.GapType != completedExecutionUnavailableCaptureGapType ||
			gap.ReasonCode != completedExecutionCaptureGapReason ||
			gap.Severity != "warning" ||
			gap.ObservedAt != envelope.Time.TerminalAt ||
			gap.PayloadCaptureMode != "none" ||
			!equalStringMap(gap.FieldProvenance, foigentGapFieldProvenance) {
			return fmt.Errorf("Foigent Envelope Capture Gap %q is invalid", claim)
		}
	}
	return nil
}

func validateFoigentReferenceRequiredShape(envelope map[string]any) error {
	require := func(value any, path string, keys ...string) (map[string]any, error) {
		object, ok := value.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%s must be an object", path)
		}
		for _, key := range keys {
			if _, exists := object[key]; !exists {
				return nil, fmt.Errorf("%s.%s is required", path, key)
			}
		}
		return object, nil
	}
	top, err := require(envelope, "$",
		"spec_version", "event_id", "event_type", "schema_ref", "schema_version",
		"producer", "scope", "subject", "actor", "time", "correlation",
		"identity", "integrity", "data_policy", "provenance", "payload",
	)
	if err != nil {
		return err
	}
	checks := []struct {
		value any
		path  string
		keys  []string
	}{
		{top["producer"], "$.producer", []string{"product", "instance", "connector_id", "connector_version"}},
		{top["scope"], "$.scope", []string{"org_ref", "project_ref"}},
		{top["subject"], "$.subject", []string{"type", "ref"}},
		{top["actor"], "$.actor", []string{"type"}},
		{top["time"], "$.time", []string{"occurred_at", "terminal_at"}},
		{top["correlation"], "$.correlation", []string{"request_ref"}},
		{top["identity"], "$.identity", []string{"idempotency_key", "source_revision"}},
		{top["integrity"], "$.integrity", []string{"source_hash", "envelope_hash"}},
		{top["data_policy"], "$.data_policy", []string{"payload_capture_mode", "raw_payload_present"}},
		{top["provenance"], "$.provenance", []string{"adapter_fidelity", "field_groups"}},
	}
	for _, check := range checks {
		if _, err := require(check.value, check.path, check.keys...); err != nil {
			return err
		}
	}
	payload, err := require(top["payload"], "$.payload",
		"terminal", "usage", "money", "timeline_complete", "capture_gaps",
	)
	if err != nil {
		return err
	}
	if _, err := require(payload["terminal"], "$.payload.terminal", "status"); err != nil {
		return err
	}
	if _, err := require(payload["usage"], "$.payload.usage",
		"model", "prompt_tokens", "uncached_prompt_tokens", "cache_read_tokens",
		"cache_creation_tokens", "completion_tokens", "source", "authority",
		"purpose", "finality", "contract_version", "cache_usage_schema",
		"cache_usage_status",
	); err != nil {
		return err
	}
	if _, err := require(payload["money"], "$.payload.money",
		"schema", "currency", "amount_scale", "calculated_cost_micros",
		"charged_cost_micros", "pricing_version",
	); err != nil {
		return err
	}
	gaps, ok := payload["capture_gaps"].([]any)
	if !ok {
		return errors.New("$.payload.capture_gaps must be an array")
	}
	for index, value := range gaps {
		if _, err := require(value, fmt.Sprintf("$.payload.capture_gaps[%d]", index),
			"gap_id", "parent_event_id", "scope", "correlation", "affected_claim",
			"gap_type", "reason_code", "severity", "observed_at",
			"payload_capture_mode", "field_provenance",
		); err != nil {
			return err
		}
	}
	return nil
}

func validateFoigentUsage(usage foigentUsageV1) error {
	values := []string{
		usage.PromptTokens,
		usage.UncachedPromptTokens,
		usage.CacheReadTokens,
		usage.CacheCreationTokens,
		usage.CompletionTokens,
	}
	parsed := make([]int64, len(values))
	for index, value := range values {
		if !foigentCounterPattern.MatchString(value) {
			return errors.New("Foigent Envelope token counter is not canonical")
		}
		number, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return errors.New("Foigent Envelope token counter exceeds int64")
		}
		parsed[index] = number
	}
	if parsed[1] > parsed[0] ||
		parsed[2] > parsed[0]-parsed[1] ||
		parsed[3] != parsed[0]-parsed[1]-parsed[2] {
		return errors.New("Foigent Envelope prompt token partition is invalid")
	}
	if _, err := strconv.ParseInt(usage.CompletionTokens, 10, 64); err != nil {
		return errors.New("Foigent Envelope completion token count is invalid")
	}
	return validateCompletedExecutionText("payload.usage.model", usage.Model, 1, 100)
}

func completedExecutionFromFoigentEnvelope(envelope foigentEnvelopeV1) (CompletedExecutionV1, error) {
	orgID, err := parsePositiveFoigentRef("scope.org_ref", envelope.Scope.OrgRef)
	if err != nil {
		return CompletedExecutionV1{}, err
	}
	projectID, err := parsePositiveFoigentRef("scope.project_ref", envelope.Scope.ProjectRef)
	if err != nil {
		return CompletedExecutionV1{}, err
	}
	terminalAt, err := parseFoigentMicrosecondTime(envelope.Time.TerminalAt)
	if err != nil {
		return CompletedExecutionV1{}, err
	}
	parseCounter := func(name, value string) (int64, error) {
		parsed, parseErr := strconv.ParseInt(value, 10, 64)
		if parseErr != nil {
			return 0, fmt.Errorf("%s: %w", name, parseErr)
		}
		return parsed, nil
	}
	prompt, err := parseCounter("prompt_tokens", envelope.Payload.Usage.PromptTokens)
	if err != nil {
		return CompletedExecutionV1{}, err
	}
	uncached, err := parseCounter("uncached_prompt_tokens", envelope.Payload.Usage.UncachedPromptTokens)
	if err != nil {
		return CompletedExecutionV1{}, err
	}
	cacheRead, err := parseCounter("cache_read_tokens", envelope.Payload.Usage.CacheReadTokens)
	if err != nil {
		return CompletedExecutionV1{}, err
	}
	cacheCreation, err := parseCounter("cache_creation_tokens", envelope.Payload.Usage.CacheCreationTokens)
	if err != nil {
		return CompletedExecutionV1{}, err
	}
	completion, err := parseCounter("completion_tokens", envelope.Payload.Usage.CompletionTokens)
	if err != nil {
		return CompletedExecutionV1{}, err
	}
	gapsByClaim := make(map[string]foigentCaptureGapV1, len(envelope.Payload.CaptureGaps))
	for _, gap := range envelope.Payload.CaptureGaps {
		gapsByClaim[gap.AffectedClaim] = gap
	}
	gaps := make([]CompletedExecutionCaptureGap, 0, len(envelope.Payload.CaptureGaps))
	for _, claim := range completedExecutionRequiredGaps {
		gap := gapsByClaim[claim]
		gaps = append(gaps, CompletedExecutionCaptureGap{
			Source: gap.AffectedClaim,
			Reason: gap.ReasonCode,
		})
	}
	snapshot := CompletedExecutionV1{
		SchemaVersion: CompletedExecutionSchemaV1,
		RequestID:     envelope.EventID,
		OrgID:         orgID,
		ProjectID:     projectID,
		Terminal: CompletedExecutionTerminalV1{
			Status:      CompletedExecutionTerminalStatus(envelope.Payload.Terminal.Status),
			AvailableAt: terminalAt,
		},
		Usage: CompletedExecutionUsageV1{
			Model:                envelope.Payload.Usage.Model,
			PromptTokens:         prompt,
			UncachedPromptTokens: uncached,
			CacheReadTokens:      cacheRead,
			CacheCreationTokens:  cacheCreation,
			CompletionTokens:     completion,
			Source:               envelope.Payload.Usage.Source,
			Authority:            envelope.Payload.Usage.Authority,
			Purpose:              envelope.Payload.Usage.Purpose,
			Finality:             envelope.Payload.Usage.Finality,
			ContractVersion:      envelope.Payload.Usage.ContractVersion,
			EstimateVersion:      envelope.Payload.Usage.EstimateVersion,
			CacheUsageSchema:     envelope.Payload.Usage.CacheUsageSchema,
			CacheUsageStatus:     envelope.Payload.Usage.CacheUsageStatus,
		},
		Money: CompletedExecutionMoneyV1{
			Schema:               envelope.Payload.Money.Schema,
			Currency:             envelope.Payload.Money.Currency,
			AmountScale:          envelope.Payload.Money.AmountScale,
			CalculatedCostMicros: envelope.Payload.Money.CalculatedCostMicros,
			ChargedCostMicros:    envelope.Payload.Money.ChargedCostMicros,
			PricingVersion:       envelope.Payload.Money.PricingVersion,
		},
		CaptureGaps: gaps,
	}
	if err := ValidateCompletedExecutionV1(snapshot); err != nil {
		return CompletedExecutionV1{}, err
	}
	return snapshot, nil
}

func foigentCaptureGapMap(
	requestID, orgRef, projectRef, observedAt, claim, reason string,
) map[string]any {
	return map[string]any{
		"gap_id":          requestID + ":gap:" + claim,
		"parent_event_id": requestID,
		"scope": map[string]any{
			"org_ref":     orgRef,
			"project_ref": projectRef,
		},
		"correlation":          map[string]any{"request_ref": requestID},
		"affected_claim":       claim,
		"gap_type":             completedExecutionUnavailableCaptureGapType,
		"reason_code":          reason,
		"severity":             "warning",
		"observed_at":          observedAt,
		"payload_capture_mode": "none",
		"field_provenance":     stringMapToAny(foigentGapFieldProvenance),
	}
}

func parsePositiveFoigentRef(name, value string) (int64, error) {
	if !regexp.MustCompile(`^[1-9][0-9]*$`).MatchString(value) {
		return 0, fmt.Errorf("%s must be a canonical positive decimal string", name)
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("%s exceeds positive int64", name)
	}
	return parsed, nil
}

func parseFoigentMicrosecondTime(value string) (time.Time, error) {
	const layout = "2006-01-02T15:04:05.000000Z"
	parsed, err := time.Parse(layout, value)
	if err != nil || parsed.Format(layout) != value {
		return time.Time{}, errors.New("Foigent Envelope time must be canonical UTC microseconds")
	}
	return parsed, nil
}

func foigentIdempotencyKey(parts ...string) (string, error) {
	var input bytes.Buffer
	input.WriteString(foigentIdempotencyDomain)
	for _, part := range parts {
		if !utf8.ValidString(part) {
			return "", errors.New("Foigent idempotency component must be valid UTF-8")
		}
		if uint64(len(part)) > uint64(^uint32(0)) {
			return "", errors.New("Foigent idempotency component exceeds uint32 bytes")
		}
		var length [4]byte
		binary.BigEndian.PutUint32(length[:], uint32(len(part)))
		input.Write(length[:])
		input.WriteString(part)
	}
	digest := sha256.Sum256(input.Bytes())
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

func stringMapToAny(input map[string]string) map[string]any {
	result := make(map[string]any, len(input))
	for key, value := range input {
		result[key] = value
	}
	return result
}

func equalStringMap(left, right map[string]string) bool {
	if len(left) != len(right) {
		return false
	}
	for key, value := range left {
		if right[key] != value {
			return false
		}
	}
	return true
}

func cloneFoigentMap(input map[string]any) map[string]any {
	cloned := make(map[string]any, len(input))
	for key, value := range input {
		if nested, ok := value.(map[string]any); ok {
			cloned[key] = cloneFoigentMap(nested)
			continue
		}
		cloned[key] = value
	}
	return cloned
}

func canonicalFoigentJSON(value any) ([]byte, error) {
	var output bytes.Buffer
	if err := appendCanonicalFoigentJSON(&output, value); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

func appendCanonicalFoigentJSON(output *bytes.Buffer, value any) error {
	switch typed := value.(type) {
	case nil:
		output.WriteString("null")
	case bool:
		if typed {
			output.WriteString("true")
		} else {
			output.WriteString("false")
		}
	case string:
		appendCanonicalFoigentString(output, typed)
	case int:
		output.WriteString(strconv.Itoa(typed))
	case int64:
		output.WriteString(strconv.FormatInt(typed, 10))
	case json.Number:
		if _, err := strconv.ParseInt(string(typed), 10, 64); err != nil {
			return fmt.Errorf("unsupported Foigent JSON number %q", typed)
		}
		output.WriteString(string(typed))
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Slice(keys, func(left, right int) bool {
			return compareFoigentUTF16(keys[left], keys[right]) < 0
		})
		output.WriteByte('{')
		for index, key := range keys {
			if index > 0 {
				output.WriteByte(',')
			}
			appendCanonicalFoigentString(output, key)
			output.WriteByte(':')
			if err := appendCanonicalFoigentJSON(output, typed[key]); err != nil {
				return err
			}
		}
		output.WriteByte('}')
	case []any:
		output.WriteByte('[')
		for index, item := range typed {
			if index > 0 {
				output.WriteByte(',')
			}
			if err := appendCanonicalFoigentJSON(output, item); err != nil {
				return err
			}
		}
		output.WriteByte(']')
	default:
		return fmt.Errorf("unsupported canonical Foigent JSON type %T", value)
	}
	return nil
}

func appendCanonicalFoigentString(output *bytes.Buffer, value string) {
	output.WriteByte('"')
	for _, character := range value {
		switch character {
		case '"', '\\':
			output.WriteByte('\\')
			output.WriteRune(character)
		case '\b':
			output.WriteString(`\b`)
		case '\t':
			output.WriteString(`\t`)
		case '\n':
			output.WriteString(`\n`)
		case '\f':
			output.WriteString(`\f`)
		case '\r':
			output.WriteString(`\r`)
		default:
			if character < 0x20 {
				fmt.Fprintf(output, `\u%04x`, character)
			} else {
				output.WriteRune(character)
			}
		}
	}
	output.WriteByte('"')
}

func compareFoigentUTF16(left, right string) int {
	leftUnits := utf16.Encode([]rune(left))
	rightUnits := utf16.Encode([]rune(right))
	limit := len(leftUnits)
	if len(rightUnits) < limit {
		limit = len(rightUnits)
	}
	for index := 0; index < limit; index++ {
		switch {
		case leftUnits[index] < rightUnits[index]:
			return -1
		case leftUnits[index] > rightUnits[index]:
			return 1
		}
	}
	switch {
	case len(leftUnits) < len(rightUnits):
		return -1
	case len(leftUnits) > len(rightUnits):
		return 1
	default:
		return 0
	}
}
