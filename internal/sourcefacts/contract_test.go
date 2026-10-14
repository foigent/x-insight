package sourcefacts

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"
)

func TestFixedSourceVectors(t *testing.T) {
	var bundle struct {
		Valid []struct {
			Name         string
			Canonical    string
			SourceSHA256 string `json:"source_sha256"`
		}
		Invalid []struct {
			Name string
			Wire string
		}
	}
	raw, e := os.ReadFile("contract/golden-source-facts.json")
	if e != nil {
		t.Fatal(e)
	}
	if e = json.Unmarshal(raw, &bundle); e != nil {
		t.Fatal(e)
	}
	for _, v := range bundle.Valid {
		t.Run(v.Name, func(t *testing.T) {
			d, e := DecodeDocument([]byte(v.Canonical))
			if e != nil {
				t.Fatal(e)
			}
			if Hash([]byte(v.Canonical)) != v.SourceSHA256 {
				t.Fatal("source digest changed")
			}
			wire, e := Envelope(d, []byte(v.Canonical))
			if e != nil {
				t.Fatal(e)
			}
			if _, e = ValidateItem(wire, d.Profile); e != nil {
				t.Fatal(e)
			}
		})
	}
	for _, v := range bundle.Invalid {
		t.Run(v.Name, func(t *testing.T) {
			if _, e := DecodeDocument([]byte(v.Wire)); e == nil {
				t.Fatal("invalid source accepted")
			}
		})
	}
}

func TestFixedPageVectors(t *testing.T) {
	type vector struct {
		Name, Profile string
		Org           string `json:"org_id"`
		Project       string `json:"project_id"`
		Previous      string `json:"previous_cursor"`
		Page          json.RawMessage
	}
	var bundle struct{ Valid, Invalid []vector }
	raw, e := os.ReadFile("contract/golden-pages.json")
	if e != nil {
		t.Fatal(e)
	}
	if e = json.Unmarshal(raw, &bundle); e != nil {
		t.Fatal(e)
	}
	for _, v := range bundle.Valid {
		t.Run(v.Name, func(t *testing.T) {
			var header Page
			json.Unmarshal(v.Page, &header)
			previous := v.Previous
			// This static fixture retains the generic "fixture-start" marker.
			// The pinned real Handler bootstraps only on an empty request cursor;
			// test that actual transition without weakening ordinary empty pages.
			if v.Name == "bootstrap_empty" {
				previous = ""
			}
			if _, e := DecodePage(v.Page, header.ProducerInstance, v.Org, v.Project, v.Profile, previous, 50); e != nil {
				t.Fatal(e)
			}
			if v.Name == "bootstrap_empty" {
				padded := append(append([]byte(nil), v.Page...), bytes.Repeat([]byte(" "), MaxPageBytes-1-len(v.Page))...)
				if _, e := DecodePage(padded, header.ProducerInstance, v.Org, v.Project, v.Profile, previous, 50); e != nil {
					t.Fatal("valid page immediately below byte limit", e)
				}
				if _, e := DecodePage(append(padded, ' '), header.ProducerInstance, v.Org, v.Project, v.Profile, previous, 50); e == nil {
					t.Fatal("page at byte limit accepted")
				}
				if _, e := DecodeDocument(bytes.Repeat([]byte(" "), MaxDocumentBytes+1)); e == nil {
					t.Fatal("oversized source document accepted")
				}
			}
		})
	}
	for _, v := range bundle.Invalid {
		t.Run(v.Name, func(t *testing.T) {
			if _, e := DecodePage(v.Page, "synthetic-ur", v.Org, v.Project, v.Profile, v.Previous, 50); e == nil {
				t.Fatal("invalid page accepted")
			}
		})
	}
}
