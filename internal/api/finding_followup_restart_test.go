package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/workspace"
)

func TestResponsibilityOrdinaryApplicationRestart(t *testing.T) {
	f := startResponsibilityProduction(t)
	old := responsibilityLogin(t, f, "reviewer.one")
	_, command := responsibilityCommand(t, f, "xs_1111111111111111")
	status, first := old.call(t, "POST", f.reviewPath()+"-actions", command, nil)
	if status != 200 {
		t.Fatalf("initial save status=%d", status)
	}
	f.server.Close()
	f.store.Close()
	reopened, err := db.OpenWorkspaceStore(context.Background(), f.workspaceDSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { reopened.Close() })
	f.store = reopened
	f.reloadAccounts(t, f.config)
	old.base, old.client.Transport = f.server.URL, f.server.Client().Transport
	if status, _ = old.call(t, "GET", f.reviewPath(), nil, nil); status != 401 {
		t.Fatalf("old session survived application restart: %d", status)
	}
	fresh := responsibilityLogin(t, f, "reviewer.one")
	status, replay := fresh.call(t, "POST", f.reviewPath()+"-actions", command, nil)
	if status != 200 || !bytes.Equal(first, replay) {
		t.Fatal("persisted receipt changed after store and service restart")
	}
	status, raw := fresh.call(t, "GET", f.reviewPath(), nil, nil)
	var review workspace.FollowupReview
	if status != 200 || json.Unmarshal(raw, &review) != nil || review.Followup.Version != 1 || len(review.History.Items) != 1 {
		t.Fatal("current projection or history lost across ordinary application restart")
	}
	t.Log("Same database reopened through a new WorkspaceStore and production handler/access service: old session 401, new human session exact receipt replay, projection version 1 and one history action preserved; PostgreSQL process not restarted")
}
