package api

import (
	"context"
	"errors"
	"github.com/foigent/x-insight/internal/integration/pullfeed"
	"github.com/foigent/x-insight/internal/reviewbody"
	"github.com/foigent/x-insight/internal/workbench"
)

type managedCustodyStore interface {
	IsManagedCustodyCollection(context.Context, string) (bool, error)
}

// Admission must already have been resolved through the subject's frozen
// request/member/manifest. A bare content reference never establishes custody.
func (s *Server) custodyContent(ctx context.Context, subject string, admission workbench.RuntimeContentRead, budget int64) (*pullfeed.ContentFile, func(context.Context) error, bool, error) {
	managed, ok := s.workbenchStore.(managedCustodyStore)
	if !ok {
		return nil, nil, false, nil
	}
	selected, e := managed.IsManagedCustodyCollection(ctx, admission.Collection.ID)
	if e != nil {
		return nil, nil, true, e
	}
	if !selected {
		return nil, nil, false, nil
	}
	store, ok := s.workbenchStore.(reviewBodyReadStore)
	if !ok || s.reviewBodyKeys.ActiveID == "" {
		return nil, nil, true, errManagedContentMissing
	}
	ref := admission.Reference
	body, e := store.ReadReviewBody(ctx, subject, ref.ContentRef, s.reviewBodyKeys, s.now().UTC())
	if e != nil {
		if ae := store.AuditReviewBodyRead(ctx, subject, ref.ContentRef, "denied", s.now().UTC()); ae != nil {
			return nil, nil, true, ae
		}
		if errors.Is(e, workbench.ErrNotFound) {
			e = errManagedContentMissing
		}
		return nil, nil, true, e
	}
	defer clear(body.Bytes)
	o := body.Owner
	if o.SourceKind != "ur_c" || body.CollectionID != admission.Collection.ID || o.SourceSystemID != ref.SourceSystemID || o.SourceInstance != ref.SourceInstance || o.OrgID != ref.OrgID || o.ProjectID != ref.ProjectID || o.SourceFactID != ref.SourceFactID || o.SourceRevision != ref.Revision || o.SourceSHA256 != ref.SourceSHA256 || o.ContentRef != ref.ContentRef || ref.Representation == nil || o.ByteDomain != ref.Representation.ByteDomain || body.SHA256 != ref.Representation.Digest {
		return nil, nil, true, pullfeed.ErrContentIntegrity
	}
	check := func(ctx context.Context) error {
		if !s.now().Before(body.ExpiresAt) {
			return reviewbody.ErrExpired
		}
		return store.ReauthorizeReviewBodyRead(ctx, subject, ref.ContentRef, s.now().UTC())
	}
	if e = check(ctx); e != nil {
		return nil, nil, true, e
	}
	file, e := pullfeed.NewCustodyContentFile(ref, body.Bytes, budget)
	if e != nil {
		return nil, nil, true, e
	}
	file.ReadableUntil = body.ExpiresAt
	if !admission.ReadingExpiresAt.IsZero() && admission.ReadingExpiresAt.Before(file.ReadableUntil) {
		file.ReadableUntil = admission.ReadingExpiresAt
	}
	clear(body.Bytes)
	if e = store.AuditReviewBodyRead(ctx, subject, ref.ContentRef, "accepted", s.now().UTC()); e != nil {
		file.Close()
		return nil, nil, true, e
	}
	return file, check, true, nil
}

var errManagedContentMissing = errors.New("managed custody content missing")
