package connectorcontrol

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log"
	"net"
	"strings"
	"syscall"
	"testing"
	"time"
)

type setupDiagnosticConn struct {
	reader      io.Reader
	writeErr    error
	deadlineErr error
}

func (c *setupDiagnosticConn) Read(p []byte) (int, error) { return c.reader.Read(p) }
func (c *setupDiagnosticConn) Write(p []byte) (int, error) {
	if c.writeErr != nil {
		return 0, c.writeErr
	}
	return len(p), nil
}
func (*setupDiagnosticConn) Close() error                     { return nil }
func (*setupDiagnosticConn) LocalAddr() net.Addr              { return &net.UnixAddr{} }
func (*setupDiagnosticConn) RemoteAddr() net.Addr             { return &net.UnixAddr{} }
func (c *setupDiagnosticConn) SetDeadline(time.Time) error    { return c.deadlineErr }
func (*setupDiagnosticConn) SetReadDeadline(time.Time) error  { return nil }
func (*setupDiagnosticConn) SetWriteDeadline(time.Time) error { return nil }

func TestSetupClientFailureLogsOnlyFixedCategories(t *testing.T) {
	const private = "private-path-ticket-secret"
	request := SetupLocalRequest{SchemaVersion: LocalSetupRequestSchema, Action: "status", ActorSubject: "xs_0123456789abcdef", InstallationID: "xci_AAAAAAAAAAAAAAAAAAAAAA"}
	valid := `{"schema_version":"` + LocalSetupResponseSchema + `","action":"status","status":{"schema_version":"` + SetupStatusSchema + `","installation_id":"` + request.InstallationID + `","state":"needs_action"}}` + "\n"
	for _, tc := range []struct {
		name, want string
		dialErr    error
		conn       *setupDiagnosticConn
	}{
		{"dial missing", "phase=dial category=socket_missing", errors.Join(io.EOF, syscall.ENOENT, errors.New(private)), nil},
		{"dial unknown", "phase=dial category=internal", errors.New(private), nil},
		{"deadline unknown", "phase=deadline category=internal", nil, &setupDiagnosticConn{reader: strings.NewReader(valid), deadlineErr: errors.New(private)}},
		{"write closed", "phase=write category=connection_closed", nil, &setupDiagnosticConn{reader: strings.NewReader(valid), writeErr: errors.Join(syscall.EPIPE, errors.New(private))}},
		{"read closed", "phase=read category=connection_closed", nil, &setupDiagnosticConn{reader: strings.NewReader("")}},
		{"decode invalid", "phase=decode category=invalid_response", nil, &setupDiagnosticConn{reader: strings.NewReader("{" + private + "}\n")}},
		{"response unknown", "phase=response category=controller_unavailable", nil, &setupDiagnosticConn{reader: strings.NewReader(`{"schema_version":"` + LocalSetupResponseSchema + `","action":"status","error":"` + private + `"}` + "\n")}},
		{"success", "", nil, &setupDiagnosticConn{reader: strings.NewReader(valid)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var logs bytes.Buffer
			old := log.Writer()
			log.SetOutput(&logs)
			defer log.SetOutput(old)
			client := &UnixController{socketPath: LocalControllerSocket, timeout: time.Second, dial: func(context.Context) (net.Conn, error) {
				if tc.dialErr != nil {
					return nil, tc.dialErr
				}
				return tc.conn, nil
			}}
			_, err := client.ExecuteSetup(t.Context(), request)
			if (tc.want == "") != (err == nil) {
				t.Fatalf("unexpected result: %v", err)
			}
			if tc.want != "" && (!strings.Contains(logs.String(), "connector setup client action=status "+tc.want) || strings.Count(logs.String(), "connector setup client") != 1) {
				t.Fatalf("missing fixed category: %q", logs.String())
			}
			if tc.want == "" && logs.Len() != 0 || strings.Contains(logs.String(), private) || strings.Contains(logs.String(), request.ActorSubject) || strings.Contains(logs.String(), request.InstallationID) {
				t.Fatalf("setup diagnostic exposed request or private error: %q", logs.String())
			}
		})
	}
}
