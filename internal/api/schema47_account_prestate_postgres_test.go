package api_test

import (
	"bytes"
	"encoding/json"
	"encoding/pem"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// Run actual CI helper logic through production HTTPS handlers and native PG.
// Its CLI main still requires a disposable CI host and fixed port9443; only
// this isolated test constructor accepts the actual ephemeral fixture port.
func TestSchema47AccountPrestateNativeAPIContractPostgres(t *testing.T) {
	server, _, _, _ := startManagedAccountAPI(t)
	cert := filepath.Join(t.TempDir(), "fixture-ca.pem")
	if err := os.WriteFile(cert, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}), 0600); err != nil {
		t.Fatal(err)
	}
	script, err := filepath.Abs("../../scripts/ci-fpv21-account-prestate.py")
	if err != nil {
		t.Fatal(err)
	}
	input, _ := json.Marshal(map[string]string{"url": server.URL, "ca": cert, "password": managedPassword, "script": script})
	code := "import json,sys,runpy,urllib.parse\nf=json.load(sys.stdin);m=runpy.run_path(f[\"script\"],run_name=\"native_fixture_import\")\nu=urllib.parse.urlsplit(f[\"url\"]);assert u.hostname==\"127.0.0.1\"\nclient=m[\"Client\"](f[\"url\"],f[\"ca\"],expected_port=u.port)\nclient.login(\"owner\",f[\"password\"])\nbefore=client.call(\"/api/v1/access/project-read-grants\")\nstate=m[\"prepare\"](client,f[\"password\"])\naccount=next(a for a in client.call(\"/api/v1/access/accounts\")[\"items\"] if a[\"subject_id\"]==state[\"subject\"])\nassert account[\"needs_activation\"] and account[\"revision\"]==state[\"revision\"] and state[\"revision\"]>3\nassert before==client.call(\"/api/v1/access/project-read-grants\")\nresult=m[\"verify\"](client,state)\nassert result[\"spent_tickets_refused\"]==2 and result[\"pending_ticket_completed_once\"] and result[\"normal_login_after_upgrade\"] and result[\"new_project_grants\"]==0\nprint(json.dumps(result,sort_keys=True))\n"
	command := exec.Command("python3", "-I", "-B", "-c", code)
	command.Stdin = bytes.NewReader(input)
	raw, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("native account prestate helper failed: %v: %s", err, raw)
	}
	var receipt map[string]any
	if json.Unmarshal(raw, &receipt) != nil || receipt["subject_preserved"] != true {
		t.Fatal("invalid safe helper result")
	}
	// API preparation/challenge only; this is not a Linux upgrade.
	t.Log("native schema47 helper: recovered account revision>3; two old links refused; pending link completed once; normal reader login; no grant change")
}
