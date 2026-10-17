package pullfeed

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strconv"
	"strings"
)

// A location lookup has a separate, explicit parsing budget. Larger saved
// representations remain readable with byte ranges; they are never partially
// parsed and reported as a valid JSON location. Token traversal avoids building
// an unbounded map/array graph from attacker-controlled JSON.
const MaxContentJSONLocationBytes int64 = 1 << 20

func (f *ContentFile) VerifyJSONPointer(ctx context.Context, pointer string) error {
	if f == nil || f.file == nil {
		return ErrContentIntegrity
	}
	if f.Size > MaxContentJSONLocationBytes || len(pointer) > 4096 {
		return ErrContentBudget
	}
	if _, err := f.file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	raw, err := io.ReadAll(io.LimitReader(f.file, MaxContentJSONLocationBytes+1))
	if err != nil {
		return err
	}
	if int64(len(raw)) != f.Size {
		return ErrContentIntegrity
	}
	var parts []string
	if pointer != "" {
		if !strings.HasPrefix(pointer, "/") {
			return ErrContentIntegrity
		}
		parts = strings.Split(pointer[1:], "/")
		for i, p := range parts {
			for j := 0; j < len(p); j++ {
				if p[j] == '~' {
					if j+1 >= len(p) || (p[j+1] != '0' && p[j+1] != '1') {
						return ErrContentIntegrity
					}
					j++
				}
			}
			parts[i] = strings.ReplaceAll(strings.ReplaceAll(p, "~1", "/"), "~0", "~")
		}
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	found := false
	var walk func([]string, bool, int) error
	walk = func(path []string, selected bool, depth int) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if depth > 128 {
			return ErrContentBudget
		}
		token, err := d.Token()
		if err != nil {
			return ErrContentIntegrity
		}
		atTarget := selected && len(path) == 0
		if atTarget {
			found = true
			selected = false
		}
		delim, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		switch delim {
		case '{':
			matched := false
			for d.More() {
				keyToken, err := d.Token()
				if err != nil {
					return ErrContentIntegrity
				}
				key, ok := keyToken.(string)
				if !ok {
					return ErrContentIntegrity
				}
				match := selected && len(path) > 0 && path[0] == key
				if match && matched {
					return ErrContentIntegrity
				}
				matched = matched || match
				next := path
				if match {
					next = path[1:]
				}
				if err := walk(next, match, depth+1); err != nil {
					return err
				}
			}
		case '[':
			index := 0
			for d.More() {
				match := selected && len(path) > 0 && path[0] == strconv.Itoa(index)
				next := path
				if match {
					next = path[1:]
				}
				if err := walk(next, match, depth+1); err != nil {
					return err
				}
				index++
			}
		default:
			return ErrContentIntegrity
		}
		end, err := d.Token()
		if err != nil || end != json.Delim(map[json.Delim]rune{'{': '}', '[': ']'}[delim]) {
			return ErrContentIntegrity
		}
		return nil
	}
	if err = walk(parts, true, 0); err != nil {
		return err
	}
	if _, err = d.Token(); err != io.EOF || !found {
		return ErrContentIntegrity
	}
	return nil
}
