package checkpointepoch

// The PostgreSQL integration tests import db, which now consumes this package's
// original checkpoint proof. Keep those tests external to avoid a test import
// cycle; export their existing private assertions only in the test binary.
var (
	TestValidateInstallationEpochBoundary = validateInstallationEpochBoundary
	TestInstallationPrepareInput          = installationPrepareInput
	TestInstallationPrepareRequestSHA256  = installationPrepareRequestSHA256
	TestHashText                          = hashText
	TestSHA256Pattern                     = sha256Pattern
)

const (
	TestInstallationA = testInstallationA
	TestInstallationB = testInstallationB
)
