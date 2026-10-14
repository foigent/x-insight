package db

import (
	"context"
	"database/sql"
	"fmt"
	"github.com/foigent/x-insight/internal/workspace"
	"time"
)

// Only the newly restored database is supplied. Historical installations and
// facts remain; foreign machine credentials cannot authorize current traffic.
func SuspendForeignRestoredAuthority(ctx context.Context, database *sql.DB, sourceDeployment, targetDeployment string, now time.Time) error {
	if sourceDeployment == targetDeployment {
		return nil
	}
	if sourceDeployment == "" || targetDeployment == "" || now.IsZero() {
		return fmt.Errorf("restored deployment binding incomplete")
	}
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var owner bool
	if err = tx.QueryRowContext(ctx, `SELECT pg_has_role(current_user,relowner,'USAGE') FROM pg_class WHERE oid='public.service_identities'::regclass`).Scan(&owner); err != nil {
		return err
	}
	if !owner {
		return workspace.ErrInvalid
	}
	rows, err := tx.QueryContext(ctx, `SELECT id FROM service_identities WHERE status='active' ORDER BY id FOR UPDATE`)
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, id := range ids {
		if _, err = revokeServiceIdentityTx(ctx, tx, id, workspace.Actor{Kind: "root", Ref: "root"}, "foreign_deployment_restore_requires_authorization", now.UTC()); err != nil {
			return err
		}
	}
	// Even tokens belonging to previously expired identities lose old authority.
	if _, err = tx.ExecContext(ctx, `UPDATE service_identity_tokens SET revoked_at=$1 WHERE revoked_at IS NULL`, now.UTC()); err != nil {
		return err
	}
	return tx.Commit()
}
