//go:build integration

package db_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/connectorcontrol"
)

type jointManagedAckDiagnostic struct {
	t      *testing.T
	actual connectorcontrol.ManagedAcknowledger
}

type jointManagedClientDiagnostic struct {
	*connectorcontrol.UnixController
	t *testing.T
}

func (d jointManagedClientDiagnostic) AcknowledgeManaged(ctx context.Context, identity string, v connectorcontrol.ControlResultV3) (connectorcontrol.Installation, error) {
	i, e := d.UnixController.AcknowledgeManaged(ctx, identity, v)
	if e != nil {
		d.t.Logf("joint real Unix managed ACK client phase=%s status=%s error=%v", v.Phase, v.Status, e)
	}
	return i, e
}

func (d jointManagedAckDiagnostic) AcknowledgeManaged(ctx context.Context, identity string, v connectorcontrol.ControlResultV3) (connectorcontrol.Installation, error) {
	i, e := d.actual.AcknowledgeManaged(ctx, identity, v)
	if e != nil {
		d.t.Logf("joint real local managed ACK phase=%s status=%s error=%v", v.Phase, v.Status, e)
	}
	return i, e
}

type jointHTTPStatusDiagnostic struct {
	http.ResponseWriter
	status int
}

func (w *jointHTTPStatusDiagnostic) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}
func (w *jointHTTPStatusDiagnostic) Write(body []byte) (int, error) {
	if w.status == 0 {
		w.status = 200
	}
	return w.ResponseWriter.Write(body)
}
func jointACKDiagnosticHandler(t *testing.T, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !strings.HasPrefix(r.URL.Path, "/api/v1/connector-control/operations/") {
			next.ServeHTTP(w, r)
			return
		}
		// Restore exactly the consumed prefix and remaining reader. Never print a
		// request, bearer, identity, body hash or original content.
		prefix, e := io.ReadAll(io.LimitReader(r.Body, 32<<10))
		if e != nil {
			next.ServeHTTP(w, r)
			return
		}
		r.Body = io.NopCloser(io.MultiReader(bytes.NewReader(prefix), r.Body))
		out := &jointHTTPStatusDiagnostic{ResponseWriter: w}
		next.ServeHTTP(out, r)
		if out.status == 400 {
			var v connectorcontrol.ControlOperationResult
			decode := json.Unmarshal(prefix, &v)
			valid := decode == nil && connectorcontrol.ValidateControlResult(v, time.Now().UTC()) == nil
			t.Logf("joint actual public ACK400 schema=%s phase=%s status=%s decoded=%t closed_result_valid=%t", v.SchemaVersion, v.Phase, v.Status, decode == nil, valid)
		}
	})
}
