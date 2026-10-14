package checkpointepoch

import (
	"errors"
	"strings"
	"testing"
)

func TestDecodePrepareRequestRejectsDuplicateUnknownAndTrailingFields(t *testing.T) {
	valid := `{
  "schema_version":"xinsight.connector-checkpoint-epoch-prepare.v1",
  "binding":{
    "source_system_id":"11111111-1111-4111-8111-111111111111",
    "adapter_manifest_id":"22222222-2222-4222-8222-222222222222",
    "cursor_key":"unirouter.completed-execution.v1"
  },
  "worker_unit":"xinsight-feed.service",
  "next_source_instance":"unirouter-campaign-2",
  "next_cursor":"cursor-2",
  "next_high_watermark":"cursor-2",
  "authorization_ref":"decision://campaign-2"
}`
	input, err := DecodePrepareRequest([]byte(valid))
	if err != nil || input.NextCursor != "cursor-2" {
		t.Fatalf("valid request input=%+v err=%v", input, err)
	}
	for _, test := range []struct {
		name    string
		payload string
	}{
		{name: "nested duplicate", payload: strings.Replace(valid,
			`"cursor_key":"unirouter.completed-execution.v1"`,
			`"cursor_key":"unirouter.completed-execution.v1","cursor_key":"unirouter.completed-execution.v1"`, 1)},
		{name: "unknown", payload: strings.Replace(valid, `"authorization_ref":"decision://campaign-2"`,
			`"authorization_ref":"decision://campaign-2","unexpected":true`, 1)},
		{name: "trailing", payload: valid + `{}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := DecodePrepareRequest([]byte(test.payload)); !errors.Is(err, ErrInvalid) {
				t.Fatalf("error=%v", err)
			}
		})
	}
}
