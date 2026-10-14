package workbench

import (
	"errors"
	"fmt"
	"testing"
	"time"
)

var purposeTestBasis = []PurposeBasis{{EvidenceID: "11111111-1111-4111-8111-111111111111", EventHash: "sha256:1111111111111111111111111111111111111111111111111111111111111111"}}
var purposeTestTime = time.Date(2026, 9, 11, 1, 0, 0, 0, time.UTC)

func purposeCommand(action, id, category string, revision int64) PurposeCommand {
	c := PurposeCommand{Action: action, ClaimID: id, Category: category, ExpectedRevision: revision, IdempotencyKey: fmt.Sprintf("purpose-%d", revision), Reason: "Review of the referenced request"}
	if category != "" {
		c.Vocabulary = PurposeVocabulary
		c.Basis = purposeTestBasis
	}
	return c
}

func TestPurposeConflictResolutionNeverResurrectsWithdrawnAdoption(t *testing.T) {
	a, b := "xpc_aaaaaaaaaaaaaaaaaaaaaa", "xpc_bbbbbbbbbbbbbbbbbbbbbb"
	s := EmptyPurposeState()
	apply := func(c PurposeCommand, newID, status, category, adopted string) {
		t.Helper()
		before := s
		var err error
		s, err = ApplyPurpose(s, c, "xs_reviewer00000001", newID, purposeTestTime)
		if err != nil || s.Status != status || s.Category != category || s.AdoptedClaim != adopted {
			t.Fatalf("%s: %+v err=%v", c.Action, s, err)
		}
		if before.Revision+1 != s.Revision {
			t.Fatal("revision lost")
		}
	}
	apply(purposeCommand("declare", "", "software_check", 0), a, "unreviewed", "", "")
	apply(purposeCommand("accept", a, "", 1), "", "adopted", "software_check", a)
	apply(purposeCommand("declare", "", "content_transformation", 2), b, "conflict", "", a)
	apply(purposeCommand("reject", b, "", 3), "", "adopted", "software_check", a)
	apply(purposeCommand("withdraw_adoption", a, "", 4), "", "unknown", "", "")
	if _, err := ApplyPurpose(s, purposeCommand("accept", b, "", 5), "xs_reviewer00000001", "", purposeTestTime); !errors.Is(err, ErrInvalid) {
		t.Fatalf("rejected claim revived: %v", err)
	}
	apply(purposeCommand("accept", a, "", 5), "", "adopted", "software_check", a)
	apply(purposeCommand("withdraw_declaration", a, "", 6), "", "unknown", "", "")
	if _, err := ApplyPurpose(s, purposeCommand("accept", a, "", 7), "xs_reviewer00000001", "", purposeTestTime); !errors.Is(err, ErrInvalid) {
		t.Fatalf("withdrawn claim revived: %v", err)
	}
}

func TestPurposeDecisionOnlyResolvesListedCandidates(t *testing.T) {
	s := EmptyPurposeState()
	a, b, c := "xpc_aaaaaaaaaaaaaaaaaaaaaa", "xpc_bbbbbbbbbbbbbbbbbbbbbb", "xpc_cccccccccccccccccccccc"
	for i, row := range []struct{ id, category string }{{a, "software_check"}, {b, "content_transformation"}, {c, "information_query"}} {
		var err error
		s, err = ApplyPurpose(s, purposeCommand("declare", "", row.category, int64(i)), "human", row.id, purposeTestTime)
		if err != nil {
			t.Fatal(err)
		}
	}
	cmd := purposeCommand("accept", a, "", 3)
	cmd.RejectClaims = []string{b}
	next, err := ApplyPurpose(s, cmd, "human", "", purposeTestTime)
	if err != nil || next.Status != "conflict" || next.AdoptedClaim != a || next.Claims[2].ReviewState != "pending" {
		t.Fatalf("unlisted candidate suppressed: %+v %v", next, err)
	}
	if s.Claims[1].ReviewState != "pending" {
		t.Fatal("prior state mutated")
	}
	next, err = ApplyPurpose(next, purposeCommand("withdraw_declaration", c, "", 4), "human", "", purposeTestTime)
	if err != nil || next.Status != "adopted" {
		t.Fatalf("conflict did not clear: %+v %v", next, err)
	}
}

func TestPurposeModifyPreservesDeclarationAndDoesNotFallback(t *testing.T) {
	a, b := "xpc_aaaaaaaaaaaaaaaaaaaaaa", "xpc_bbbbbbbbbbbbbbbbbbbbbb"
	s, _ := ApplyPurpose(EmptyPurposeState(), purposeCommand("declare", "", "software_check", 0), "first", a, purposeTestTime)
	s, _ = ApplyPurpose(s, purposeCommand("accept", a, "", 1), "second", "", purposeTestTime)
	next, err := ApplyPurpose(s, purposeCommand("modify", a, "content_transformation", 2), "second", b, purposeTestTime)
	if err != nil || next.Category != "content_transformation" || next.Claims[0].Category != "software_check" || next.Claims[0].DeclaredBy != "first" || next.Claims[1].Revises != a {
		t.Fatalf("modify: %+v %v", next, err)
	}
	next, err = ApplyPurpose(next, purposeCommand("withdraw_declaration", b, "", 3), "second", "", purposeTestTime)
	if err != nil || next.Status != "unknown" || next.AdoptedClaim != "" {
		t.Fatalf("old adoption silently restored: %+v %v", next, err)
	}
}

func TestPurposeRejectsDomainMixingAndStaleDecisions(t *testing.T) {
	for _, category := range []string{"customer_service", "retrieval", "unknown", "conflict", ""} {
		c := purposeCommand("declare", "", category, 0)
		if ValidatePurposeCommand(c) == nil {
			t.Fatalf("category accepted: %q", category)
		}
	}
	valid := purposeCommand("declare", "", "software_check", 0)
	for _, change := range []func(*PurposeCommand){func(c *PurposeCommand) { c.Vocabulary = "future" }, func(c *PurposeCommand) { c.Basis = nil }, func(c *PurposeCommand) { c.Reason = " " }, func(c *PurposeCommand) { c.Basis = append(c.Basis, c.Basis[0]) }, func(c *PurposeCommand) { c.Tags = []string{"customer_service"} }, func(c *PurposeCommand) { c.ClaimID = "xpc_aaaaaaaaaaaaaaaaaaaaaa" }} {
		c := valid
		change(&c)
		if ValidatePurposeCommand(c) == nil {
			t.Fatalf("invalid command accepted %+v", c)
		}
	}
	s, _ := ApplyPurpose(EmptyPurposeState(), valid, "human", "xpc_aaaaaaaaaaaaaaaaaaaaaa", purposeTestTime)
	if _, err := ApplyPurpose(s, purposeCommand("accept", s.Claims[0].ID, "", 0), "human", "", purposeTestTime); !errors.Is(err, ErrStaleRevision) {
		t.Fatalf("stale accepted: %v", err)
	}
}
