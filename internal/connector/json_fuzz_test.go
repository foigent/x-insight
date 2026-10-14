package connector

import (
	"reflect"
	"testing"
)

func FuzzStrictJSONObjectCanonicalRoundTrip(f *testing.F) {
	for _, seed := range [][]byte{
		[]byte(`{}`),
		[]byte(`{"a":1,"nested":{"ok":true},"items":[null,"value"]}`),
		[]byte(`{"duplicate":1,"duplicate":2}`),
		[]byte(`{"unsafe":9007199254740992}`),
		[]byte(`{"unicode":"\uD83D\uDE00"}`),
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, payload []byte) {
		if len(payload) > 64<<10 {
			return
		}
		decoded, err := decodeStrictJSONObject(payload, "fuzz object")
		if err != nil {
			return
		}
		canonical, err := canonicalJSON(decoded)
		if err != nil {
			t.Fatalf("accepted object cannot be canonicalized: %v", err)
		}
		roundTrip, err := decodeStrictJSONObject(canonical, "canonical fuzz object")
		if err != nil {
			t.Fatalf("canonical object cannot be decoded: %v", err)
		}
		if !reflect.DeepEqual(decoded, roundTrip) {
			t.Fatalf("canonical round trip changed value: before=%#v after=%#v", decoded, roundTrip)
		}
	})
}
