package pagination

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func FuzzCursorDecodeRoundTrip(f *testing.F) {
	now := time.Date(2026, 8, 17, 6, 0, 0, 0, time.UTC)
	valid, err := Encode(Cursor{
		Version: Version, Kind: KindTimeline, AsOf: now,
		FilterHash:  "sha256:" + strings.Repeat("a", 64),
		ExecutionID: "xe1_" + strings.Repeat("b", 64),
		Keys:        []string{"2026-08-17T06:00:00Z", "event-1"},
	})
	if err != nil {
		f.Fatal(err)
	}
	for _, seed := range []string{valid, "", "not-base64", "e30", strings.Repeat("a", MaxCursorBytes+1)} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, encoded string) {
		cursor, err := Decode(encoded, now)
		if err != nil {
			return
		}
		canonical, err := Encode(cursor)
		if err != nil {
			t.Fatalf("decoded cursor cannot be encoded: %v", err)
		}
		roundTrip, err := Decode(canonical, now)
		if err != nil {
			t.Fatalf("encoded cursor cannot be decoded: %v", err)
		}
		if !reflect.DeepEqual(cursor, roundTrip) {
			t.Fatalf("cursor round trip changed value: before=%#v after=%#v", cursor, roundTrip)
		}
	})
}
