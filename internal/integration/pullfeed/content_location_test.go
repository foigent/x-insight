package pullfeed

import (
	"context"
	"errors"
	"os"
	"testing"
)

func TestContentJSONLocationActualBytes(t *testing.T) {
	for _, tt := range []struct {
		body, pointer string
		ok            bool
	}{
		{`{"a/b":[{"~key":null}],"":0}`, "/a~1b/0/~0key", true},
		{`{"a/b":[{"~key":null}],"":0}`, "/", true},
		{`{"a":[1,2]}`, "/a/01", false},
		{`{"a":[1,2]}`, "/a/2", false},
		{`{"a":1,"a":2}`, "/a", false},
		{`{"a":1} {"a":2}`, "/a", false},
		{`{"a":1`, "/a", false},
		{`"plain text JSON"`, "", true},
		{`<script>doNotRun()</script>`, "", false},
		{`{"~x":1}`, "/~x", false},
	} {
		t.Run(tt.body+tt.pointer, func(t *testing.T) {
			f, err := os.CreateTemp(t.TempDir(), "owned-*")
			if err != nil {
				t.Fatal(err)
			}
			_, _ = f.WriteString(tt.body)
			file := &ContentFile{file: f, Size: int64(len(tt.body)), BlockBytes: 64}
			defer file.Close()
			err = file.VerifyJSONPointer(context.Background(), tt.pointer)
			if (err == nil) != tt.ok {
				t.Fatalf("location: %v", err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if err = file.VerifyJSONPointer(ctx, tt.pointer); !errors.Is(err, context.Canceled) && tt.ok {
				t.Fatal(err)
			}
		})
	}
}
