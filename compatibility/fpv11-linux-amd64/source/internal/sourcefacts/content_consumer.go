package sourcefacts

import (
	"context"
	"errors"
	"reflect"
	"strconv"
	"time"
)

// ContentCollection carries only the C authorization projection and cursor.
// Credential bytes and content bytes are deliberately absent.
type ContentCollection struct {
	RetentionFloor        ContentBoundary `json:"retention_floor"`
	Coverage              string          `json:"coverage"`
	Gaps                  []ContentGap    `json:"gaps"`
	ID                    string          `json:"id"`
	SourceSystemID        string          `json:"source_system_id"`
	SourceInstance        string          `json:"source_instance"`
	OrgID                 string          `json:"org_id"`
	ProjectID             string          `json:"project_id"`
	CredentialID          string          `json:"credential_id"`
	RegistrationHash      string          `json:"registration_hash"`
	ContentScopeRevision  string          `json:"content_scope_revision"`
	RecoveryEpoch         string          `json:"recovery_epoch"`
	AllowedPurposes       []string        `json:"allowed_purposes"`
	CoverageFrom          ContentBoundary `json:"coverage_from"`
	Cursor                string          `json:"-"`
	LastSequence          string          `json:"last_sequence"`
	HighWatermarkSequence string          `json:"high_watermark_sequence"`
	HighWatermark         string          `json:"-"`
	Status                string          `json:"status"`
	Revision              int64           `json:"revision"`
	RequiresAttention     bool            `json:"requires_attention"`
	LastError             string          `json:"last_error"`
}

func ValidContentCollection(c ContentCollection) bool {
	n, e := strconv.ParseInt(c.CoverageFrom.AfterSequence, 10, 64)
	return uuidPattern.MatchString(c.ID) && uuidPattern.MatchString(c.SourceSystemID) && boundedText(c.SourceInstance, 128) && Positive(c.OrgID) && Positive(c.ProjectID) && boundedText(c.CredentialID, 128) && hashPattern.MatchString(c.RegistrationHash) && boundedText(c.ContentScopeRevision, 128) && Positive(c.RecoveryEpoch) && reflect.DeepEqual(c.AllowedPurposes, []string{ContentPurposeHumanReview}) && ValidTime(c.CoverageFrom.Time) && e == nil && n >= 0 && strconv.FormatInt(n, 10) == c.CoverageFrom.AfterSequence
}
func SameContentCollection(a, b ContentCollection) bool {
	return a.ID == b.ID && a.SourceSystemID == b.SourceSystemID && a.SourceInstance == b.SourceInstance && a.OrgID == b.OrgID && a.ProjectID == b.ProjectID && a.CredentialID == b.CredentialID && a.RegistrationHash == b.RegistrationHash && a.ContentScopeRevision == b.ContentScopeRevision && a.RecoveryEpoch == b.RecoveryEpoch && a.CoverageFrom == b.CoverageFrom && reflect.DeepEqual(a.AllowedPurposes, b.AllowedPurposes)
}

type ContentReader interface {
	FetchContentCapabilities(context.Context) ([]byte, error)
	FetchContentManifests(context.Context, string, int) ([]byte, error)
}
type ContentStore interface {
	LoadContentCollection(context.Context, string) (ContentCollection, error)
	CommitContentManifests(context.Context, ContentCollection, []byte, time.Time) error
	RecordContentFailure(context.Context, ContentCollection, string, time.Time) error
}
type ContentConsumer struct {
	Store       ContentStore
	Reader      ContentReader
	Expected    ContentCollection
	Now         func() time.Time
	AfterCommit func(context.Context, ContentCollection, ContentLimits) error
}

func PreflightContent(ctx context.Context, reader ContentReader, c ContentCollection) (ContentCapabilities, error) {
	if reader == nil || !ValidContentCollection(c) {
		return ContentCapabilities{}, ErrInvalid
	}
	raw, err := reader.FetchContentCapabilities(ctx)
	if err != nil {
		return ContentCapabilities{}, err
	}
	cap, err := DecodeContentCapabilities(raw)
	if err != nil {
		return cap, err
	}
	if cap.SourceInstance != c.SourceInstance || cap.OrgID != c.OrgID || cap.ProjectID != c.ProjectID || cap.ContentScopeRevision != c.ContentScopeRevision || cap.RecoveryEpoch != c.RecoveryEpoch || cap.CoverageFrom != c.CoverageFrom || !reflect.DeepEqual(cap.AllowedPurposes, c.AllowedPurposes) {
		return cap, ErrScopeChanged
	}
	if cap.Status != "active" || cap.CredentialStatus != "feed_active" {
		return cap, ErrUnavailable
	}
	return cap, nil
}
func MatchContentPage(c ContentCollection, p ContentManifestPage) error {
	if !ValidContentCollection(c) || p.SourceInstance != c.SourceInstance || p.OrgID != c.OrgID || p.ProjectID != c.ProjectID || p.ContentScopeRevision != c.ContentScopeRevision || p.RecoveryEpoch != c.RecoveryEpoch || p.CoverageFrom != c.CoverageFrom {
		return ErrScopeChanged
	}
	after := c.LastSequence
	if c.Cursor == "" {
		after = c.CoverageFrom.AfterSequence
		if contentSequence(p.RetentionFloor.AfterSequence) > contentSequence(after) {
			after = p.RetentionFloor.AfterSequence
		}
	}
	if p.AfterSequence != after || (c.HighWatermarkSequence != "" && contentSequence(p.HighWatermarkSequence) < contentSequence(c.HighWatermarkSequence)) {
		return ErrConflict
	}
	for _, entry := range p.Items {
		if entry.Document.OccurredAt < c.CoverageFrom.Time || contentSequence(entry.Sequence) <= contentSequence(c.CoverageFrom.AfterSequence) {
			return ErrScopeChanged
		}
	}
	return nil
}
func (c ContentConsumer) PullOnce(ctx context.Context) (bool, error) {
	if c.Store == nil || c.Reader == nil || !ValidContentCollection(c.Expected) {
		return false, ErrInvalid
	}
	live, err := c.Store.LoadContentCollection(ctx, c.Expected.ID)
	if err != nil {
		return false, err
	}
	if !SameContentCollection(live, c.Expected) {
		return false, ErrScopeChanged
	}
	if live.Status != "active" || live.RequiresAttention {
		return false, ErrUnavailable
	}
	now := time.Now
	if c.Now != nil {
		now = c.Now
	}
	fail := func(cause error) (bool, error) {
		if ctx.Err() != nil {
			return false, ctx.Err()
		}
		if err := c.Store.RecordContentFailure(ctx, live, ContentFailure(cause), now()); err != nil {
			return false, err
		}
		return false, cause
	}
	cap, err := PreflightContent(ctx, c.Reader, live)
	if err != nil {
		return fail(err)
	}
	limit := cap.Limits.MaxManifestItems
	if limit > 50 {
		limit = 50
	}
	raw, err := c.Reader.FetchContentManifests(ctx, live.Cursor, limit)
	if err != nil {
		return fail(err)
	}
	page, err := DecodeContentManifestPage(raw)
	if err != nil {
		return fail(err)
	}
	for _, entry := range page.Items {
		canonical, e := canonicalContentValue(entry.Document)
		if e != nil || int64(len(canonical)) > cap.Limits.MaxManifestBytes {
			return fail(ErrInvalid)
		}
	}
	if len(page.Items) > limit {
		return fail(ErrInvalid)
	}
	if err = MatchContentPage(live, page); err != nil {
		return fail(err)
	}
	if err = c.Store.CommitContentManifests(ctx, live, raw, now()); err != nil {
		if errors.Is(err, ErrInvalid) || errors.Is(err, ErrConflict) {
			return fail(err)
		}
		return false, err
	}
	if c.AfterCommit != nil {
		if err = c.AfterCommit(ctx, live, cap.Limits); err != nil {
			return false, err
		}
	}
	return page.HasMore, nil
}
func ContentFailure(err error) string {
	var h interface {
		error
		HTTPStatusCode() int
		SourceErrorCode() string
	}
	if errors.As(err, &h) {
		code := h.SourceErrorCode()
		switch {
		case h.HTTPStatusCode() == 403 && code == "profile_not_authorized":
			return code
		case h.HTTPStatusCode() == 503 && code == "source_paused":
			return code
		case h.HTTPStatusCode() == 409 && code == "staged_credential_requires_pause":
			return code
		case h.HTTPStatusCode() == 409:
			return "cursor_or_epoch_conflict"
		case h.HTTPStatusCode() == 410:
			return "cursor_expired"
		case h.HTTPStatusCode() == 401 || h.HTTPStatusCode() == 403:
			return "source_authorization_denied"
		}
	}
	switch {
	case errors.Is(err, ErrScopeChanged):
		return "scope_changed"
	case errors.Is(err, ErrInvalid):
		return "invalid_content_page"
	case errors.Is(err, ErrConflict):
		return "source_fact_conflict"
	}
	return "source_unavailable"
}
func ContentNeedsAttention(code string) bool {
	switch code {
	case "profile_not_authorized", "staged_credential_requires_pause", "cursor_or_epoch_conflict", "cursor_expired", "source_authorization_denied", "scope_changed", "invalid_content_page", "source_fact_conflict", "restore_requires_authorization":
		return true
	}
	return false
}
