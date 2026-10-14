package reviewcopy

import (
	"bytes"
	"context"
	"time"

	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/integration/pullfeed"
	"github.com/foigent/x-insight/internal/reviewbody"
	"github.com/foigent/x-insight/internal/sourcefacts"
)

type URContentSource interface {
	FetchContentFile(context.Context, sourcefacts.FrozenManifestRef, sourcefacts.ContentLimits, int64, string,
		...func(context.Context) error) (*pullfeed.ContentFile, error)
}

type CustodyStore interface {
	CheckURCReviewCopy(context.Context, string, sourcefacts.FrozenManifestRef, time.Time) error
	CommitURCReviewBody(context.Context, string, sourcefacts.FrozenManifestRef, []byte,
		reviewbody.KeyRing, time.Time) (db.ReviewBodyReceipt, error)
}

// CopyURCReviewBody is the independent human-review path. The exact managed
// C source is checked before and during fetch; pullfeed verifies headers,
// length and SHA-256, and XI rechecks policy/manifest in the sealing commit.
// No execution-temporary content endpoint or browser-supplied URL is used.
func CopyURCReviewBody(ctx context.Context, source URContentSource, store CustodyStore,
	collectionID string, ref sourcefacts.FrozenManifestRef, limits sourcefacts.ContentLimits,
	keys reviewbody.KeyRing, now func() time.Time) (db.ReviewBodyReceipt, error) {
	var empty db.ReviewBodyReceipt
	if source == nil || store == nil || now == nil || keys.Validate() != nil {
		return empty, reviewbody.ErrInvalid
	}
	check := func(ctx context.Context) error {
		return store.CheckURCReviewCopy(ctx, collectionID, ref, now().UTC())
	}
	if err := check(ctx); err != nil {
		return empty, err
	}
	file, err := source.FetchContentFile(ctx, ref, limits, 32<<20, "", check)
	if err != nil {
		return empty, err
	}
	defer file.Close()
	var body bytes.Buffer
	if err = file.Publish(ctx, &body, check); err != nil {
		return empty, err
	}
	return store.CommitURCReviewBody(ctx, collectionID, ref, body.Bytes(), keys, now().UTC())
}
