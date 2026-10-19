package main

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/foigent/x-insight/internal/connectorcontrol"
	"github.com/foigent/x-insight/internal/connectorsync"
)

// This lane checks continuous workers, future projects and restart continuity.
// The current product therefore needs an explicit owner Auto choice. A frozen
// predecessor keeps its original polling intent through migration instead.
func (d *driver) collectionIntent(ctx context.Context, preserved bool) error {
	if !connectorcontrol.IsInstallationID(d.installationID) || d.adminCookie == nil || d.adminCSRF == "" {
		return errors.New("collection_sync_owner_unavailable")
	}
	target := d.input.XIControlOrigin + "/api/v1/connector-control/installations/" + d.installationID + "/sync"
	var before connectorsync.Status
	if err := d.call(ctx, http.MethodGet, target, nil, d.adminCookie, "", &before); err != nil {
		return errors.New("collection_sync_status_failed")
	}
	if before.InstallationID != d.installationID || before.Revision != 1 || before.RequestID != "" {
		return errors.New("collection_sync_prestate_invalid")
	}
	if preserved {
		if !before.Auto || before.Cadence != nil {
			return errors.New("collection_sync_migration_changed_intent")
		}
		return nil
	}
	if before.Auto || before.Cadence != nil {
		return errors.New("collection_sync_manual_default_missing")
	}
	if r := before.Run; r != nil && (r.Deadline.Sub(r.Started) <= 0 || r.Deadline.Sub(r.Started) > time.Minute || r.Pages < 0 || r.Pages > 32 || r.Attempts < 0 || r.Attempts > 64) {
		return errors.New("collection_sync_initial_run_unbounded")
	}
	// Five seconds is the product's minimum valid cadence, explicitly selected
	// by this synthetic owner. No fixture SQL changes the preference or gate.
	requestID := uuid()
	body := map[string]any{"expected_revision": before.Revision, "request_id": requestID, "auto_sync": true, "cadence_seconds": 5}
	var after connectorsync.Status
	if err := d.call(ctx, http.MethodPut, target, body, d.adminCookie, d.adminCSRF, &after); err != nil {
		return errors.New("collection_sync_choice_failed")
	}
	if after.InstallationID != d.installationID || !after.Auto || after.Cadence == nil || *after.Cadence != 5 || after.Revision != before.Revision+1 || after.RequestID != requestID {
		return errors.New("collection_sync_choice_not_retained")
	}
	return nil
}
