package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"time"

	productaccess "github.com/foigent/x-insight/internal/access"
	"github.com/foigent/x-insight/internal/integration/pullfeed"
	"github.com/foigent/x-insight/internal/sourcefacts"
	"github.com/foigent/x-insight/internal/workbench"
	"github.com/foigent/x-insight/internal/workspace"
)

type contentChoiceStore interface {
	ResolveContentChoice(context.Context, string, workspace.ContentChoice, time.Time) (workbench.RuntimeContentRead, error)
}

// Verify new JSON locations before entering the existing grant transaction.
// The handler still performs its normal strict request decode and authorization.
// Proof is scoped to this request and precise subject/ref/location/relation.
func (s *Server) verifyContentLocations(w http.ResponseWriter, r *http.Request, auth productaccess.Authentication) (*http.Request, bool) {
	if r.Method != http.MethodPost || r.Body == nil {
		return r, true
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxAssessmentBodyBytes))
	if err != nil {
		writeProblem(w, 400, "invalid_request", "Request body is invalid", false)
		return r, false
	}
	r.Body = io.NopCloser(bytes.NewReader(raw))
	if len(raw) == 0 {
		return r, true
	}
	if rejectDuplicateJSONFields(raw) != nil {
		writeProblem(w, 400, "invalid_request", "Request body is invalid", false)
		return r, false
	}
	var object any
	if json.Unmarshal(raw, &object) != nil {
		writeProblem(w, 400, "invalid_request", "Request body is invalid", false)
		return r, false
	}
	choices := []workspace.ContentChoice{}
	seen := map[string]bool{}
	var collect func(any) error
	collect = func(v any) error {
		switch v := v.(type) {
		case map[string]any:
			for key, value := range v {
				if key == "contents" || key == "verification_contents" {
					items, ok := value.([]any)
					if !ok {
						return workspace.ErrInvalid
					}
					for _, item := range items {
						b, _ := json.Marshal(item)
						var c workspace.ContentChoice
						if json.Unmarshal(b, &c) != nil {
							return workspace.ErrInvalid
						}
						if c.Location.Kind == "json_pointer" {
							key := workspace.ContentChoiceKey(c)
							if !seen[key] {
								choices = append(choices, c)
								seen[key] = true
							}
						}
					}
				} else if err := collect(value); err != nil {
					return err
				}
			}
		case []any:
			for _, item := range v {
				if err := collect(item); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if collect(object) != nil || len(choices) > 32 {
		writeProblem(w, 400, "invalid_content_choice", "Content choice is invalid", false)
		return r, false
	}
	if len(choices) == 0 {
		return r, true
	}
	store, ok := s.workspaceStore.(contentChoiceStore)
	if !ok {
		writeProblem(w, 503, "content_not_supported", "Content location verification is unavailable", false)
		return r, false
	}
	select {
	case s.contentCapacity <- struct{}{}:
		defer func() { <-s.contentCapacity }()
	default:
		writeProblem(w, 503, "content_read_busy", "Another content read is in progress", true)
		return r, false
	}
	ctx, cancel := context.WithTimeout(r.Context(), pullfeed.MaxHTTPRequestTimeout)
	defer cancel()
	scope, err := s.contentCurrentScope(ctx, auth)
	if err != nil {
		writeContentFailure(w, err)
		return r, false
	}
	if r.Header.Get("X-XInsight-Scope-Revision") != scope {
		writeProblem(w, 409, "scope_revision_changed", "Authorization changed; refresh the session", false)
		return r, false
	}
	for _, choice := range choices {
		if err = s.verifyContentLocation(ctx, store, auth, scope, choice); err != nil {
			writeContentFailure(w, err)
			return r, false
		}
	}
	return r.WithContext(workspace.WithVerifiedContentLocations(r.Context(), auth.Session.SubjectID, choices)), true
}

func (s *Server) verifyContentLocation(ctx context.Context, store contentChoiceStore, auth productaccess.Authentication, scope string, choice workspace.ContentChoice) error {
	if choice.Reference.Representation == nil || choice.Reference.Representation.SavedBytes > pullfeed.MaxContentJSONLocationBytes {
		return pullfeed.ErrContentBudget
	}
	before, err := store.ResolveContentChoice(ctx, auth.Session.SubjectID, choice, s.now().UTC())
	if err != nil {
		return err
	}
	localCheck := func(ctx context.Context) error {
		current, err := s.contentCurrentScope(ctx, auth)
		if err != nil {
			return err
		}
		if current != scope {
			return workbench.ErrAccessChanged
		}
		after, err := store.ResolveContentChoice(ctx, auth.Session.SubjectID, choice, s.now().UTC())
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(before.Reference, after.Reference) || !sourcefacts.SameContentCollection(before.Collection, after.Collection) || before.ContentAuthorizationRevision != after.ContentAuthorizationRevision {
			return workbench.ErrAccessChanged
		}
		return nil
	}

	file, custodyCheck, custody, err := s.custodyContent(ctx, auth.Session.SubjectID, before, pullfeed.MaxContentJSONLocationBytes)
	if err != nil {
		return err
	}
	var source *pullfeed.HTTPSource
	if custody {
		original := localCheck
		localCheck = func(ctx context.Context) error {
			if e := original(ctx); e != nil {
				return e
			}
			return custodyCheck(ctx)
		}
	} else {
		if s.contentSource == nil {
			return workbench.ErrNotFound
		}
		source, err = s.contentSource(ctx, before.Collection)
		if err != nil {
			return err
		}
		defer source.CloseContentConnections()
		caps, err := sourcefacts.PreflightContent(ctx, source, before.Collection)
		if err != nil {
			return err
		}
		original := localCheck
		localCheck = func(ctx context.Context) error {
			if e := original(ctx); e != nil {
				return e
			}
			return s.verifyCurrentContentBinding(ctx, before.Collection, source)
		}
		file, err = source.FetchContentFile(ctx, before.Reference, caps.Limits, pullfeed.MaxContentJSONLocationBytes, "", localCheck)
		if err != nil {
			return err
		}
	}
	defer file.Close()
	if choice.Location.Pointer == nil {
		return workspace.ErrInvalid
	}
	if err = file.VerifyJSONPointer(ctx, *choice.Location.Pointer); err != nil {
		return err
	}
	if err = localCheck(ctx); err != nil {
		return err
	}
	if custody {
		return nil
	}
	_, err = sourcefacts.PreflightContent(ctx, source, before.Collection)
	return err
}
