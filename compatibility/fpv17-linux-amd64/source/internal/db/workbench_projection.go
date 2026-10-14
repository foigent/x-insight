package db

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strconv"
	"time"

	"github.com/foigent/x-insight/internal/executionid"
)

var workbenchMicrosPattern = regexp.MustCompile(`^(0|[1-9][0-9]{0,29})$`)

type workbenchEconomicFact struct {
	SourceSystemID            string
	SourceVersion             string
	SourceRef                 string
	SourceEventHash           string
	EvidenceEventID           string
	RequestID                 string
	ExecutionID               string
	ModelRef                  string
	SourceProjectRef          string
	CostProjectRef            string
	TerminalStatus            string
	OccurredAt                time.Time
	GatewayChargedMicros      string
	CalculatedReferenceMicros string
	Currency                  string
	AmountScale               int64
}

// projectWorkbenchEconomicFact runs inside the Evidence transaction. This
// keeps the V1 Evidence write and its immutable, identity-preserving economic fact atomic.
// The business identity deliberately excludes adapter and delivery epoch.
func projectWorkbenchEconomicFact(ctx context.Context, tx *sql.Tx, event map[string]any, eventID string) error {
	fact, applicable, err := workbenchEconomicFactFromEvent(event, eventID)
	if err != nil || !applicable {
		return err
	}
	rows, err := tx.QueryContext(ctx, `
INSERT INTO workbench_economic_facts (
  source_system_id, source_version, source_ref, source_event_hash,
  evidence_event_id, request_id, execution_id, model_ref, source_project_ref,
  cost_project_ref, terminal_status, occurred_at, gateway_charged_micros,
  calculated_reference_micros, currency, amount_scale, event_hash
) VALUES (
  $1::uuid, $2, $3, $4,
  $5::uuid, $6, $7, $8,
  $9, $10, $11, $12,
  $13::numeric, $14::numeric, $15, $16, (SELECT event_hash FROM evidence_events WHERE id=$5::uuid)
)
ON CONFLICT (source_system_id, source_version, source_ref) DO NOTHING
RETURNING id`,
		fact.SourceSystemID, fact.SourceVersion, fact.SourceRef, fact.SourceEventHash,
		fact.EvidenceEventID, fact.RequestID, fact.ExecutionID, nullString(fact.ModelRef),
		nullString(fact.SourceProjectRef), nullString(fact.CostProjectRef), fact.TerminalStatus, fact.OccurredAt,
		fact.GatewayChargedMicros, fact.CalculatedReferenceMicros, fact.Currency, fact.AmountScale,
	)
	if err != nil {
		return fmt.Errorf("project workbench economic fact: %w", err)
	}
	inserted := rows.Next()
	if closeErr := rows.Close(); closeErr != nil {
		return fmt.Errorf("close workbench economic projection result: %w", closeErr)
	}
	if inserted {
		return nil
	}

	var existing workbenchEconomicFact
	var sourceProject, costProject sql.NullString
	err = tx.QueryRowContext(ctx, `
SELECT source_system_id::text, source_version, source_ref, source_event_hash,
       evidence_event_id::text, request_id, execution_id, COALESCE(model_ref,''), source_project_ref,
       cost_project_ref, terminal_status, occurred_at,
       gateway_charged_micros::text, calculated_reference_micros::text,
       currency, amount_scale::bigint
FROM workbench_economic_facts
WHERE source_system_id = $1::uuid AND source_version = $2 AND source_ref = $3`,
		fact.SourceSystemID, fact.SourceVersion, fact.SourceRef,
	).Scan(
		&existing.SourceSystemID, &existing.SourceVersion, &existing.SourceRef,
		&existing.SourceEventHash, &existing.EvidenceEventID, &existing.RequestID,
		&existing.ExecutionID, &existing.ModelRef, &sourceProject, &costProject, &existing.TerminalStatus,
		&existing.OccurredAt, &existing.GatewayChargedMicros,
		&existing.CalculatedReferenceMicros, &existing.Currency, &existing.AmountScale,
	)
	if err != nil {
		return fmt.Errorf("read existing workbench economic fact: %w", err)
	}
	existing.SourceProjectRef = nullStringValue(sourceProject)
	existing.CostProjectRef = nullStringValue(costProject)
	// EvidenceEventID is intentionally not compared: a later qualified adapter
	// may observe the same source fact, but it may not manufacture another fee.
	if existing.SourceSystemID != fact.SourceSystemID || existing.SourceVersion != fact.SourceVersion ||
		existing.SourceRef != fact.SourceRef || existing.SourceEventHash != fact.SourceEventHash ||
		existing.RequestID != fact.RequestID || existing.ExecutionID != fact.ExecutionID ||
		existing.ModelRef != fact.ModelRef ||
		existing.SourceProjectRef != fact.SourceProjectRef || existing.CostProjectRef != fact.CostProjectRef ||
		existing.TerminalStatus != fact.TerminalStatus || !existing.OccurredAt.Equal(fact.OccurredAt) ||
		existing.GatewayChargedMicros != fact.GatewayChargedMicros ||
		existing.CalculatedReferenceMicros != fact.CalculatedReferenceMicros ||
		existing.Currency != fact.Currency || existing.AmountScale != fact.AmountScale {
		return fmt.Errorf("workbench economic fact identity conflicts with immutable source content")
	}
	return nil
}

func workbenchEconomicFactFromEvent(event map[string]any, eventID string) (workbenchEconomicFact, bool, error) {
	if stringField(event, "source_version") != uniRouterCompletedSourceVersionV1 {
		return workbenchEconomicFact{}, false, nil
	}
	eventType := requiredString(event, "event_type")
	if eventType != "billing.execution.settled" && eventType != "billing.execution.cancelled" {
		return workbenchEconomicFact{}, false, nil
	}
	usage, ok := event["usage"].(map[string]any)
	if !ok {
		return workbenchEconomicFact{}, false, fmt.Errorf("completed-execution workbench projection requires usage")
	}
	extensions, _ := usage["extensions"].(map[string]any)
	charged, _ := usage["cost_micros"].(string)
	calculated, _ := extensions["calculated_cost_micros"].(string)
	currency, _ := usage["currency"].(string)
	scale, err := integerField(usage["amount_scale"])
	if err != nil || !workbenchMicrosPattern.MatchString(charged) ||
		!workbenchMicrosPattern.MatchString(calculated) || currency != "CNY" || scale != 1000000 {
		return workbenchEconomicFact{}, false, fmt.Errorf("completed-execution workbench money is invalid")
	}
	requestID := stringField(event, "request_id")
	executionID, err := executionid.New(requiredString(event, "source_system_id"), requestID)
	if err != nil {
		return workbenchEconomicFact{}, false, fmt.Errorf("derive workbench execution identity: %w", err)
	}
	occurredAt, err := time.Parse(time.RFC3339Nano, requiredString(event, "occurred_at"))
	if err != nil {
		return workbenchEconomicFact{}, false, fmt.Errorf("completed-execution workbench time is invalid")
	}
	outcome, _ := event["outcome"].(map[string]any)
	capability, _ := event["capability"].(map[string]any)
	outcomeExtensions, _ := outcome["extensions"].(map[string]any)
	terminalStatus, _ := outcomeExtensions["terminal_status"].(string)
	if terminalStatus != "settled" && terminalStatus != "cancelled" {
		return workbenchEconomicFact{}, false, fmt.Errorf("completed-execution workbench terminal status is invalid")
	}
	return workbenchEconomicFact{
		SourceSystemID:            requiredString(event, "source_system_id"),
		SourceVersion:             requiredString(event, "source_version"),
		SourceRef:                 requiredString(event, "source_ref"),
		SourceEventHash:           requiredString(event, "source_event_hash"),
		EvidenceEventID:           eventID,
		RequestID:                 requestID,
		ExecutionID:               executionID,
		ModelRef:                  stringField(capability, "model"),
		SourceProjectRef:          stringField(event, "project_ref"),
		TerminalStatus:            terminalStatus,
		OccurredAt:                occurredAt.UTC(),
		GatewayChargedMicros:      charged,
		CalculatedReferenceMicros: calculated,
		Currency:                  currency,
		AmountScale:               scale,
	}, true, nil
}

func integerField(value any) (int64, error) {
	switch typed := value.(type) {
	case int:
		return int64(typed), nil
	case int64:
		return typed, nil
	case float64:
		if typed != float64(int64(typed)) {
			return 0, fmt.Errorf("not an integer")
		}
		return int64(typed), nil
	case string:
		return strconv.ParseInt(typed, 10, 64)
	default:
		return 0, fmt.Errorf("not an integer")
	}
}
