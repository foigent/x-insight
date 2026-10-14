package pullfeed

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"github.com/foigent/x-insight/internal/connector"
	"github.com/foigent/x-insight/internal/sourcefacts"
	"os"
)

// NewCustodyContentFile uses the existing anonymous request-lifetime spool.
// It verifies the full frozen representation before exposing any content bytes.
func NewCustodyContentFile(ref sourcefacts.FrozenManifestRef, body []byte, budget int64) (*ContentFile, error) {
	block, e := ContentReadBudget(ref, sourcefacts.ContentLimits{MaxReadBytes: MaxContentReadBytes, MaxBufferBytes: MaxContentBlockBytes}, budget)
	if e != nil {
		return nil, e
	}
	digest := sha256.Sum256(body)
	if ref.Representation == nil || int64(len(body)) != ref.Representation.SavedBytes || "sha256:"+hex.EncodeToString(digest[:]) != ref.Representation.Digest {
		return nil, ErrContentIntegrity
	}
	metadata := sourcefacts.ContentReadMetadata{SchemaVersion: "unirouter.content-read-metadata.v1", SourceInstance: ref.SourceInstance, OrgID: ref.OrgID, ProjectID: ref.ProjectID, RequestID: ref.RequestID, SourceFactID: ref.SourceFactID, Revision: ref.Revision, SourceSHA256: ref.SourceSHA256, ContentRef: ref.ContentRef, Representation: *ref.Representation, ExpiresAt: ref.ExpiresAt}
	if sourcefacts.MatchContentReadMetadata(metadata, ref) != nil {
		return nil, ErrContentIntegrity
	}
	raw, e := json.Marshal(metadata)
	if e != nil {
		return nil, e
	}
	object, e := connector.DecodeStrictJSONObject(raw)
	if e != nil {
		return nil, e
	}
	raw, e = connector.CanonicalizeJSONValue(object)
	if e != nil {
		return nil, e
	}
	file, e := os.CreateTemp("", "xinsight-content-*")
	if e != nil {
		return nil, e
	}
	if e = os.Remove(file.Name()); e != nil {
		file.Close()
		os.Remove(file.Name())
		return nil, e
	}
	out := &ContentFile{file: file, MetadataHeader: base64.RawURLEncoding.EncodeToString(raw), Size: int64(len(body)), BlockBytes: block}
	if _, e = file.Write(body); e != nil {
		out.Close()
		return nil, e
	}
	return out, nil
}
