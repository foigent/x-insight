package evidence

import "fmt"

func ValidateProjectionConsistency(event map[string]any) error {
	checks := []struct {
		top       string
		group     string
		groupPath string
	}{
		{"request_id", "correlation", "request_id"},
		{"trace_id", "correlation", "trace_id"},
		{"span_id", "correlation", "span_id"},
		{"org_ref", "identity", "org_id"},
		{"project_ref", "identity", "project_id"},
		{"payload_capture_mode", "payload_policy", "payload_capture_mode"},
	}

	for _, check := range checks {
		topVal := stringField(event, check.top)
		groupVal := nestedStringField(event, check.group, check.groupPath)
		if topVal != "" && groupVal != "" && topVal != groupVal {
			return fmt.Errorf("%s=%q does not match %s.%s=%q", check.top, topVal, check.group, check.groupPath, groupVal)
		}
	}
	return nil
}

func stringField(m map[string]any, key string) string {
	v, _ := m[key].(string)
	return v
}

func nestedStringField(m map[string]any, group string, key string) string {
	g, ok := m[group].(map[string]any)
	if !ok {
		return ""
	}
	v, _ := g[key].(string)
	return v
}
