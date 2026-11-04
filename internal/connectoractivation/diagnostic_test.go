package connectoractivation

import (
	"context"
	"errors"
	"github.com/foigent/x-insight/internal/connectorsync"
	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/hostctl"
	"github.com/foigent/x-insight/internal/integration/urmachine"
	"github.com/jackc/pgx/v5/pgconn"
	"testing"
	"time"
)

func TestSyncFailureClassifiesFactsWithoutGuessingCredentials(t *testing.T) {
	for _, tc := range []struct {
		err      error
		category string
	}{{urmachine.ErrUnauthorized, "authorization"}, {urmachine.ErrUnavailable, "unavailable"}, {urmachine.ErrUnconfirmed, "unconfirmed"}, {urmachine.ErrContract, "contract"}, {hostctl.ErrOperationBusy, "host_busy"}, {context.DeadlineExceeded, "deadline"}, {errors.New("private unknown reason"), "unknown"}} {
		err := syncFailure("enrollment_list", tc.err, "synthetic-unit")
		var f *connectorsync.Failure
		if !errors.Is(err, tc.err) || !errors.As(err, &f) || f.Category() != tc.category || f.Stage() != "enrollment_list" {
			t.Fatal("lost category/cause", err)
		}
	}
	if err := syncFailure("inspect", connectorsync.ErrChanged); err != connectorsync.ErrChanged {
		t.Fatal("control stop changed")
	}
}

type failedEnrollmentSave struct {
	err error
	got db.ManagedEnrollment
}

func (s *failedEnrollmentSave) SaveManagedEnrollment(_ context.Context, r db.ManagedEnrollment, _ time.Time) error {
	s.got = r
	return s.err
}
func TestEnrollmentSaveInjectionKeepsOwnBoundaryAndEarlierFailure(t *testing.T) {
	original := &pgconn.PgError{Code: "23514", Message: "private SQL value must not print"}
	store := &failedEnrollmentSave{err: original}
	record := db.ManagedEnrollment{ProjectID: "9", EnrollmentID: "enrollment-1"}
	save := saveManagedEnrollment(t.Context(), store, record, time.Now())
	var failure *connectorsync.Failure
	if !errors.Is(save, original) || !errors.As(save, &failure) || failure.Stage() != "enrollment_save" || failure.Category() != "local_store" || store.got.ProjectID != "9" {
		t.Fatal("save failure misattributed", save)
	}
	qualify := syncFailure("project_qualify", urmachine.ErrConflict, "9/enrollment-1")
	joined := errors.Join(qualify, save)
	if !errors.Is(joined, urmachine.ErrConflict) || !errors.Is(joined, original) {
		t.Fatal("saving a migration marker lost qualification failure", joined)
	}
	store.err = nil
	if err := saveManagedEnrollment(t.Context(), store, record, time.Now()); err != nil {
		t.Fatal("successful save changed behavior", err)
	}
}
