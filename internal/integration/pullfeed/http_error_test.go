package pullfeed

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAcceptedCursorExpiredErrorFixtureClassifiesPermanentCode(t *testing.T) {
	payload, err := os.ReadFile(filepath.Join(
		"..", "..", "..", "contracts", "completed-execution", "v1", "fixtures", "error-cursor-expired.json",
	))
	if err != nil {
		t.Fatalf("read cursor-expired fixture: %v", err)
	}
	endpointError, ok := decodeHTTPError(payload)
	if !ok ||
		endpointError.Code != "completed_execution_feed.cursor_expired" ||
		endpointError.Retryable {
		t.Fatalf("endpoint error=%+v valid=%t", endpointError, ok)
	}
}
