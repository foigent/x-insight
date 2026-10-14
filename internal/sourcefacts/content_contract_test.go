package sourcefacts

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/foigent/x-insight/internal/connector"
)

const contentTestTime = "2026-09-15T00:00:00.000000Z"
const contentTestExpiry = "2026-09-16T00:00:00.000000Z"
const contentTestSourceSystem = "11111111-2222-3333-4444-555555555555"

func contentTestWire(t *testing.T, value any) []byte {
	t.Helper()
	b, err := canonicalContentValue(value)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func contentTestManifest() ContentManifest {
	m := ContentManifest{
		SchemaVersion: ContentManifestVersion, SourceInstance: "synthetic-source", OrgID: "7", ProjectID: "9", RequestID: "request-one", Profile: ContentProfile,
		Revision: "1", OccurredAt: contentTestTime, RecordedAt: contentTestTime, Protocol: "openai.responses", ObservationPoint: "provider_response", AttemptID: "attempt-one", ContentItemID: "body-0",
		Capture:        ContentCapture{Mode: "full_content", PolicyRef: "policy-one", PolicyVersion: "1", PolicyEffectiveAt: contentTestTime, Completeness: "complete", Reason: "none"},
		ContentRef:     "uc1_" + strings.Repeat("a", 64),
		Representation: &ContentRepresentation{MediaType: "application/octet-stream", Encoding: "identity", ByteDomain: "stored_representation_before_http_content_encoding", SavedBytes: 0, Digest: "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"},
		CapturedAt:     contentTestTime, ExpiresAt: contentTestExpiry,
	}
	contentTestIdentity(&m)
	return m
}

func contentTestIdentity(m *ContentManifest) {
	m.SourceFactID = SourceID(ContentManifestVersion, m.SourceInstance, m.OrgID, m.ProjectID, ContentProfile, m.RequestID, m.ObservationPoint, m.AttemptID, m.ContentItemID)
	m.EventID = SourceID(ContentManifestVersion+".revision", m.SourceFactID, m.Revision)
}

func contentTestEntry(t *testing.T, m ContentManifest) ContentManifestEntry {
	t.Helper()
	return ContentManifestEntry{Sequence: "1", Document: m, SourceSHA256: Hash(contentTestWire(t, m)), Availability: ContentAvailability{State: "available", EvaluatedAt: contentTestTime}}
}

func contentTestPage(t *testing.T) ContentManifestPage {
	return ContentManifestPage{
		SchemaVersion: ContentManifestPageVersion, Profile: ContentProfile, SourceInstance: "synthetic-source", OrgID: "7", ProjectID: "9", GeneratedAt: contentTestTime,
		ContentScopeRevision: "scope-1", RecoveryEpoch: "1", CoverageFrom: ContentBoundary{contentTestTime, "0"}, RetentionFloor: ContentBoundary{contentTestTime, "0"},
		AfterSequence: "0", LastSequence: "1", HighWatermarkSequence: "1", NextCursor: "opaque_next", HighWatermark: "opaque_high", Coverage: "bounded", Gaps: []ContentGap{}, Items: []ContentManifestEntry{contentTestEntry(t, contentTestManifest())},
	}
}

func TestContentContractSchemaIdentity(t *testing.T) {
	if Hash(contentSchemaBytes) != "sha256:bb6c1ef5a7bf0e82d5f807cea7f2f53261e45525900bfbdac7370e876d72f180" {
		t.Fatal("shared candidate.2 schema changed")
	}
	if _, err := contentSchemas(); err != nil {
		t.Fatal(err)
	}
}

func TestContentContractRegexpCompatibility(t *testing.T) {
	re, err := contentRegexp(`^[^\u0000-\u001f\u007f]+$`)
	if err != nil {
		t.Fatal(err)
	}
	for r := rune(0); r <= 128; r++ {
		if got, want := re.MatchString(string(r)), r >= 32 && r != 127; got != want {
			t.Fatalf("rune %d: got %v want %v", r, got, want)
		}
	}
	if re.MatchString("") || !re.MatchString("合成") {
		t.Fatal("text boundary changed")
	}
}

func TestContentContractManifestSavedAndUnsaved(t *testing.T) {
	for _, state := range []string{"complete", "partial", "pending", "not_captured"} {
		t.Run(state, func(t *testing.T) {
			m := contentTestManifest()
			m.Capture.Completeness = state
			switch state {
			case "partial":
				m.Capture.Reason = "stream_interrupted"
			case "pending":
				m.Capture.Reason = "capture_in_progress"
			case "not_captured":
				m.Capture.Mode, m.Capture.Reason = "metadata_only", "policy_metadata_only"
			}
			if state == "pending" || state == "not_captured" {
				m.ContentRef, m.Representation, m.CapturedAt, m.ExpiresAt = "", nil, "", ""
			}
			decoded, err := DecodeContentManifest(contentTestWire(t, m))
			if err != nil || decoded.Capture.Completeness != state {
				t.Fatalf("decode: %v", err)
			}
			entry := contentTestEntry(t, m)
			ref, err := FrozenContentManifestRef(contentTestSourceSystem, entry)
			if err != nil || ref.CaptureStatus != state {
				t.Fatalf("freeze: %v", err)
			}
			if state == "pending" || state == "not_captured" {
				if ref.Representation != nil || ref.ContentRef != "" {
					t.Fatal("unsaved capture has saved reference")
				}
				m.ContentRef = "uc1_" + strings.Repeat("a", 64)
				if _, err := DecodeContentManifest(contentTestWire(t, m)); err == nil {
					t.Fatal("unsaved capture accepted saved reference")
				}
			}
		})
	}
}

func TestContentContractManifestRejectsInvalidDomain(t *testing.T) {
	cases := map[string]func(*ContentManifest){
		"wrong_fact":        func(m *ContentManifest) { m.SourceFactID = "uf1_" + strings.Repeat("f", 64) },
		"wrong_event":       func(m *ContentManifest) { m.EventID = m.SourceFactID },
		"wrong_scope":       func(m *ContentManifest) { m.ProjectID = "8" },
		"missing_attempt":   func(m *ContentManifest) { m.AttemptID = ""; contentTestIdentity(m) },
		"first_predecessor": func(m *ContentManifest) { m.PreviousRevision = "1" },
		"revision_gap": func(m *ContentManifest) {
			m.Revision, m.PreviousRevision, m.PreviousHash, m.CorrectionReason = "3", "1", Hash(nil), "correction"
			contentTestIdentity(m)
		},
		"recorded_before_observed":     func(m *ContentManifest) { m.RecordedAt = "2026-09-14T00:00:00.000000Z" },
		"expiry_equal_capture":         func(m *ContentManifest) { m.ExpiresAt = m.CapturedAt },
		"bad_calendar":                 func(m *ContentManifest) { m.CapturedAt = "2026-02-30T00:00:00.000000Z" },
		"int64_overflow":               func(m *ContentManifest) { m.OrgID = "9223372036854775808"; contentTestIdentity(m) },
		"unpromised_encoding":          func(m *ContentManifest) { m.Representation.Encoding = "gzip" },
		"wrong_byte_domain":            func(m *ContentManifest) { m.Representation.ByteDomain = "decoded_characters" },
		"redaction_unidentified":       func(m *ContentManifest) { m.Capture.Mode = "redacted" },
		"plain_with_redaction":         func(m *ContentManifest) { m.Representation.RedactionRef = &ContentVersionRef{"redactor", "1"} },
		"saved_without_representation": func(m *ContentManifest) { m.Representation = nil },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			m := contentTestManifest()
			change(&m)
			if _, err := DecodeContentManifest(contentTestWire(t, m)); err == nil {
				t.Fatal("invalid manifest accepted")
			}
		})
	}
}

func TestContentContractStrictRawJSON(t *testing.T) {
	raw := contentTestWire(t, contentTestManifest())
	for name, bad := range map[string][]byte{
		"noncanonical":     append([]byte(" "), raw...),
		"trailing":         append(append([]byte{}, raw...), []byte("{}")...),
		"duplicate":        bytes.Replace(raw, []byte(`"revision":"1"`), []byte(`"revision":"1","revision":"1"`), 1),
		"null":             bytes.Replace(raw, []byte(`"attempt_id":"attempt-one"`), []byte(`"attempt_id":null`), 1),
		"unknown":          bytes.Replace(raw, []byte(`"attempt_id":"attempt-one"`), []byte(`"attempt_id":"attempt-one","extra":"no"`), 1),
		"bad_surrogate":    bytes.Replace(raw, []byte(`"attempt-one"`), []byte(`"\ud800"`), 1),
		"bad_utf8":         bytes.Replace(raw, []byte(`"attempt-one"`), []byte{'"', 0xff, '"'}, 1),
		"fractional_bytes": bytes.Replace(raw, []byte(`"saved_bytes":0`), []byte(`"saved_bytes":0.5`), 1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := DecodeContentManifest(bad); err == nil {
				t.Fatal("invalid raw input accepted")
			}
		})
	}
}

func TestContentContractFrozenReferenceAndReadMetadata(t *testing.T) {
	entry := contentTestEntry(t, contentTestManifest())
	ref, err := FrozenContentManifestRef(contentTestSourceSystem, entry)
	if err != nil {
		t.Fatal(err)
	}
	metadata := ContentReadMetadata{SchemaVersion: ContentReadMetadataVersion, SourceInstance: ref.SourceInstance, OrgID: ref.OrgID, ProjectID: ref.ProjectID, RequestID: ref.RequestID, SourceFactID: ref.SourceFactID, Revision: ref.Revision, SourceSHA256: ref.SourceSHA256, ContentRef: ref.ContentRef, Representation: *ref.Representation, ExpiresAt: ref.ExpiresAt}
	if err := MatchContentReadMetadata(metadata, ref); err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeContentReadMetadata(append([]byte(" "), contentTestWire(t, metadata)...)); err == nil {
		t.Fatal("noncanonical read header accepted")
	}
	for name, change := range map[string]func(*ContentReadMetadata){
		"source":      func(m *ContentReadMetadata) { m.SourceInstance = "other-source" },
		"project":     func(m *ContentReadMetadata) { m.ProjectID = "8" },
		"request":     func(m *ContentReadMetadata) { m.RequestID = "other-request" },
		"revision":    func(m *ContentReadMetadata) { m.Revision = "2" },
		"source_hash": func(m *ContentReadMetadata) { m.SourceSHA256 = Hash([]byte("different manifest")) },
		"content_ref": func(m *ContentReadMetadata) { m.ContentRef = "uc1_" + strings.Repeat("b", 64) },
		"length":      func(m *ContentReadMetadata) { m.Representation.SavedBytes++ },
		"digest":      func(m *ContentReadMetadata) { m.Representation.Digest = Hash([]byte("different bytes")) },
		"expiry":      func(m *ContentReadMetadata) { m.ExpiresAt = "2026-09-17T00:00:00.000000Z" },
	} {
		t.Run(name, func(t *testing.T) {
			changed := metadata
			change(&changed)
			if MatchContentReadMetadata(changed, ref) == nil {
				t.Fatal("mismatched read accepted")
			}
		})
	}
	entry.SourceSHA256 = Hash([]byte("wrong"))
	if _, err := FrozenContentManifestRef(contentTestSourceSystem, entry); err == nil {
		t.Fatal("wrong source hash frozen")
	}
	entry = contentTestEntry(t, contentTestManifest())
	ref.SourceSHA256 = ref.Representation.Digest
	if MatchContentManifestReference(ref, entry) == nil {
		t.Fatal("body hash accepted as manifest hash")
	}
	if _, err := FrozenContentManifestRef("not-a-managed-source-uuid", entry); err == nil {
		t.Fatal("invalid source system accepted")
	}
}

func TestContentContractRevisionSuccessor(t *testing.T) {
	previous := contentTestEntry(t, contentTestManifest())
	currentManifest := contentTestManifest()
	currentManifest.Revision, currentManifest.PreviousRevision, currentManifest.PreviousHash, currentManifest.CorrectionReason = "2", "1", previous.SourceSHA256, "record-correction"
	contentTestIdentity(&currentManifest)
	current := contentTestEntry(t, currentManifest)
	if err := ValidateContentManifestSuccessor(previous, current); err != nil {
		t.Fatal(err)
	}
	current.Document.Representation.Digest = Hash([]byte("changed representation"))
	current.SourceSHA256 = Hash(contentTestWire(t, current.Document))
	if ValidateContentManifestSuccessor(previous, current) == nil {
		t.Fatal("same ref changed representation")
	}
	current.Document.ContentRef = "uc1_" + strings.Repeat("b", 64)
	current.SourceSHA256 = Hash(contentTestWire(t, current.Document))
	if err := ValidateContentManifestSuccessor(previous, current); err != nil {
		t.Fatal(err)
	}
	current.Document.PreviousHash = Hash(nil)
	current.SourceSHA256 = Hash(contentTestWire(t, current.Document))
	if ValidateContentManifestSuccessor(previous, current) == nil {
		t.Fatal("wrong predecessor hash")
	}
}

func TestContentContractManifestPage(t *testing.T) {
	p := contentTestPage(t)
	if _, err := DecodeContentManifestPage(contentTestWire(t, p)); err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*ContentManifestPage){
		"hash":              func(p *ContentManifestPage) { p.Items[0].SourceSHA256 = Hash(nil) },
		"scope":             func(p *ContentManifestPage) { p.OrgID = "8" },
		"sequence":          func(p *ContentManifestPage) { p.Items[0].Sequence = "2" },
		"last":              func(p *ContentManifestPage) { p.LastSequence = "0" },
		"more":              func(p *ContentManifestPage) { p.HasMore = true },
		"overflow":          func(p *ContentManifestPage) { p.HighWatermarkSequence = "9223372036854775808" },
		"retention_gap":     func(p *ContentManifestPage) { p.RetentionFloor.AfterSequence = "1" },
		"expired_available": func(p *ContentManifestPage) { p.Items[0].Availability.EvaluatedAt = contentTestExpiry },
		"premature_expiry":  func(p *ContentManifestPage) { p.Items[0].Availability.State = "expired" },
		"inverted_gap": func(p *ContentManifestPage) {
			p.Coverage = "partial"
			p.Gaps = []ContentGap{{Reason: "missing_predecessor", RangeKnown: true, AfterSequence: "2", ThroughSequence: "1"}}
		},
	} {
		t.Run(name, func(t *testing.T) {
			p := contentTestPage(t)
			change(&p)
			if _, err := DecodeContentManifestPage(contentTestWire(t, p)); err == nil {
				t.Fatal("invalid page accepted")
			}
		})
	}
	p.Items[0].Availability = ContentAvailability{State: "expired", EvaluatedAt: contentTestExpiry}
	if _, err := DecodeContentManifestPage(contentTestWire(t, p)); err != nil {
		t.Fatal("current expiry must preserve complete historical manifest", err)
	}
	p.Items = []ContentManifestEntry{}
	p.AfterSequence = "1"
	if _, err := DecodeContentManifestPage(contentTestWire(t, p)); err != nil {
		t.Fatal("empty page with stable cursor position", err)
	}
}

func TestContentContractAccessAndCapabilities(t *testing.T) {
	a := ContentAccess{RegistrationHash: Hash([]byte("existing-registration-bytes")), Profile: ContentProfile, ContentScopeRevision: "c-scope-1", AllowedPurposes: []string{}}
	if _, err := DecodeContentAccess(contentTestWire(t, a)); err != nil {
		t.Fatal(err)
	}
	a.Enabled = true
	if _, err := DecodeContentAccess(contentTestWire(t, a)); err == nil {
		t.Fatal("enabled without approved coverage/purpose")
	}
	a.CoverageFrom = &ContentBoundary{contentTestTime, "0"}
	a.AllowedPurposes = []string{ContentPurposeHumanReview}
	if _, err := DecodeContentAccess(contentTestWire(t, a)); err != nil {
		t.Fatal(err)
	}
	a.AllowedPurposes = []string{"automated_analysis"}
	if _, err := DecodeContentAccess(contentTestWire(t, a)); err == nil {
		t.Fatal("unapproved purpose")
	}
	c := ContentCapabilities{SchemaVersion: ContentCapabilitiesVersion, Profile: ContentProfile, SourceInstance: "synthetic-source", OrgID: "7", ProjectID: "9", Status: "active", CredentialStatus: "feed_active", SupportedProtocols: []string{"openai.responses"}, ObservationPoints: []string{"client_request"}, CaptureModes: []string{"disabled", "metadata_only", "full_content"}, AllowedPurposes: []string{ContentPurposeHumanReview}, ContentScopeRevision: "scope-1", RecoveryEpoch: "1", CoverageFrom: ContentBoundary{contentTestTime, "0"}, RetentionFloor: ContentBoundary{contentTestTime, "0"}, Limits: ContentLimits{100, 100, 10, 65536, 100}, Endpoints: ContentEndpoints{"/api/v1/integrations/xinsight/source/v2/content-capabilities", "/api/v1/integrations/xinsight/source/v2/content-manifests", "/api/v1/integrations/xinsight/source/v2/contents/{content_ref}"}}
	if _, err := DecodeContentCapabilities(contentTestWire(t, c)); err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*ContentCapabilities){
		"staged":                  func(c *ContentCapabilities) { c.CredentialStatus = "feed_staged" },
		"paused":                  func(c *ContentCapabilities) { c.Status = "paused" },
		"wrong_profile":           func(c *ContentCapabilities) { c.Profile = RuntimeProfile },
		"read_smaller_than_saved": func(c *ContentCapabilities) { c.Limits.MaxReadBytes = 99 },
		"buffer_larger_than_read": func(c *ContentCapabilities) { c.Limits.MaxBufferBytes = 101 },
	} {
		t.Run(name, func(t *testing.T) {
			changed := c
			change(&changed)
			if _, err := DecodeContentCapabilities(contentTestWire(t, changed)); err == nil {
				t.Fatal("invalid capabilities accepted")
			}
		})
	}
}

func TestContentContractChoiceLocations(t *testing.T) {
	ref, err := FrozenContentManifestRef(contentTestSourceSystem, contentTestEntry(t, contentTestManifest()))
	if err != nil {
		t.Fatal(err)
	}
	zero, one := int64(0), int64(1)
	choice := ContentChoice{Scope: "finding", Reference: ref, Location: ContentLocation{Kind: "byte_range", Offset: &zero, Length: &zero}, Relation: "supports"}
	if _, err := DecodeContentChoice(contentTestWire(t, choice)); err != nil {
		t.Fatal("zero byte representation valid", err)
	}
	choice.Location.Length = &one
	if _, err := DecodeContentChoice(contentTestWire(t, choice)); err == nil {
		t.Fatal("range exceeds saved bytes")
	}
	root := ""
	choice.Location = ContentLocation{Kind: "json_pointer", Pointer: &root}
	if _, err := DecodeContentChoice(contentTestWire(t, choice)); err != nil {
		t.Fatal("root pointer", err)
	}
	choice.Location.Offset = &zero
	if _, err := DecodeContentChoice(contentTestWire(t, choice)); err == nil {
		t.Fatal("mixed location kinds")
	}
	choice.Location.Offset = nil
	choice.Reference.CaptureStatus = "pending"
	choice.Reference.ContentRef = ""
	choice.Reference.Representation = nil
	choice.Reference.ExpiresAt = ""
	if _, err := DecodeContentChoice(contentTestWire(t, choice)); err == nil {
		t.Fatal("pending capture admitted as chosen evidence")
	}
}

func contentTestProjectionWire(t *testing.T, p ContentProjection) []byte {
	t.Helper()
	b := contentTestWire(t, p)
	m, err := connector.DecodeStrictJSONObject(b)
	if err != nil {
		t.Fatal(err)
	}
	delete(m, "projection_sha256")
	b, err = connector.CanonicalizeJSONValue(m)
	if err != nil {
		t.Fatal(err)
	}
	p.ProjectionSHA256 = Hash(b)
	return contentTestWire(t, p)
}

func TestContentContractProjectionExactProtocolIDs(t *testing.T) {
	root := ""
	for _, kind := range []string{"message", "tool_call", "tool_result"} {
		t.Run(kind, func(t *testing.T) {
			link := ContentLink{Kind: kind, Location: ContentLocation{Kind: "json_pointer", Pointer: &root}, AssociationBasis: "exact_protocol_id", EvidenceLevel: "captured_message", SessionRef: "session-one"}
			if kind == "message" {
				link.MessageRole = "assistant"
				link.ProtocolMessageID = "message-one"
			} else {
				link.ToolCallID = "call-one"
			}
			if kind == "tool_result" {
				link.EvidenceLevel = "client_reported_result"
			}
			p := ContentProjection{ParserRef: ContentVersionRef{"parser", "1"}, Links: []ContentLink{link}}
			wire := contentTestProjectionWire(t, p)
			if _, err := DecodeContentProjection(wire); err != nil {
				t.Fatal(err)
			}
			p.Links[0].SessionRef = ""
			if _, err := DecodeContentProjection(contentTestProjectionWire(t, p)); err == nil {
				t.Fatal("exact id without session")
			}
			p.Links[0] = link
			p.Links[0].ToolCallID = ""
			p.Links[0].ProtocolMessageID = ""
			if _, err := DecodeContentProjection(contentTestProjectionWire(t, p)); err == nil {
				t.Fatal("exact id missing kind-specific id")
			}
			var decoded map[string]any
			if err := json.Unmarshal(wire, &decoded); err != nil {
				t.Fatal(err)
			}
			decoded["projection_sha256"] = Hash(nil)
			if _, err := DecodeContentProjection(contentTestWire(t, decoded)); err == nil {
				t.Fatal("projection digest mismatch")
			}
		})
	}
}
