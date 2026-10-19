package db

import (
	"context"
	"database/sql"
	"github.com/foigent/x-insight/internal/connectorsync"
	"time"
)

func (s *WorkspaceStore) SyncStore() *connectorsync.Store {
	gate := connectorsync.New(s.db)
	gate.Audit = func(ctx context.Context, tx *sql.Tx, r connectorsync.Request) error {
		return insertAccessAudit(ctx, tx, "human", r.Actor, "connector.sync."+r.Action, "connector_installation", r.InstallationID, "accepted", time.Now())
	}
	return gate
}

func (s *SourceFactsStore) SyncStore() *connectorsync.Store {
	gate := connectorsync.New(s.db)
	gate.Audit = func(ctx context.Context, tx *sql.Tx, r connectorsync.Request) error {
		return insertAccessAudit(ctx, tx, "human", r.Actor, "connector.sync."+r.Action, "connector_installation", r.InstallationID, "accepted", time.Now())
	}
	return gate
}
func (s *SourceFactsStore) BeginSync(ctx context.Context, source, org, project, unit string) (context.Context, func(error), error) {
	gate := s.SyncStore()
	id, e := gate.Resolve(ctx, source, org, project, false)
	if e != nil {
		return ctx, nil, e
	}
	return gate.Begin(ctx, id, "source", 0, unit)
}
func (s *SourceFactsStore) SyncSchedule(ctx context.Context, kind, id string, base time.Duration) (time.Duration, bool, bool, error) {
	var source, org, project string
	var query string
	switch kind {
	case "facts":
		query = `SELECT source_instance,org_id,project_id FROM source_fact_streams WHERE id=$1::uuid`
	case "budget":
		query = `SELECT source_instance,org_id,project_id FROM source_budget_collections WHERE id=$1::uuid`
	case "content":
		query = `SELECT source_instance,org_id,project_id FROM source_content_collections WHERE id=$1::uuid`
	default:
		return base, false, false, connectorsync.ErrBinding
	}
	if e := s.db.QueryRowContext(ctx, query, id).Scan(&source, &org, &project); e != nil {
		return base, false, false, e
	}
	gate := s.SyncStore()
	installation, e := gate.Resolve(ctx, source, org, project, false)
	if e != nil {
		return base, false, false, e
	}
	return gate.Schedule(ctx, installation, base)
}

func (s *WorkspaceStore) ExecuteConnectorSync(ctx context.Context, r connectorsync.Request) (connectorsync.Status, error) {
	if e := s.requireConnectorDeployment(ctx, r.InstallationID); e != nil {
		return connectorsync.Status{}, e
	}
	gate := s.SyncStore()
	if e := gate.Reconcile(ctx, r.InstallationID); e != nil {
		return connectorsync.Status{}, e
	}
	if r.Action == "status" {
		return gate.Status(ctx, r.InstallationID)
	}
	return gate.Change(ctx, r)
}
