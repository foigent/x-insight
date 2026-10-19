// Package connectorsync controls this XI installation's outbound collection.
// It grants no source authority and has no peer wake or cross-admin credential.
package connectorsync

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"time"
)

var ErrPaused = errors.New("connector sync is manual or not due")
var ErrChanged = errors.New("connector sync revision changed")
var ErrConflict = errors.New("connector sync request conflicts")
var ErrInvalid = errors.New("connector sync request invalid")
var ErrBinding = errors.New("connector sync ownership unavailable")
var installationPattern = regexp.MustCompile(`^xci_[A-Za-z0-9_-]{22}$`)
var requestPattern = regexp.MustCompile(`^[a-f0-9]{8}-[a-f0-9]{4}-[1-5][a-f0-9]{3}-[89ab][a-f0-9]{3}-[a-f0-9]{12}$`)

const LocalSchema = "xinsight.local-connector-sync.v1"
const DefaultCadence = 60

type Request struct {
	SchemaVersion    string `json:"schema_version"`
	Action           string `json:"action"`
	InstallationID   string `json:"installation_id"`
	Actor            string `json:"actor"`
	ExpectedRevision int64  `json:"expected_revision"`
	RequestID        string `json:"request_id"`
	Auto             bool   `json:"auto_sync"`
	Cadence          *int   `json:"cadence_seconds,omitempty"`
}

func (r Request) Valid() bool {
	if r.SchemaVersion != LocalSchema || !installationPattern.MatchString(r.InstallationID) {
		return false
	}
	if r.Action == "status" {
		return true
	}
	return (r.Action == "set" || r.Action == "once") && requestPattern.MatchString(r.RequestID) && r.ExpectedRevision > 0 && (r.Cadence == nil || *r.Cadence >= 5 && *r.Cadence <= 3600)
}

type Run struct {
	RequestID string            `json:"request_id"`
	Revision  int64             `json:"revision"`
	Started   time.Time         `json:"started_at"`
	Deadline  time.Time         `json:"deadline"`
	Pages     int               `json:"pages"`
	Attempts  int               `json:"attempts"`
	State     string            `json:"state"`
	Items     int64             `json:"items"`
	Results   map[string]string `json:"results"`
}
type Slot struct {
	Worker   string     `json:"worker"`
	Revision int64      `json:"revision"`
	Busy     bool       `json:"busy"`
	Started  time.Time  `json:"started_at"`
	Ended    *time.Time `json:"ended_at"`
	Error    string     `json:"last_error"`
}
type Status struct {
	RequestID      string     `json:"last_request_id"`
	InstallationID string     `json:"installation_id"`
	Auto           bool       `json:"auto_sync"`
	Cadence        *int       `json:"cadence_seconds"`
	Revision       int64      `json:"revision"`
	Effective      string     `json:"effective"`
	Peer           string     `json:"peer"`
	Checked        *time.Time `json:"last_checked_at"`
	Data           *time.Time `json:"last_data_at"`
	Error          string     `json:"last_error"`
	Required       []string   `json:"required_workers"`
	Run            *Run       `json:"run,omitempty"`
	Slots          []Slot     `json:"workers"`
}
type Response struct {
	SchemaVersion string  `json:"schema_version"`
	Status        *Status `json:"status,omitempty"`
	Error         string  `json:"error,omitempty"`
}
type Controller interface {
	ExecuteSync(context.Context, Request) (Response, error)
}

type permit struct {
	Store                                         *Store
	Installation, Worker, Owner, Token, Run, Unit string
	Revision                                      int64
	Deadline                                      time.Time
	More                                          bool
	Items                                         int
}
type contextKey struct{}

// Fence must run before business locks, in the SAME transaction as the write.
// Offline operations have no permit and retain their existing authorization.
func Fence(ctx context.Context, tx *sql.Tx) error {
	p, _ := ctx.Value(contextKey{}).(*permit)
	if p == nil {
		return nil
	}
	var rev int64
	if err := tx.QueryRowContext(ctx, `SELECT revision FROM connector_sync_controls WHERE installation_id=$1 FOR UPDATE`, p.Installation).Scan(&rev); err != nil {
		return err
	}
	var authorized bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM public.xinsight_sync_units($1))`, p.Installation).Scan(&authorized); err != nil {
		return err
	}
	if !authorized {
		return ErrBinding
	}
	if rev != p.Revision || !time.Now().Before(p.Deadline) {
		return ErrChanged
	}
	var valid bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM connector_sync_slots WHERE installation_id=$1 AND worker=$2 AND token=$3 AND owner=$4 AND busy AND revision=$5)`, p.Installation, p.Worker, p.Token, p.Owner, p.Revision).Scan(&valid); err != nil {
		return err
	}
	if !valid {
		return ErrChanged
	}
	return nil
}

// BeforeHTTP is called for each application attempt. Client redirect policies
// still enforce the original origin contract; sync cannot weaken them.
func BeforeHTTP(ctx context.Context, required bool) error {
	p, _ := ctx.Value(contextKey{}).(*permit)
	if p == nil {
		if required {
			return ErrBinding
		}
		return nil
	}
	return p.Store.attempt(ctx, p)
}

func IsControlStop(err error) bool {
	return errors.Is(err, ErrPaused) || errors.Is(err, ErrChanged) || errors.Is(err, ErrConflict) || errors.Is(err, ErrBinding)
}

// Progress is per logical unit, so an empty completed category is not polled
// again during the same explicit once. Existing consumers own their cursors.
func Progress(ctx context.Context, more bool, items int) {
	if p, _ := ctx.Value(contextKey{}).(*permit); p != nil {
		p.More = more
		p.Items = items
	}
}
func CommitFence(ctx context.Context, tx *sql.Tx) error {
	if e := Fence(ctx, tx); e != nil {
		return e
	}
	if p, _ := ctx.Value(contextKey{}).(*permit); p != nil {
		_, e := tx.ExecContext(ctx, `UPDATE connector_sync_controls SET last_data_at=clock_timestamp() WHERE installation_id=$1`, p.Installation)
		return e
	}
	return nil
}
