package workchain

import (
	"encoding/json"
	"github.com/foigent/x-insight/internal/investigation"
	"github.com/foigent/x-insight/internal/workbench"
	"strings"
	"testing"
)

func TestProjectionPreservesOriginalHashAndWithholdsFields(t *testing.T) {
	original := investigation.EvidenceEventDetail{Source: investigation.EventSource{EventHash: "original-hash", SourceRef: "identity-in-url"}, Identity: investigation.IdentityGroup{ActorRef: "private-actor", APIKeyRef: "private-key"}, Decision: investigation.DecisionGroup{DecisionReason: "private-reason"}, Usage: investigation.UsageGroup{CostMicros: "999"}, PayloadPolicy: investigation.PayloadPolicyGroup{PayloadRef: "private-raw"}}
	view := ProjectEvent(original, Fields{})
	raw, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"private-actor", "private-key", "private-reason", "identity-in-url", "private-raw", "999"} {
		if strings.Contains(string(raw), secret) {
			t.Fatal("projection leaked", secret)
		}
	}
	if view.Source.EventHash != original.Source.EventHash || view.ViewBoundary == nil || original.Identity.ActorRef != "private-actor" || original.PayloadPolicy.PayloadRef != "private-raw" {
		t.Fatal("hash/source mutated")
	}
	full := ProjectEvent(original, Fields{Context: true, Identity: true, Cost: true})
	if full.PayloadPolicy.PayloadRef != "" || full.Source.EventHash != original.Source.EventHash {
		t.Fatal("raw payload escaped explicit projection")
	}
}
func TestGrantFieldsNeverCombineAcrossDifferentProjects(t *testing.T) {
	a := Access{Authorization: workbench.Authorization{Grants: []workbench.Grant{{SourceSystemID: "A", SourceProject: "one", AllowDetails: true, AllowSourceContext: true}, {SourceSystemID: "A", SourceProject: "two", AllowDetails: true, AllowSourceIdentity: true, AllowSourceCost: true}}}}
	fields, ok := a.FieldsFor("A", "one", "")
	if !ok || !fields.Context || fields.Identity || fields.Cost {
		t.Fatal("cross-project capabilities combined", fields)
	}
	if _, ok = a.FieldsFor("A", "", ""); ok {
		t.Fatal("unknown project matched project grant")
	}
}

func TestContextPermissionDoesNotExposeUnclassifiedIdentityReferences(t *testing.T) {
	e := investigation.EvidenceEventDetail{Capability: investigation.CapabilityGroup{Model: "allowed-model", Provider: "allowed-provider", TargetRef: "private-target", Channel: "private-channel"}, PayloadPolicy: investigation.PayloadPolicyGroup{RetentionPolicyRef: "private-policy", PayloadRef: "private-payload"}}
	raw, err := json.Marshal(ProjectEvent(e, Fields{Context: true}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "private-") || !strings.Contains(string(raw), "allowed-provider") || !strings.Contains(string(raw), "allowed-model") {
		t.Fatalf("profile classification: %s", raw)
	}
}
