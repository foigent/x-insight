package connectorsync

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"
	"strings"
)

// Failure retains errors.Is/As for the caller, while its printable form and
// stored code contain only closed vocabulary. Never record an upstream body,
// URL, filesystem path, credential, or the original error text here.
type Failure struct {
	stage, category, unit string
	cause                 error
}

func (f *Failure) Error() string    { return f.stage + ":" + f.category }
func (f *Failure) Unwrap() error    { return f.cause }
func (f *Failure) Stage() string    { return f.stage }
func (f *Failure) Category() string { return f.category }

func word(value, allowed string) string {
	for _, candidate := range strings.Fields(allowed) {
		if value == candidate {
			return value
		}
	}
	return "unknown"
}

// WithFailure annotates the actual failing boundary without changing the
// failure, retry, control-stop, or transaction semantics. Inner stages win.
func WithFailure(stage, category string, err error, unit ...string) error {
	if err == nil || IsControlStop(err) {
		return err
	}
	var known *Failure
	if errors.As(err, &known) {
		return err
	}
	f := &Failure{stage: word(stage, "unknown inspect original_activation receiver_proof source_descriptor descriptor_commit receiver_binding receiver_state activation_request machine_prepare machine_request machine_enqueue machine_config machine_runtime policy_scopes enrollment_client enrollment_list project_read project_revoke project_qualify project_configure enrollment_save project_publish configuration_encode activation_epoch feed_stage feed_credential feed_register feed_promote feed_activate activation_claim activation_commit feed_activation feed_poll"), category: word(category, "unknown authorization unavailable contract conflict not_found unconfirmed canceled deadline local_permission local_store host_busy"), cause: err}
	if len(unit) > 0 {
		f.unit = unit[0]
	}
	return f
}
func diagnosticCode(err error) string {
	code := ""
	visitFailures(err, func(err error) {
		next := "unknown:unknown"
		var f *Failure
		if errors.As(err, &f) {
			next = f.Error()
		}
		if code == "" {
			code = next
		} else if code != next {
			code = "multiple:failures"
		}
	})
	return code
}
func reference(value string) string {
	if value == "" {
		return ""
	}
	return fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(value)))
}

// Each failed logical subunit is journaled before the best-effort status
// transaction: a failed status write must not erase the original safe cause.
func logFailures(ctx context.Context, p *permit, result error) {
	visitFailures(result, func(err error) {
		var f *Failure
		stage, category, unit := "unknown", "unknown", ""
		if errors.As(err, &f) {
			stage, category, unit = f.stage, f.category, f.unit
		}
		slog.ErrorContext(ctx, "connector_sync_failure", "installation_ref", reference(p.Installation), "run_ref", reference(p.Run), "revision", p.Revision, "worker", word(p.Worker, "activation projects source feed policy credentials"), "unit_ref", reference(p.Unit), "subunit_ref", reference(unit), "stage", stage, "category", category)
	})
}

func visitFailures(err error, visit func(error)) {
	if err == nil {
		return
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		for _, child := range joined.Unwrap() {
			visitFailures(child, visit)
		}
		return
	}
	if !IsControlStop(err) {
		visit(err)
	}
}

// Delegated authority can legitimately be pending. Record that observed fact
// without turning a deferred phase into an error or guessing its root cause.
func Deferred(ctx context.Context, err error) {
	p, _ := ctx.Value(contextKey{}).(*permit)
	if p == nil || err == nil {
		return
	}
	var f *Failure
	if !errors.As(err, &f) {
		return
	}
	slog.InfoContext(ctx, "connector_sync_deferred", "installation_ref", reference(p.Installation), "run_ref", reference(p.Run), "revision", p.Revision, "unit_ref", reference(p.Unit), "stage", f.stage, "category", f.category)
}
