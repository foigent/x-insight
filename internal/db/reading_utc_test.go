package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/workbench"
)

type readingUTCDriver struct {
	at      time.Time
	revoked bool
}
type readingUTCConn struct{ state readingUTCDriver }

func (d readingUTCDriver) Open(string) (driver.Conn, error) { return &readingUTCConn{d}, nil }
func (c *readingUTCConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (c *readingUTCConn) Close() error              { return nil }
func (c *readingUTCConn) Begin() (driver.Tx, error) { return nil, errors.New("read must not write") }
func readingUTCGrant() workbench.Grant {
	return workbench.Grant{ID: "grant", SubjectID: "subject", SourceSystemID: "source", AllowSummary: true, AllowDetails: true, AllowSourceRuntime: true, Revision: 1, LegacyRevision: 1}
}
func (c *readingUTCConn) QueryContext(_ context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	row := func(v ...driver.Value) (driver.Rows, error) { return &receiptTestRows{values: v}, nil }
	grant := readingUTCGrant()
	auth := workbenchAuthorizationFromGrants([]workbench.Grant{grant})
	at := c.state.at
	switch {
	case strings.Contains(q, "FROM workbench_readings WHERE"):
		if args[1].Value != "subject" {
			return &receiptTestRows{values: make([]driver.Value, 15), done: true}, nil
		}
		return row(args[0].Value, workbench.ContractVersion, workbench.MetricVersion, at.Add(-time.Hour), at, "Asia/Shanghai", at, at.Add(workbench.ReadingTTL), string(workbench.MoneyGatewayCharged), "CNY", "membership", digestText(workbenchInterpretationA0), auth.Revision, []byte(`{}`), []byte(`{}`))
	case strings.Contains(q, "FROM runtime_readings WHERE"):
		if args[1].Value != "subject" {
			return &receiptTestRows{values: make([]driver.Value, 16), done: true}, nil
		}
		return row(args[0].Value, workbench.RuntimeContractVersion, workbench.RuntimeMetricVersion, at.Add(-time.Hour), at, "Asia/Shanghai", at, at.Add(workbench.ReadingTTL), auth.ObservationRevision, digestText(runtimeInterpretation), "membership", int64(1), int64(0), int64(1), []byte(`{}`), "")
	case strings.Contains(q, "FROM workbench_access_grants WHERE"):
		if c.state.revoked {
			return &receiptTestRows{values: make([]driver.Value, 28), done: true}, nil
		}
		return row(grant.ID, grant.SubjectID, grant.SourceSystemID, "", "", true, true, false, false, int64(1), false, false, false, false, false, true, false, false, false, false, false, false, false, false, false, []byte(`[]`), int64(1), int64(0))
	case q == runtimeManifestDigestSQL:
		return row("membership")
	case q == `SELECT to_regclass('public.workbench_purpose_manifests') IS NOT NULL`:
		return row(false)
	default:
		return nil, fmt.Errorf("unexpected reading query: %.100s", q)
	}
}
func TestReadingReadbackUTCWithOffsetDriver(t *testing.T) {
	for _, offset := range []int{8 * 3600, -7 * 3600} {
		for _, kind := range []string{"cost", "runtime"} {
			for _, scenario := range []string{"valid", "wrong_owner", "expired", "revoked"} {
				t.Run(fmt.Sprintf("%s/%d/%s", kind, offset, scenario), func(t *testing.T) {
					at := time.Date(2026, 9, 21, 19, 23, 54, 410194000, time.UTC).In(time.FixedZone("driver-offset", offset))
					name := fmt.Sprintf("reading-utc-%d", receiptDriverCounter.Add(1))
					sql.Register(name, readingUTCDriver{at: at, revoked: scenario == "revoked"})
					pool, err := sql.Open(name, "")
					if err != nil {
						t.Fatal(err)
					}
					defer pool.Close()
					store := &WorkspaceStore{db: pool}
					subject := "subject"
					now := at
					want := error(nil)
					switch scenario {
					case "wrong_owner":
						subject = "other"
						want = workbench.ErrNotFound
					case "expired":
						now = at.Add(workbench.ReadingTTL)
						want = workbench.ErrExpired
					case "revoked":
						want = workbench.ErrAccessChanged
					}
					var wire []byte
					if kind == "cost" {
						r, e := store.GetWorkbenchReading(context.Background(), subject, "xwr_"+strings.Repeat("a", 22), now)
						err = e
						if err == nil {
							wire, err = json.Marshal(r)
						}
					} else {
						r, e := store.GetRuntimeReading(context.Background(), subject, "xrt_"+strings.Repeat("b", 22), now)
						err = e
						if err == nil {
							wire, err = json.Marshal(r)
						}
					}
					if !errors.Is(err, want) {
						t.Fatal("read boundary error", err, want)
					}
					if want != nil {
						return
					}
					var got struct {
						ID     string `json:"reading_id"`
						Period struct {
							Start string `json:"start_inclusive"`
							End   string `json:"end_exclusive"`
							Zone  string `json:"timezone"`
						} `json:"period"`
						Cutoff  string `json:"cutoff"`
						Expires string `json:"expires_at"`
						Member  string `json:"membership_ref"`
					}
					if err = json.Unmarshal(wire, &got); err != nil {
						t.Fatal(err)
					}
					for _, v := range []struct {
						got  string
						want time.Time
					}{{got.Period.Start, at.Add(-time.Hour)}, {got.Period.End, at}, {got.Cutoff, at}, {got.Expires, at.Add(workbench.ReadingTTL)}} {
						if v.got != v.want.UTC().Format(time.RFC3339Nano) {
							t.Fatalf("public instant lost UTC or precision: got=%s want=%s", v.got, v.want.UTC().Format(time.RFC3339Nano))
						}
					}
					if got.Period.Zone != "Asia/Shanghai" || got.Member != "membership" {
						t.Fatal("normalization changed display zone or reading identity")
					}
				})
			}
		}
	}
}
