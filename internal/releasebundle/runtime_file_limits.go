package releasebundle

import "fmt"

const (
	MaxRuntimeConfigFileBytes  = 8 << 20
	MaxRuntimeWorkerFileBytes  = 1 << 20
	MaxRuntimeEnvironmentBytes = 64 << 10
)

// The signed payload already bounds executable size. Do not apply the small
// configuration-file limit to the Go launcher, which can exceed 8 MiB.
func RuntimeFileLimit(path string) int64 {
	switch path {
	case "bin/xinsight-launcher":
		return MaxPayloadFileBytes
	case "deploy/xinsight.service", "deploy/postgres.compose.yml":
		return MaxRuntimeConfigFileBytes
	case "deploy/xinsight-feed.service", "deploy/xinsight-litellm-feed.service", "deploy/xinsight-source-facts.service", "deploy/xinsight-connector-controller.service":
		return MaxRuntimeWorkerFileBytes
	case "deploy/xinsight.env.example", "deploy/99-xinsight-managed-launcher.conf":
		return MaxRuntimeEnvironmentBytes
	default:
		return 0
	}
}

func validateRuntimeFileSizes(files []FileEntry) error {
	for _, file := range files {
		if limit := RuntimeFileLimit(file.Path); limit != 0 && (file.SizeBytes <= 0 || file.SizeBytes > limit) {
			return fmt.Errorf("runtime file %s has size %d; required range is 1..%d bytes", file.Path, file.SizeBytes, limit)
		}
	}
	return nil
}
