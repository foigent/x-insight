package db

import (
	"strings"
	"testing"

	"github.com/foigent/x-insight/internal/investigation"
)

func TestValidateImportProvenanceSeparatesLivePullFromSidecarProof(t *testing.T) {
	artifactHash := "sha256:" + strings.Repeat("a", 64)
	payloadHash := "sha256:" + strings.Repeat("b", 64)
	manifestHash := "sha256:" + strings.Repeat("c", 64)
	testCases := []struct {
		name       string
		provenance investigation.ImportProvenance
		wantError  string
	}{
		{
			name: "valid sidecar",
			provenance: investigation.ImportProvenance{
				IntegrationMode:        investigation.IntegrationSidecar,
				ProofClass:             investigation.ProofOperatorApproved,
				AssuranceMode:          investigation.AssuranceOperatorAttested,
				SourceArtifactSHA256:   artifactHash,
				ApprovalManifestSHA256: manifestHash,
				ExchangeID:             "exchange-1",
			},
		},
		{
			name: "sidecar requires exchange",
			provenance: investigation.ImportProvenance{
				IntegrationMode:        investigation.IntegrationSidecar,
				ProofClass:             investigation.ProofOperatorApproved,
				AssuranceMode:          investigation.AssuranceOperatorAttested,
				SourceArtifactSHA256:   artifactHash,
				ApprovalManifestSHA256: manifestHash,
			},
			wantError: "exchange ID",
		},
		{
			name: "sidecar rejects live payload hash",
			provenance: investigation.ImportProvenance{
				IntegrationMode:        investigation.IntegrationSidecar,
				ProofClass:             investigation.ProofOperatorApproved,
				AssuranceMode:          investigation.AssuranceOperatorAttested,
				SourceArtifactSHA256:   artifactHash,
				SourcePayloadSHA256:    payloadHash,
				ApprovalManifestSHA256: manifestHash,
				ExchangeID:             "exchange-1",
			},
			wantError: "must not include a source payload hash",
		},
		{
			name: "valid live pull",
			provenance: investigation.ImportProvenance{
				IntegrationMode:     investigation.IntegrationPull,
				ProofClass:          investigation.ProofOperatorApproved,
				AssuranceMode:       investigation.AssuranceContractedPull,
				SourcePayloadSHA256: payloadHash,
			},
		},
		{
			name: "valid source-neutral connector pull",
			provenance: investigation.ImportProvenance{
				IntegrationMode:     investigation.IntegrationConnectorPull,
				ProofClass:          investigation.ProofOperatorApproved,
				AssuranceMode:       investigation.AssuranceContractedPull,
				SourcePayloadSHA256: payloadHash,
			},
		},
		{
			name: "connector pull rejects checkpoint watermark",
			provenance: investigation.ImportProvenance{
				IntegrationMode:     investigation.IntegrationConnectorPull,
				ProofClass:          investigation.ProofOperatorApproved,
				AssuranceMode:       investigation.AssuranceContractedPull,
				SourcePayloadSHA256: payloadHash,
				ImportWatermark:     "connector-checkpoint",
			},
			wantError: "only to durable worker state",
		},
		{
			name: "valid bounded offline connector import",
			provenance: investigation.ImportProvenance{
				IntegrationMode:     investigation.IntegrationBoundedOffline,
				ProofClass:          investigation.ProofOperatorApproved,
				AssuranceMode:       investigation.AssuranceBoundedOffline,
				SourcePayloadSHA256: payloadHash,
			},
		},
		{
			name: "bounded offline rejects delivery watermark",
			provenance: investigation.ImportProvenance{
				IntegrationMode:     investigation.IntegrationBoundedOffline,
				ProofClass:          investigation.ProofOperatorApproved,
				AssuranceMode:       investigation.AssuranceBoundedOffline,
				SourcePayloadSHA256: payloadHash,
				ImportWatermark:     "bundle-receipt-state",
			},
			wantError: "only to an operational receipt",
		},
		{
			name: "bounded offline rejects contracted pull assurance",
			provenance: investigation.ImportProvenance{
				IntegrationMode:     investigation.IntegrationBoundedOffline,
				ProofClass:          investigation.ProofOperatorApproved,
				AssuranceMode:       investigation.AssuranceContractedPull,
				SourcePayloadSHA256: payloadHash,
			},
			wantError: "requires bounded-offline assurance",
		},
		{
			name: "live pull requires payload hash",
			provenance: investigation.ImportProvenance{
				IntegrationMode: investigation.IntegrationPull,
				ProofClass:      investigation.ProofOperatorApproved,
				AssuranceMode:   investigation.AssuranceContractedPull,
			},
			wantError: "requires a source payload hash",
		},
		{
			name: "live pull rejects sidecar fields",
			provenance: investigation.ImportProvenance{
				IntegrationMode:        investigation.IntegrationPull,
				ProofClass:             investigation.ProofOperatorApproved,
				AssuranceMode:          investigation.AssuranceContractedPull,
				SourceArtifactSHA256:   artifactHash,
				SourcePayloadSHA256:    payloadHash,
				ApprovalManifestSHA256: manifestHash,
				ExchangeID:             "exchange-1",
			},
			wantError: "must not include sidecar",
		},
		{
			name: "live pull rejects malformed payload hash",
			provenance: investigation.ImportProvenance{
				IntegrationMode:     investigation.IntegrationPull,
				ProofClass:          investigation.ProofOperatorApproved,
				AssuranceMode:       investigation.AssuranceContractedPull,
				SourcePayloadSHA256: "sha256:not-a-hash",
			},
			wantError: "source payload hash must be",
		},
		{
			name: "live pull rejects page watermark",
			provenance: investigation.ImportProvenance{
				IntegrationMode:     investigation.IntegrationPull,
				ProofClass:          investigation.ProofOperatorApproved,
				AssuranceMode:       investigation.AssuranceContractedPull,
				SourcePayloadSHA256: payloadHash,
				SourceWatermark:     "page-high-watermark",
			},
			wantError: "only to durable worker state",
		},
		{
			name: "valid synthetic",
			provenance: investigation.ImportProvenance{
				IntegrationMode:      investigation.IntegrationFixture,
				ProofClass:           investigation.ProofSyntheticFixture,
				AssuranceMode:        investigation.AssuranceSyntheticFixture,
				SourceArtifactSHA256: artifactHash,
			},
		},
		{
			name: "synthetic rejects live payload hash",
			provenance: investigation.ImportProvenance{
				IntegrationMode:      investigation.IntegrationFixture,
				ProofClass:           investigation.ProofSyntheticFixture,
				AssuranceMode:        investigation.AssuranceSyntheticFixture,
				SourceArtifactSHA256: artifactHash,
				SourcePayloadSHA256:  payloadHash,
			},
			wantError: "must not include live payload",
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			err := validateImportProvenance(testCase.provenance)
			if testCase.wantError == "" {
				if err != nil {
					t.Fatalf("valid provenance rejected: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), testCase.wantError) {
				t.Fatalf("error=%v, want substring %q", err, testCase.wantError)
			}
		})
	}
}

func TestDeterministicImportProvenanceIDPreservesLegacyIdentity(t *testing.T) {
	provenance := investigation.ImportProvenance{
		SourceSystemID:         "11111111-1111-1111-1111-111111111111",
		AdapterManifestID:      uniRouterAdapterManifestIDV2,
		IntegrationMode:        investigation.IntegrationSidecar,
		ProofClass:             investigation.ProofOperatorApproved,
		AssuranceMode:          investigation.AssuranceOperatorAttested,
		SourceArtifactSHA256:   "sha256:" + strings.Repeat("a", 64),
		ApprovalManifestSHA256: "sha256:" + strings.Repeat("b", 64),
		ExchangeID:             "exchange-1",
		ImportedRevision:       "revision-1",
		TimelineComplete:       true,
	}
	legacyID := deterministicUUID(strings.Join([]string{
		"import_provenance",
		provenance.SourceSystemID,
		provenance.AdapterManifestID,
		provenance.IntegrationMode,
		provenance.ProofClass,
		provenance.AssuranceMode,
		provenance.SourceArtifactSHA256,
		provenance.ApprovalManifestSHA256,
		provenance.ExchangeID,
		provenance.SourceRevision,
		provenance.ImportedRevision,
		provenance.SourceWatermark,
		provenance.ImportWatermark,
		"true",
		"",
		"",
	}, "\x00"))
	if got := deterministicImportProvenanceID(provenance); got != legacyID {
		t.Fatalf("legacy provenance identity changed: got=%s want=%s", got, legacyID)
	}

	live := investigation.ImportProvenance{
		SourceSystemID:      provenance.SourceSystemID,
		AdapterManifestID:   uniRouterCompletedAdapterIDV1,
		IntegrationMode:     investigation.IntegrationPull,
		ProofClass:          investigation.ProofOperatorApproved,
		AssuranceMode:       investigation.AssuranceContractedPull,
		SourcePayloadSHA256: "sha256:" + strings.Repeat("c", 64),
		TimelineComplete:    true,
	}
	otherPayload := live
	otherPayload.SourcePayloadSHA256 = "sha256:" + strings.Repeat("d", 64)
	if deterministicImportProvenanceID(live) == deterministicImportProvenanceID(otherPayload) {
		t.Fatal("live provenance identity did not include source payload hash")
	}
	connectorPull := live
	connectorPull.IntegrationMode = investigation.IntegrationConnectorPull
	if deterministicImportProvenanceID(live) ==
		deterministicImportProvenanceID(connectorPull) {
		t.Fatal("completed-execution and connector pull provenance identities collided")
	}
	boundedOffline := live
	boundedOffline.IntegrationMode = investigation.IntegrationBoundedOffline
	boundedOffline.AssuranceMode = investigation.AssuranceBoundedOffline
	if deterministicImportProvenanceID(connectorPull) ==
		deterministicImportProvenanceID(boundedOffline) {
		t.Fatal("pull and bounded-offline provenance identities collided")
	}
}
