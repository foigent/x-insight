package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/foigent/x-insight/internal/discovery"
	"github.com/foigent/x-insight/internal/investigation"
)

func (s *WorkspaceStore) SaveDiscoveryRun(ctx context.Context, run discovery.AnalysisRun) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("workspace store is not initialized")
	}
	if err := validateDiscoveryRun(run); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `
INSERT INTO discovery_analysis_runs (
  id, method_version, analysis_as_of, sample_size, minimum_sample,
  input_digest, generated_at
) VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (id) DO NOTHING`,
		run.ID, run.MethodVersion, run.AnalysisAsOf.UTC(), run.SampleSize,
		run.MinimumSample, run.InputDigest, run.GeneratedAt.UTC(),
	); err != nil {
		return err
	}
	var persisted discovery.AnalysisRun
	if err := tx.QueryRowContext(ctx, `
SELECT id, method_version, analysis_as_of, sample_size, minimum_sample,
       input_digest, generated_at
FROM discovery_analysis_runs
WHERE id = $1`, run.ID).Scan(
		&persisted.ID, &persisted.MethodVersion, &persisted.AnalysisAsOf,
		&persisted.SampleSize, &persisted.MinimumSample, &persisted.InputDigest,
		&persisted.GeneratedAt,
	); err != nil {
		return err
	}
	persisted.AnalysisAsOf = persisted.AnalysisAsOf.UTC()
	persisted.GeneratedAt = persisted.GeneratedAt.UTC()
	if persisted.ID != run.ID ||
		persisted.MethodVersion != run.MethodVersion ||
		!persisted.AnalysisAsOf.Equal(run.AnalysisAsOf) ||
		persisted.SampleSize != run.SampleSize ||
		persisted.MinimumSample != run.MinimumSample ||
		persisted.InputDigest != run.InputDigest ||
		!persisted.GeneratedAt.Equal(run.GeneratedAt) {
		return errors.New("discovery run identity conflict")
	}
	for _, item := range run.Hypotheses {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO discovery_hypotheses (
  id, analysis_run_id, hypothesis_type, title, statement,
	  support_score_decimal, status, evidence_event_ids, execution_ids,
  limitations, abstention_reason, method_version, generated_at
) VALUES (
  $1, $2, $3, $4, $5, NULLIF($6, ''), $7, $8::uuid[], $9,
  $10, NULLIF($11, ''), $12, $13
)
ON CONFLICT (id) DO NOTHING`,
			item.ID, item.AnalysisRunID, item.HypothesisType, item.Title,
			item.Statement, item.SupportScoreDecimal, item.Status,
			item.EvidenceEventIDs, item.ExecutionIDs, item.Limitations,
			item.AbstentionReason, item.MethodVersion, item.GeneratedAt.UTC(),
		); err != nil {
			return err
		}
		persistedItem, err := loadDiscoveryHypothesis(ctx, tx, item.ID)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(persistedItem, item) {
			return errors.New("discovery hypothesis identity conflict")
		}
	}
	return tx.Commit()
}

func validateDiscoveryRun(run discovery.AnalysisRun) error {
	if run.ID == "" ||
		run.MethodVersion != discovery.MethodVersion ||
		run.AnalysisAsOf.IsZero() ||
		run.AnalysisAsOf.Location() != time.UTC ||
		run.GeneratedAt.IsZero() ||
		run.GeneratedAt.Location() != time.UTC ||
		run.GeneratedAt.Before(run.AnalysisAsOf) ||
		run.SampleSize < 0 ||
		run.SampleSize > discovery.MaxAnalysisExecutions ||
		run.MinimumSample < 20 ||
		run.MinimumSample > discovery.MaxAnalysisExecutions ||
		len(run.Hypotheses) != 6 {
		return errors.New("discovery run is invalid")
	}
	types := map[string]struct{}{}
	for _, item := range run.Hypotheses {
		if item.AnalysisRunID != run.ID ||
			!item.AnalysisAsOf.Equal(run.AnalysisAsOf) ||
			item.SampleSize != run.SampleSize ||
			item.InputDigest != run.InputDigest ||
			item.MethodVersion != run.MethodVersion ||
			!item.GeneratedAt.Equal(run.GeneratedAt) {
			return errors.New("discovery hypothesis run binding is invalid")
		}
		types[item.HypothesisType] = struct{}{}
	}
	if len(types) != 6 {
		return errors.New("discovery hypothesis classes are incomplete")
	}
	return nil
}

type discoveryQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func loadDiscoveryHypothesis(
	ctx context.Context,
	queryer discoveryQueryer,
	id string,
) (investigation.DiscoveryHypothesis, error) {
	var item investigation.DiscoveryHypothesis
	var supportScore, abstention sql.NullString
	var evidenceIDs, executionIDs, limitations string
	err := queryer.QueryRowContext(ctx, `
SELECT h.id, h.analysis_run_id, run.analysis_as_of, run.sample_size,
       run.input_digest, h.hypothesis_type, h.title, h.statement,
	       h.support_score_decimal, h.status,
       array_to_json(h.evidence_event_ids)::text,
       array_to_json(h.execution_ids)::text,
       array_to_json(h.limitations)::text,
       h.abstention_reason, h.method_version, h.generated_at
FROM discovery_hypotheses h
JOIN discovery_analysis_runs run ON run.id = h.analysis_run_id
WHERE h.id = $1`, id).Scan(
		&item.ID, &item.AnalysisRunID, &item.AnalysisAsOf, &item.SampleSize,
		&item.InputDigest, &item.HypothesisType, &item.Title, &item.Statement,
		&supportScore, &item.Status, &evidenceIDs, &executionIDs,
		&limitations, &abstention, &item.MethodVersion, &item.GeneratedAt,
	)
	if err != nil {
		return investigation.DiscoveryHypothesis{}, err
	}
	item.SupportScoreDecimal = supportScore.String
	item.EvidenceEventIDs = decodeNonNilJSONStringSlice(evidenceIDs)
	item.ExecutionIDs = decodeNonNilJSONStringSlice(executionIDs)
	item.Limitations = decodeNonNilJSONStringSlice(limitations)
	item.AbstentionReason = abstention.String
	item.AnalysisAsOf = item.AnalysisAsOf.UTC()
	item.GeneratedAt = item.GeneratedAt.UTC()
	return item, nil
}

func decodeNonNilJSONStringSlice(raw string) []string {
	values := decodeJSONStringSlice(raw)
	if values == nil {
		return []string{}
	}
	return values
}
