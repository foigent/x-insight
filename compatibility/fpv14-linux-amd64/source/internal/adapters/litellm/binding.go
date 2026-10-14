package litellm

import "github.com/foigent/x-insight/internal/connector"

// Binding returns the compiled, product-owned registry entry for the sole
// LiteLLM Connector registration. Its editable JSON mirror is equality-tested.
func Binding() connector.ManifestBinding {
	return connector.ManifestBinding{
		BindingVersion: connector.ProjectionBindingVersionV1,
		Protocol: connector.ProtocolBinding{
			Version:       ProtocolVersion,
			Tag:           "v1.1.2",
			Commit:        ProtocolCommit,
			ReleaseDigest: ProtocolReleaseSHA,
		},
		Schemas: connector.SchemaBindings{
			EventEnvelope: connector.ArtifactPin{
				Path: "schemas/event-envelope.v1.schema.json", SHA256: "sha256:6942e85d9b5d8b08ff9828b970c317596394184ef29908cbf2b8fc792187cf26",
			},
			ModelRequestCompleted: connector.ArtifactPin{
				Path: "schemas/model-request-completed.v2.schema.json", SHA256: "sha256:75e5af3fe8a433054988c6afa3faf5a9a9fbe03a300c8d1d5a869912ecd9154e",
			},
			ConnectorManifest: connector.ArtifactPin{
				Path: "schemas/connector-manifest.v1.schema.json", SHA256: "sha256:59c405086310d0dfb2aaff37e55d73853e04319e6ccf659c66f98d8ba704a205",
			},
			ConnectorLifecycle: connector.ArtifactPin{
				Path: "schemas/connector-lifecycle.v1.schema.json", SHA256: "sha256:bd5c730587041ddc7ad6f66bca4deca35edf5f9b5b92b761ac53701f37e18d5e",
			},
			EventBatch: connector.ArtifactPin{
				Path: "schemas/event-batch.v1.schema.json", SHA256: "sha256:8e2133922bcb77cf032309290593665df1bf56cfa47e433dd573d0e42f36eef9",
			},
			Checkpoint: connector.ArtifactPin{
				Path: "schemas/connector-checkpoint.v1.schema.json", SHA256: "sha256:8be2fd290d6db8d288a9b10ac6d9aa0b55a66ff21ea0fb55ac0af2d1b66bf800",
			},
			OperationalReceipt: connector.ArtifactPin{
				Path: "schemas/operational-receipt.v1.schema.json", SHA256: "sha256:3c3b99e009e208456039d3bfe26d84af7e3f1af23589e7fa62a1e1a61e0fb3c6",
			},
			ConnectorError: connector.ArtifactPin{
				Path: "schemas/connector-error.v1.schema.json", SHA256: "sha256:f449ba6314f1f730be5a0b188dc6d2cc2704e8f5d84a4eebecac576221e4ca56",
			},
		},
		ReferenceManifest: connector.ArtifactPin{
			Path: "manifests/foigent.litellm.spend-logs-1.0.0.json", SHA256: "sha256:86a10565a8c66abe5b31d6c39e853cebf5e9b90721d6784ea0bf7c28d2c20693",
		},
		Compatibility: connector.CompatibilityBindings{
			ProjectionContractSHA256:  "sha256:72bb90b6a5ddf0f44f7773a7378c0f7f458438f9fc1363d31a65602c0547f088",
			LifecycleMappingSHA256:    "sha256:f1980ad31b1b3be978b39dee854f9e9e1accf99fa266615fe5a43f10dc115503",
			IdentityMappingSHA256:     "sha256:a14c7ed4fe5475cf4ad50da32731824eb8b85fb249dcd5b4bc5b7556d6b33305",
			ReferenceVectorSHA256:     "sha256:9997bcade1f65f613e2176266a35b43aef64547c3ed4ba87e61de94080803dc7",
			CanonicalVectorSHA256:     "sha256:50fc8e8be08c37381611d6423e29780894741fb108fc4569df7c895452e5d4db",
			ConformanceContractSHA256: "sha256:1863e2f72c9dcd7e65a18a4ce42cd463a16749c5ee3e5f420936ed2ebef199a5",
		},
		Connector: connector.PortableConnectorBinding{
			ConnectorID:        ConnectorID,
			ConnectorVersion:   ConnectorVersion,
			SourceProduct:      "litellm",
			EventType:          "model.request.completed",
			SchemaRef:          "foigent://events/model/request-completed/v2",
			SchemaVersion:      "2",
			SourceSchema:       SourceSchemaV1,
			SourceSchemaSHA256: "sha256:bc6360ac33f17f57dc7845b8d6dc413736a635d6e1fb14b16dde1a9ae8ea0949",
		},
		Projection: connector.ProjectionBinding{
			ProfileID:         ProjectionProfileID,
			SourceSystemID:    SourceSystemID,
			AdapterManifestID: AdapterManifestID,
			CursorKey:         CursorKey,
		},
	}
}

func validateBinding(binding connector.ManifestBinding) error {
	return connector.ValidateExactRegistration(binding, Binding())
}
