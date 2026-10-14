package db

import (
	"context"
	"github.com/foigent/x-insight/internal/connectorcontrol"
)

// Bind once at process construction to the target's own root-managed identity.
// Restored installation history is readable but cannot become local authority.
func (s *WorkspaceStore) SetConnectorDeploymentID(id string) error {
	if s == nil || !connectorcontrol.IsDeploymentID(id) || s.connectorDeploymentID != "" && s.connectorDeploymentID != id {
		return connectorcontrol.ErrConflict
	}
	s.connectorDeploymentID = id
	return nil
}
func (s *WorkspaceStore) requireConnectorDeployment(ctx context.Context, id string) error {
	if s == nil || s.db == nil {
		return connectorcontrol.ErrInvalid
	}
	if s.connectorDeploymentID == "" {
		return nil
	} // Existing isolated stores, not production construction.
	var deployment string
	if err := s.db.QueryRowContext(ctx, `SELECT deployment_id FROM connector_installations WHERE id=$1`, id).Scan(&deployment); err != nil {
		return err
	}
	if deployment != s.connectorDeploymentID {
		return connectorcontrol.ErrConflict
	}
	return nil
}
