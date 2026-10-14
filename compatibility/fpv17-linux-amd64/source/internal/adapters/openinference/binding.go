package openinference

import "github.com/foigent/x-insight/internal/connector"

// Binding is the product-owned registration. Shared protocol pins remain the
// only source of Envelope/lifecycle semantics; OpenInference-specific source
// and payload semantics stay in this adapter package and its reviewed files.
func Binding() connector.SourceNeutralManifestBinding {
	return connector.SourceNeutralManifestBinding{
		BindingVersion: connector.SourceNeutralBindingVersionV2,
		Protocol: connector.ProtocolBinding{
			Version:       ProtocolVersion,
			Tag:           "v2.0.2",
			Commit:        ProtocolCommit,
			ReleaseDigest: ProtocolReleaseSHA,
		},
		Schemas: connector.SourceNeutralSchemaBindings{
			EventEnvelope: connector.ArtifactPin{
				Path: "schemas/event-envelope.v1.schema.json", SHA256: "sha256:6942e85d9b5d8b08ff9828b970c317596394184ef29908cbf2b8fc792187cf26",
			},
			CaptureGap: connector.ArtifactPin{
				Path: "schemas/capture-gap.v1.schema.json", SHA256: "sha256:1a5be69da99f62033ceadf05f66d45296d1d5f8b662830f77c0fc6af49edaec8",
			},
			ConnectorManifest: connector.ArtifactPin{
				Path: "schemas/connector-manifest.v2.schema.json", SHA256: "sha256:ce1e8e84b49e50d0740366e42c64a6899b27834c3cf83116a63a35d3c1a901e5",
			},
			ConnectorLifecycle: connector.ArtifactPin{
				Path: "schemas/connector-lifecycle.v2.schema.json", SHA256: "sha256:b2b8475605fe58e75b97ebe2c6fdab567647757a64112dfeed1b1d4b48afefd0",
			},
			EventBatch: connector.ArtifactPin{
				Path: "schemas/event-batch.v2.schema.json", SHA256: "sha256:36e7da64aa6c33e74e60379274d8426459f1e2ee520e44d0d935fc794b4925da",
			},
			Checkpoint: connector.ArtifactPin{
				Path: "schemas/connector-checkpoint.v2.schema.json", SHA256: "sha256:80bf1e98b24424a25a14985f36aa179c838363c4deb540c7521b20582eccdd6b",
			},
			OperationalReceipt: connector.ArtifactPin{
				Path: "schemas/operational-receipt.v2.schema.json", SHA256: "sha256:d2d091c2571d54df7a65329da049f13a4d7465af38b7916e4ae79e57255a04b6",
			},
			ConnectorError: connector.ArtifactPin{
				Path: "schemas/connector-error.v2.schema.json", SHA256: "sha256:bae3e9a0ee4096feb01655e1f9fc0e4032b3656c0cd0e2a7cad78ec7379a3dee",
			},
			DomainCatalog: connector.ArtifactPin{
				Path: "catalog/domain-catalog.v2.json", SHA256: "sha256:7adfd8210a0dc52e205c714aea0d5e7a26b7617b45cf6e27cbb2ca9d382633fa",
			},
			Source: connector.ArtifactPin{
				Path: "schemas/openinference-span.v1.schema.json", SHA256: "sha256:b5f4da5b586ca4d34af716d52215955c843e5b824f7af7fdbc9a1dfda728b29a",
			},
			Payload: connector.ArtifactPin{
				Path: "schemas/observability-openinference-span-completed.v1.schema.json", SHA256: "sha256:b40e06b7ddc0f5c2800aa81c7562f3d79e35074fd9ba692cb663741491e2c0ce",
			},
		},
		ReferenceManifest: connector.ArtifactPin{
			Path:   "manifests/foigent.openinference.otlp-spans-1.0.0.json",
			SHA256: "sha256:363123d5837741ed78c134e305593d3fd0fdf68040cebff068d3f76d5bc62a1b",
		},
		Mappings: connector.SourceNeutralMappingBindings{
			SourceToSanitized:      connector.ArtifactPin{Path: "mappings/openinference/otlp-json-to-sanitized-span.v1.json", SHA256: "sha256:0120993d85b821b8e17757f54b5878a1b3d71f573cacb53a2246d9419c320c70"},
			SanitizedToEvent:       connector.ArtifactPin{Path: "mappings/openinference/sanitized-span-to-event-envelope.v1.json", SHA256: "sha256:7811a3f1d4cdb80385a72b17e6940dc16b29b995d216c681dba0e4377e6eb44f"},
			EventToM0:              connector.ArtifactPin{Path: "mappings/xinsight/openinference-span-to-m0-m0.1-v2.json", SHA256: "sha256:d5abcabffeb8bb368e5c188b7ed5179b5674092e9421af2c38e548bfdbec7077"},
			ConnectorCompatibility: connector.ArtifactPin{Path: "compatibility/foigent-openinference-bounded-export-1.0.0-to-xinsight-m0.1.json", SHA256: "sha256:0dd606acf3ee1111987d2d74a2f8ad3b9881ca5daf4ad5920351ba5c643e4f60"},
			ReferenceProjection:    connector.ArtifactPin{Path: "mappings/xinsight/model-request-completed-to-m0-m0.1-v1.json", SHA256: "sha256:c812a9c70d55ad0413650218eab6f968fd44974e1e7d77a147d58f62e18e8b03"},
			ConsumerLifecycle:      connector.ArtifactPin{Path: "mappings/xinsight/connector-lifecycle-to-current-xinsight-v1.json", SHA256: "sha256:f1980ad31b1b3be978b39dee854f9e9e1accf99fa266615fe5a43f10dc115503"},
			ConsumerIdentity:       connector.ArtifactPin{Path: "mappings/xinsight/portable-connector-identity-to-current-xinsight-v1.json", SHA256: "sha256:a14c7ed4fe5475cf4ad50da32731824eb8b85fb249dcd5b4bc5b7556d6b33305"},
			ReferenceCompatibility: connector.ArtifactPin{Path: "mappings/xinsight/completed-execution-compatibility-seed-v1.json", SHA256: "sha256:5cd40aa6c0256a70ad7317c05e078759685e0469be77a3e960a976adc3158e8e"},
			V1ToV2Compatibility:    connector.ArtifactPin{Path: "compatibility/v1-to-v2-source-neutral-matrix.json", SHA256: "sha256:bdc2527ca947bf7b6a9c1feb2d5a97a9426b5b3ec602690a2d7b23d3f85ace52"},
		},
		Conformance: connector.SourceNeutralConformanceBindings{
			Contract:     connector.ArtifactPin{Path: "conformance/source-neutral-conformance-contract.v2.json", SHA256: "sha256:f820936448590144156c2e499134eb4b3142ca0265275b6e9bdf1e5cd7855e14"},
			VectorBundle: connector.ArtifactPin{Path: "vectors/source-neutral-vector-bundle.v2.json", SHA256: "sha256:9e50bb4570dcc1ff07157ad6198641e937357b641ae052cddcb3420b52ad11ed"},
			Runner:       connector.ArtifactPin{Path: "scripts/run-source-neutral-v2-consumer-kit.sh", SHA256: "sha256:a7fb4d8fb4013f8aae1bbf86c517cd52f33438366ad843e5438c48ac8e4914f2"},
		},
		Connector: connector.PortableConnectorBinding{
			ConnectorID:        ConnectorID,
			ConnectorVersion:   ConnectorVersion,
			SourceProduct:      "openinference",
			EventType:          "observability.span.completed",
			SchemaRef:          "foigent://events/observability/openinference-span-completed/v1",
			SchemaVersion:      "1",
			SourceSchema:       SourceSchemaV1,
			SourceSchemaSHA256: "sha256:b5f4da5b586ca4d34af716d52215955c843e5b824f7af7fdbc9a1dfda728b29a",
		},
		Projection: connector.SourceNeutralProjectionBinding{
			ProfileID:         ProjectionProfileID,
			SourceSystemID:    SourceSystemID,
			AdapterManifestID: AdapterManifestID,
		},
	}
}

func validateBinding(binding connector.SourceNeutralManifestBinding) error {
	return connector.ValidateExactSourceNeutralRegistration(binding, Binding())
}
