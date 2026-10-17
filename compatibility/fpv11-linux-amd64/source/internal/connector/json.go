package connector

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"unicode/utf16"
	"unicode/utf8"
)

const maxSafeJSONInteger = int64(9007199254740991)

// DecodeStrictJSONObject shares the existing duplicate-key, UTF-8, surrogate
// and safe-number checks with typed product adapters. It does not relax the
// caller's schema or canonical-byte contract.
func DecodeStrictJSONObject(payload []byte) (map[string]any, error) {
	return decodeStrictJSONObject(payload, "connector document")
}

// DecodeStrictLosslessJSONObject preserves arbitrary JSON numbers as
// json.Number while retaining duplicate-key and Unicode checks. Native source
// exports can contain integers outside the connector canonical-safe profile;
// callers must hash their exact raw bytes rather than canonicalizing them.
func DecodeStrictLosslessJSONObject(payload []byte) (map[string]any, error) {
	if !utf8.Valid(payload) {
		return nil, errors.New("native document must be valid UTF-8")
	}
	if err := validateJSONUnicodeEscapes(payload, "native document"); err != nil {
		return nil, err
	}
	if err := scanStrictJSON(payload, "native document"); err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()
	var object map[string]any
	if err := decoder.Decode(&object); err != nil || object == nil {
		return nil, errors.New("native document must be an object")
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("native document contains trailing content")
	}
	return object, nil
}

func decodeStrictJSONObject(payload []byte, label string) (map[string]any, error) {
	if !utf8.Valid(payload) {
		return nil, fmt.Errorf("%s must be valid UTF-8", label)
	}
	if err := validateJSONUnicodeEscapes(payload, label); err != nil {
		return nil, err
	}
	if err := scanStrictJSON(payload, label); err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()
	var object map[string]any
	if err := decoder.Decode(&object); err != nil {
		return nil, fmt.Errorf("decode %s: %w", label, err)
	}
	if object == nil {
		return nil, fmt.Errorf("%s must be an object", label)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("%s contains trailing content", label)
	}
	if _, err := canonicalJSON(object); err != nil {
		return nil, fmt.Errorf("%s violates the canonical safe-integer profile: %w", label, err)
	}
	return object, nil
}

func scanStrictJSON(payload []byte, label string) error {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()
	if err := scanStrictJSONValue(decoder, "$", label); err != nil {
		return err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return fmt.Errorf("%s contains trailing content", label)
	}
	return nil
}

func scanStrictJSONValue(decoder *json.Decoder, path, label string) error {
	token, err := decoder.Token()
	if err != nil {
		return fmt.Errorf("decode %s: %w", label, err)
	}
	delim, composite := token.(json.Delim)
	if !composite {
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]struct{}{}
		for decoder.More() {
			keyToken, keyErr := decoder.Token()
			if keyErr != nil {
				return fmt.Errorf("decode %s: %w", label, keyErr)
			}
			key, ok := keyToken.(string)
			if !ok {
				return fmt.Errorf("%s object key at %s is not a string", label, path)
			}
			if _, duplicate := seen[key]; duplicate {
				return fmt.Errorf("%s contains duplicate field at %s.%s", label, path, key)
			}
			seen[key] = struct{}{}
			if err := scanStrictJSONValue(decoder, path+"."+key, label); err != nil {
				return err
			}
		}
		if _, err := decoder.Token(); err != nil {
			return fmt.Errorf("decode %s: %w", label, err)
		}
	case '[':
		for index := 0; decoder.More(); index++ {
			if err := scanStrictJSONValue(
				decoder,
				path+"["+strconv.Itoa(index)+"]",
				label,
			); err != nil {
				return err
			}
		}
		if _, err := decoder.Token(); err != nil {
			return fmt.Errorf("decode %s: %w", label, err)
		}
	default:
		return fmt.Errorf("%s contains an invalid JSON delimiter", label)
	}
	return nil
}

func validateJSONUnicodeEscapes(payload []byte, label string) error {
	inString := false
	for index := 0; index < len(payload); index++ {
		switch payload[index] {
		case '"':
			inString = !inString
		case '\\':
			if !inString {
				continue
			}
			if index+1 >= len(payload) {
				return fmt.Errorf("%s contains an incomplete JSON escape", label)
			}
			if payload[index+1] != 'u' {
				index++
				continue
			}
			code, ok := decodeHexQuad(payload, index+2)
			if !ok {
				return fmt.Errorf("%s contains an invalid Unicode escape", label)
			}
			switch {
			case code >= 0xd800 && code <= 0xdbff:
				if index+11 >= len(payload) ||
					payload[index+6] != '\\' ||
					payload[index+7] != 'u' {
					return fmt.Errorf("%s contains an unpaired UTF-16 high surrogate", label)
				}
				low, lowOK := decodeHexQuad(payload, index+8)
				if !lowOK || low < 0xdc00 || low > 0xdfff {
					return fmt.Errorf("%s contains an unpaired UTF-16 high surrogate", label)
				}
				index += 11
			case code >= 0xdc00 && code <= 0xdfff:
				return fmt.Errorf("%s contains an unpaired UTF-16 low surrogate", label)
			default:
				index += 5
			}
		}
	}
	return nil
}

func decodeHexQuad(payload []byte, offset int) (uint16, bool) {
	if offset+4 > len(payload) {
		return 0, false
	}
	var value uint16
	for _, raw := range payload[offset : offset+4] {
		value <<= 4
		switch {
		case raw >= '0' && raw <= '9':
			value += uint16(raw - '0')
		case raw >= 'a' && raw <= 'f':
			value += uint16(raw-'a') + 10
		case raw >= 'A' && raw <= 'F':
			value += uint16(raw-'A') + 10
		default:
			return 0, false
		}
	}
	return value, true
}

func canonicalJSON(value any) ([]byte, error) {
	var output bytes.Buffer
	if err := appendCanonicalJSON(&output, value); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

// CanonicalizeJSONValue exposes the protocol's bounded RFC 8785 profile to
// product-owned adapters without exporting its mutable implementation.
func CanonicalizeJSONValue(value any) ([]byte, error) {
	return canonicalJSON(cloneJSONValue(value))
}

func appendCanonicalJSON(output *bytes.Buffer, value any) error {
	switch typed := value.(type) {
	case nil:
		output.WriteString("null")
	case bool:
		if typed {
			output.WriteString("true")
		} else {
			output.WriteString("false")
		}
	case string:
		appendCanonicalJSONString(output, typed)
	case json.Number:
		canonical, err := canonicalSafeInteger(typed)
		if err != nil {
			return err
		}
		output.WriteString(canonical)
	case int:
		if int64(typed) > maxSafeJSONInteger || int64(typed) < -maxSafeJSONInteger {
			return errors.New("JSON integer exceeds the safe-integer profile")
		}
		output.WriteString(strconv.Itoa(typed))
	case int64:
		if typed > maxSafeJSONInteger || typed < -maxSafeJSONInteger {
			return errors.New("JSON integer exceeds the safe-integer profile")
		}
		output.WriteString(strconv.FormatInt(typed, 10))
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Slice(keys, func(left, right int) bool {
			return compareUTF16(keys[left], keys[right]) < 0
		})
		output.WriteByte('{')
		for index, key := range keys {
			if index > 0 {
				output.WriteByte(',')
			}
			appendCanonicalJSONString(output, key)
			output.WriteByte(':')
			if err := appendCanonicalJSON(output, typed[key]); err != nil {
				return err
			}
		}
		output.WriteByte('}')
	case []any:
		output.WriteByte('[')
		for index, item := range typed {
			if index > 0 {
				output.WriteByte(',')
			}
			if err := appendCanonicalJSON(output, item); err != nil {
				return err
			}
		}
		output.WriteByte(']')
	default:
		return fmt.Errorf("unsupported canonical JSON type %T", value)
	}
	return nil
}

func canonicalSafeInteger(number json.Number) (string, error) {
	text := string(number)
	if text == "" || text == "-0" {
		return "", fmt.Errorf("JSON number %q is not a canonical integer", text)
	}
	start := 0
	if text[0] == '-' {
		if len(text) == 1 {
			return "", fmt.Errorf("JSON number %q is not a canonical integer", text)
		}
		start = 1
	}
	if text[start] == '0' && len(text)-start > 1 {
		return "", fmt.Errorf("JSON number %q is not a canonical integer", text)
	}
	for _, character := range text[start:] {
		if character < '0' || character > '9' {
			return "", fmt.Errorf("JSON number %q is not a canonical integer", text)
		}
	}
	parsed, err := strconv.ParseInt(text, 10, 64)
	if err != nil || parsed > maxSafeJSONInteger || parsed < -maxSafeJSONInteger {
		return "", fmt.Errorf("JSON integer %q exceeds the safe-integer profile", text)
	}
	return text, nil
}

func appendCanonicalJSONString(output *bytes.Buffer, value string) {
	output.WriteByte('"')
	for _, character := range value {
		switch character {
		case '"', '\\':
			output.WriteByte('\\')
			output.WriteRune(character)
		case '\b':
			output.WriteString(`\b`)
		case '\t':
			output.WriteString(`\t`)
		case '\n':
			output.WriteString(`\n`)
		case '\f':
			output.WriteString(`\f`)
		case '\r':
			output.WriteString(`\r`)
		default:
			if character < 0x20 {
				fmt.Fprintf(output, `\u%04x`, character)
			} else {
				output.WriteRune(character)
			}
		}
	}
	output.WriteByte('"')
}

func compareUTF16(left, right string) int {
	leftUnits := utf16.Encode([]rune(left))
	rightUnits := utf16.Encode([]rune(right))
	limit := len(leftUnits)
	if len(rightUnits) < limit {
		limit = len(rightUnits)
	}
	for index := 0; index < limit; index++ {
		switch {
		case leftUnits[index] < rightUnits[index]:
			return -1
		case leftUnits[index] > rightUnits[index]:
			return 1
		}
	}
	switch {
	case len(leftUnits) < len(rightUnits):
		return -1
	case len(leftUnits) > len(rightUnits):
		return 1
	default:
		return 0
	}
}

func cloneJSONValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		cloned := make(map[string]any, len(typed))
		for key, item := range typed {
			cloned[key] = cloneJSONValue(item)
		}
		return cloned
	case []any:
		cloned := make([]any, len(typed))
		for index, item := range typed {
			cloned[index] = cloneJSONValue(item)
		}
		return cloned
	default:
		return typed
	}
}
