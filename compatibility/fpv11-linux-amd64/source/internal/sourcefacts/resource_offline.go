package sourcefacts

import (
	_ "embed"
	"path/filepath"
	"regexp"
	"time"
)

const ResourceOfflineAuthorizationVersion = "xinsight.source-facts-offline-authorization.v3"
const ResourceOfflineProfile = "xinsight.synthetic-resource-access.v1"
const ResourceOfflineBundleVersion = "xinsight.synthetic-resource-bundle.v1"
const ResourceOfflineConnectorID = "xinsight.synthetic-resources"
const ResourceOfflineSourceType = "xinsight_synthetic_resource"
const ActionOfflineProfile = "xinsight.resource-action.v1"
const ActionOfflineBundleVersion = "xinsight.resource-action-bundle.v1"
const ActionOfflineConnectorID = "xinsight.resource-actions"
const ActionOfflineSourceType = "xinsight_resource_action"

//go:embed contract/resource-access.schema.json
var resourceSchemaBytes []byte

//go:embed contract/resource-action.schema.json
var actionSchemaBytes []byte

func ResourceSchemaSHA256() string { return Hash(resourceSchemaBytes) }
func ActionSchemaSHA256() string   { return Hash(actionSchemaBytes) }

var resourceDatabaseName = regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`)
var resourceReference = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/@-]{0,127}$`)

func (a OfflineAuthorization) validateResource(now time.Time) error {
	synthetic := a.Profile == ResourceOfflineProfile && a.SourceSchemaSHA256 == ResourceSchemaSHA256() && (a.TargetDatabaseSchema == "37" || a.TargetDatabaseSchema == "38" || a.TargetDatabaseSchema == "39" || a.TargetDatabaseSchema == "40" || a.TargetDatabaseSchema == "41" || a.TargetDatabaseSchema == "42" || a.TargetDatabaseSchema == "43")
	action := a.Profile == ActionOfflineProfile && a.SourceSchemaSHA256 == ActionSchemaSHA256() && (a.TargetDatabaseSchema == "40" || a.TargetDatabaseSchema == "41" || a.TargetDatabaseSchema == "42" || a.TargetDatabaseSchema == "43") && (a.Environment == "test" || a.Environment == "development")
	if (!synthetic && !action) ||
		!resourceDatabaseName.MatchString(a.TargetDatabaseName) || !Positive(a.TargetDatabaseOID) || !Positive(a.TargetDatabaseSystemID) ||
		a.ProtocolVersion != "" || a.ProtocolCommit != "" || a.ProtocolReleaseDigest != "" ||
		!namePattern.MatchString(a.AuthorizationID) || !namePattern.MatchString(a.TargetRef) ||
		(a.Environment != "test" && a.Environment != "development" && a.Environment != "integration") ||
		!hashPattern.MatchString(a.MachineIDSHA256) || !boundedText(a.ProductVersion, 128) || !boundedText(a.ProductCommit, 128) ||
		!uuidPattern.MatchString(a.SourceSystemID) || !resourceReference.MatchString(a.SourceInstance) || !Positive(a.OrgID) || !Positive(a.ProjectID) ||
		!uuidPattern.MatchString(a.TargetSourceSystemID) || !resourceReference.MatchString(a.TargetSourceInstance) || !Positive(a.TargetOrgID) || !Positive(a.TargetProjectID) ||
		!namePattern.MatchString(a.OperationID) || !namePattern.MatchString(a.BundleID) || !hashPattern.MatchString(a.BundleSHA256) ||
		!filepath.IsAbs(a.BundlePath) || filepath.Clean(a.BundlePath) != a.BundlePath || a.MaxRecords < 1 || a.MaxRecords > 50 ||
		a.PayloadCaptureMode != "metadata_only" || a.RawPayloadPresent || !ValidTime(a.NotBefore) || !ValidTime(a.ExpiresAt) {
		return ErrAuthorization
	}
	start, _ := time.Parse(TimeFormat, a.NotBefore)
	end, _ := time.Parse(TimeFormat, a.ExpiresAt)
	if !end.After(start) || end.Sub(start) > 2*time.Hour || now.Before(start) || !now.Before(end) {
		return ErrAuthorization
	}
	return nil
}
