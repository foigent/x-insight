package investigation

import "context"

// EvidenceRevision names a committed prefix of the evidence ledger. ID is the
// external reference; Order is local to the database and is never sent on the wire.
type EvidenceRevision struct {
	ID    string
	Order int64
}

type evidenceRevisionKey struct{}

func WithEvidenceRevision(ctx context.Context, revision EvidenceRevision) context.Context {
	return context.WithValue(ctx, evidenceRevisionKey{}, revision)
}

func EvidenceRevisionFromContext(ctx context.Context) (EvidenceRevision, bool) {
	revision, present := ctx.Value(evidenceRevisionKey{}).(EvidenceRevision)
	return revision, present
}
