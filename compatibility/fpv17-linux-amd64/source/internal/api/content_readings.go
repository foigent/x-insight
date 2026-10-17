package api

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"time"

	productaccess "github.com/foigent/x-insight/internal/access"
	"github.com/foigent/x-insight/internal/integration/pullfeed"
	"github.com/foigent/x-insight/internal/reviewbody"
	"github.com/foigent/x-insight/internal/sourcefacts"
	"github.com/foigent/x-insight/internal/workbench"
	"github.com/foigent/x-insight/internal/workchain"
)

type ContentReadingStore interface {
	GetRuntimeContents(context.Context, string, string, string, int, string, time.Time) (workbench.RuntimeContents, error)
	ResolveRuntimeContent(context.Context, string, string, string, sourcefacts.FrozenManifestRef, time.Time) (workbench.RuntimeContentRead, error)
}

// ContentSourceResolver selects an exact managed binding. It never accepts a
// browser endpoint, credential or purpose. Production reloads its managed file.
type ContentSourceResolver func(context.Context, sourcefacts.ContentCollection) (*pullfeed.HTTPSource, error)

func (s *Server) handleRuntimeContents(w http.ResponseWriter, r *http.Request) {
	store, ok := s.workbenchStore.(ContentReadingStore)
	if !ok {
		writeProblem(w, 503, "content_not_supported", "Content reading is unavailable", false)
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/v1/runtime/readings/"), "/")
	if r.Method != http.MethodGet || len(parts) != 4 || parts[1] != "requests" || parts[3] != "contents" {
		writeWorkbenchFailure(w, workbench.ErrNotFound)
		return
	}
	if failure := validateQueryKeys(r, "limit", "cursor"); failure != nil {
		writeRequestFailure(w, failure)
		return
	}
	auth, ok := accessAuthentication(r)
	if !ok {
		writeProblem(w, 401, "authentication_required", "Authentication is required", false)
		return
	}
	limit, err := strconv.Atoi(defaultString(r.URL.Query().Get("limit"), "50"))
	if err != nil {
		writeWorkbenchFailure(w, workbench.ErrInvalid)
		return
	}
	result, err := store.GetRuntimeContents(r.Context(), auth.Session.SubjectID, parts[0], parts[2], limit, r.URL.Query().Get("cursor"), s.now().UTC())
	if err != nil {
		writeContentFailure(w, err)
		return
	}
	writeJSON(w, 200, result)
}

func (s *Server) contentCurrentScope(ctx context.Context, auth productaccess.Authentication) (string, error) {
	if s.access == nil || s.scopedStore == nil {
		return "", workbench.ErrScopeForbidden
	}
	if err := s.access.WithCurrentAuthentication(auth, func() error { return nil }); err != nil {
		return "", err
	}
	return s.scopedStore.CurrentRevision(ctx, auth.Session.SubjectID, auth.Session.Role == productaccess.RoleScopedReviewer)
}

// This handler is deliberately outside scopedResponse's 16 MiB JSON buffer.
// Both its current-scope check and member resolver release their transactions
// before any source request or downstream write.
func (s *Server) handleRuntimeContentBytes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowedFor(w, http.MethodGet)
		return
	}
	if len(r.Header.Values("Range")) > 0 {
		writeProblem(w, 400, "unsupported_range", "Partial content reads are not supported", false)
		return
	}
	if failure := validateQueryKeys(r, "source_fact_id", "revision"); failure != nil {
		writeRequestFailure(w, failure)
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/v1/runtime/readings/"), "/")
	if len(parts) != 5 || parts[1] != "requests" || parts[3] != "contents" || !workbench.ValidRuntimeReadingID(parts[0]) || !workbench.ValidRuntimeRequestRef(parts[2]) {
		writeWorkbenchFailure(w, workbench.ErrNotFound)
		return
	}
	auth, ok := accessAuthentication(r)
	if !ok {
		writeProblem(w, 401, "authentication_required", "Authentication is required", false)
		return
	}
	store, ok := s.workbenchStore.(ContentReadingStore)
	if !ok {
		writeProblem(w, 503, "content_not_supported", "Content source is not configured", false)
		return
	}
	// One spool owner per server: <=32 MiB temporary bytes and bounded blocks.
	select {
	case s.contentCapacity <- struct{}{}:
		defer func() { <-s.contentCapacity }()
	default:
		writeProblem(w, 503, "content_read_busy", "Another content read is in progress", true)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), pullfeed.MaxHTTPRequestTimeout)
	defer cancel()
	scope, err := s.contentCurrentScope(ctx, auth)
	if err != nil {
		writeContentFailure(w, err)
		return
	}
	w.Header().Set("X-XInsight-Scope-Revision", scope)
	if expected := r.Header.Get("X-XInsight-Scope-Revision"); expected == "" || expected != scope {
		writeProblem(w, 409, "scope_revision_changed", "Authorization changed; refresh the session", false)
		return
	}
	selector := sourcefacts.FrozenManifestRef{ContentRef: parts[4], SourceFactID: r.URL.Query().Get("source_fact_id"), Revision: r.URL.Query().Get("revision")}
	admission, err := store.ResolveRuntimeContent(ctx, auth.Session.SubjectID, parts[0], parts[2], selector, s.now().UTC())
	if err != nil {
		writeContentFailure(w, err)
		return
	}
	localCheck := func(ctx context.Context) error {
		current, err := s.contentCurrentScope(ctx, auth)
		if err != nil {
			return err
		}
		if current != scope {
			return workbench.ErrAccessChanged
		}
		next, err := store.ResolveRuntimeContent(ctx, auth.Session.SubjectID, parts[0], parts[2], admission.Reference, s.now().UTC())
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(next.Reference, admission.Reference) || !sourcefacts.SameContentCollection(next.Collection, admission.Collection) || next.ContentAuthorizationRevision != admission.ContentAuthorizationRevision || next.MembershipRef != admission.MembershipRef {
			return workbench.ErrAccessChanged
		}
		return nil
	}

	file, custodyCheck, custody, err := s.custodyContent(ctx, auth.Session.SubjectID, admission, pullfeed.MaxContentReadBytes)
	if err != nil {
		writeContentFailure(w, err)
		return
	}
	var source *pullfeed.HTTPSource
	if !custody {
		if s.contentSource == nil {
			writeProblem(w, 503, "content_not_supported", "Content source is not configured", false)
			return
		}
		source, err = s.contentSource(ctx, admission.Collection)
		if err != nil {
			writeContentFailure(w, err)
			return
		}
		defer source.CloseContentConnections()
		caps, err := sourcefacts.PreflightContent(ctx, source, admission.Collection)
		if err != nil {
			writeContentFailure(w, err)
			return
		}
		frozenCheck := localCheck
		localCheck = func(ctx context.Context) error {
			if e := frozenCheck(ctx); e != nil {
				return e
			}
			return s.verifyCurrentContentBinding(ctx, admission.Collection, source)
		}
		file, err = source.FetchContentFile(ctx, admission.Reference, caps.Limits, pullfeed.MaxContentReadBytes, "", localCheck)
		if err != nil {
			writeContentFailure(w, err)
			return
		}
	}
	defer file.Close()
	controller := http.NewResponseController(w)
	check := func(ctx context.Context) error {
		if err := localCheck(ctx); err != nil {
			return err
		}
		if custody {
			if err := custodyCheck(ctx); err != nil {
				return err
			}
		} else {
			live, err := sourcefacts.PreflightContent(ctx, source, admission.Collection)
			if err != nil {
				return err
			}
			block, err := pullfeed.ContentReadBudget(admission.Reference, live.Limits, pullfeed.MaxContentReadBytes)
			if err != nil {
				return err
			}
			if block < file.BlockBytes {
				return pullfeed.ErrContentBudget
			}
		}
		expires, err := time.Parse(time.RFC3339Nano, admission.Reference.ExpiresAt)
		if err != nil || (!custody && !s.now().Before(expires)) {
			return errContentExpired
		}
		return controller.SetWriteDeadline(s.now().Add(10 * time.Second))
	}
	if err = check(ctx); err != nil {
		writeContentFailure(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.FormatInt(file.Size, 10))
	w.Header().Set(pullfeed.ContentMetadataHeader, file.MetadataHeader)
	if custody {
		w.Header().Set("X-XInsight-Content-Expires-At", file.ReadableUntil.UTC().Format(sourcefacts.TimeFormat))
	}
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Disposition", `attachment; filename="content.bin"`)
	w.Header().Set("X-XInsight-Representation", "authorized_projection")
	w.Header().Set("X-XInsight-Projection-Version", workchain.ProjectionVersion)
	// Do not append a JSON error after binary publication. Abort the response;
	// clients only render after full length/hash/scope validation succeeds.
	if err = file.Publish(ctx, w, check); err != nil {
		panic(http.ErrAbortHandler)
	}
	if file.Size == 0 {
		w.WriteHeader(http.StatusOK)
	}
}

var errContentExpired = errors.New("content expired")

func writeContentFailure(w http.ResponseWriter, err error) {
	if errors.Is(err, productaccess.ErrUnauthenticated) {
		writeProblem(w, 401, "authentication_required", "Authentication is required", false)
		return
	}
	if errors.Is(err, sourcefacts.ErrAuthorization) {
		writeProblem(w, 403, "workbench_scope_denied", "Content is not authorized", false)
		return
	}
	if errors.Is(err, sourcefacts.ErrUnavailable) {
		writeProblem(w, 503, "content_source_unavailable", "Content source is unavailable", true)
		return
	}
	if errors.Is(err, errContentExpired) || errors.Is(err, workbench.ErrContentExpired) || errors.Is(err, reviewbody.ErrExpired) {
		writeProblem(w, 410, "content_expired", "Content is no longer available", false)
		return
	}
	if errors.Is(err, pullfeed.ErrContentBudget) {
		writeProblem(w, 503, "content_read_limit", "Selected content exceeds this reader's available capacity", false)
		return
	}
	if errors.Is(err, pullfeed.ErrContentIntegrity) || errors.Is(err, workbench.ErrContentIntegrity) {
		writeProblem(w, 503, "content_integrity_failed", "Content integrity verification failed", false)
		return
	}
	if errors.Is(err, sourcefacts.ErrScopeChanged) {
		writeProblem(w, 409, "scope_revision_changed", "Content authorization changed", false)
		return
	}
	if errors.Is(err, errManagedContentMissing) {
		writeProblem(w, 503, "content_missing", "Content is unavailable", false)
		return
	}
	var sourceErr *pullfeed.HTTPSourceError
	if errors.As(err, &sourceErr) {
		switch sourceErr.Code {
		case "profile_not_authorized":
			writeProblem(w, 403, "workbench_scope_denied", "Content is not authorized", false)
		case "content_scope_changed", "source_epoch_changed":
			writeProblem(w, 409, "scope_revision_changed", "Content authorization changed", false)
		case "content_expired":
			writeProblem(w, 410, "content_expired", "Content is no longer available", false)
		case "content_missing":
			writeProblem(w, 503, "content_missing", "Content is unavailable", false)
		case "content_corrupt", "representation_mismatch":
			writeProblem(w, 503, "content_integrity_failed", "Content integrity verification failed", false)
		default:
			writeProblem(w, 503, "content_source_unavailable", "Content source is unavailable", sourceErr.IsRetryable())
		}
		return
	}
	writeWorkbenchFailure(w, err)
}

func (s *Server) verifyCurrentContentBinding(ctx context.Context, expected sourcefacts.ContentCollection, original *pullfeed.HTTPSource) error {
	current, err := s.contentSource(ctx, expected)
	if err != nil {
		return err
	}
	defer current.CloseContentConnections()
	if !original.SameContentBinding(current) {
		return sourcefacts.ErrScopeChanged
	}
	return nil
}
