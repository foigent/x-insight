package db

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/foigent/x-insight/internal/investigation"
)

func (s *EvidenceStore) GetInvestigationProjection(ctx context.Context, executionID string, asOf time.Time) (investigation.InvestigationProjection, error) {
	if s == nil || s.db == nil {
		return investigation.InvestigationProjection{}, errors.New("evidence store is not initialized")
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return investigation.InvestigationProjection{}, err
	}
	defer tx.Rollback()

	projection, err := readInvestigationProjection(ctx, investigationReader{db: tx}, executionID, asOf)
	if err != nil {
		return investigation.InvestigationProjection{}, err
	}
	if err := tx.Commit(); err != nil {
		return investigation.InvestigationProjection{}, err
	}
	if err := s.checkScopedExecution(ctx, executionID); err != nil {
		return investigation.InvestigationProjection{}, err
	}
	return projection, nil
}

func readInvestigationProjection(
	ctx context.Context,
	store investigationReader,
	executionID string,
	asOf time.Time,
) (investigation.InvestigationProjection, error) {
	detail, err := store.GetExecution(ctx, executionID, asOf)
	if err != nil {
		return investigation.InvestigationProjection{}, err
	}
	timeline, timelineTotal, err := store.ListTimeline(ctx, investigation.TimelineQuery{
		ExecutionID: executionID, AsOf: asOf, Limit: 201,
	})
	if err != nil {
		return investigation.InvestigationProjection{}, err
	}
	events, eventTotal, err := store.ListExecutionEvents(ctx, executionID, asOf, 201)
	if err != nil {
		return investigation.InvestigationProjection{}, err
	}
	if timelineTotal != eventTotal {
		return investigation.InvestigationProjection{}, errors.New("investigation event views are inconsistent")
	}
	gaps, gapTotal, err := store.ListExecutionGaps(ctx, investigation.GapQuery{
		ExecutionID: executionID, AsOf: asOf, Limit: 101,
	})
	if err != nil {
		return investigation.InvestigationProjection{}, err
	}
	return investigation.BuildProjection(investigation.ProjectionInput{
		Detail: detail, Timeline: timeline, Events: events, TotalEventCount: timelineTotal,
		Gaps: gaps, TotalGapCount: gapTotal,
	}), nil
}
