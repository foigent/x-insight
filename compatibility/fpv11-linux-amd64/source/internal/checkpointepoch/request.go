package checkpointepoch

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"unicode/utf8"
)

const PrepareRequestSchema = "xinsight.connector-checkpoint-epoch-prepare.v1"

type PrepareRequest struct {
	SchemaVersion      string  `json:"schema_version"`
	Binding            Binding `json:"binding"`
	WorkerUnit         string  `json:"worker_unit"`
	NextSourceInstance string  `json:"next_source_instance"`
	NextCursor         string  `json:"next_cursor"`
	NextHighWatermark  string  `json:"next_high_watermark"`
	AuthorizationRef   string  `json:"authorization_ref"`
}

func DecodePrepareRequest(payload []byte) (PrepareInput, error) {
	if !utf8.Valid(payload) {
		return PrepareInput{}, fmt.Errorf("%w: prepare request must be valid UTF-8", ErrInvalid)
	}
	if err := rejectDuplicateJSONFields(payload); err != nil {
		return PrepareInput{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var request PrepareRequest
	if err := decoder.Decode(&request); err != nil {
		return PrepareInput{}, fmt.Errorf("%w: decode prepare request", ErrInvalid)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return PrepareInput{}, fmt.Errorf("%w: prepare request contains trailing content", ErrInvalid)
	}
	if request.SchemaVersion != PrepareRequestSchema {
		return PrepareInput{}, fmt.Errorf("%w: prepare request schema_version", ErrInvalid)
	}
	input := PrepareInput{
		Binding:            request.Binding,
		WorkerUnit:         request.WorkerUnit,
		NextSourceInstance: request.NextSourceInstance,
		NextCursor:         request.NextCursor,
		NextHighWatermark:  request.NextHighWatermark,
		AuthorizationRef:   request.AuthorizationRef,
	}
	if err := validatePrepareInput(input); err != nil {
		return PrepareInput{}, err
	}
	return input, nil
}

func rejectDuplicateJSONFields(payload []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	if err := scanJSONValue(decoder, "$", make(map[string]struct{})); err != nil {
		return err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return fmt.Errorf("%w: prepare request contains trailing content", ErrInvalid)
	}
	return nil
}

func scanJSONValue(decoder *json.Decoder, path string, seen map[string]struct{}) error {
	token, err := decoder.Token()
	if err != nil {
		return fmt.Errorf("%w: decode prepare request", ErrInvalid)
	}
	delimiter, composite := token.(json.Delim)
	if !composite {
		return nil
	}
	switch delimiter {
	case '{':
		for key := range seen {
			delete(seen, key)
		}
		for decoder.More() {
			keyToken, keyErr := decoder.Token()
			if keyErr != nil {
				return fmt.Errorf("%w: decode prepare request", ErrInvalid)
			}
			key, ok := keyToken.(string)
			if !ok {
				return fmt.Errorf("%w: object key at %s", ErrInvalid, path)
			}
			if _, duplicate := seen[key]; duplicate {
				return fmt.Errorf("%w: duplicate field at %s.%s", ErrInvalid, path, key)
			}
			seen[key] = struct{}{}
			if err := scanJSONValue(decoder, path+"."+key, make(map[string]struct{})); err != nil {
				return err
			}
		}
		if _, err := decoder.Token(); err != nil {
			return fmt.Errorf("%w: decode prepare request", ErrInvalid)
		}
	case '[':
		for index := 0; decoder.More(); index++ {
			if err := scanJSONValue(decoder, path+"["+strconv.Itoa(index)+"]", make(map[string]struct{})); err != nil {
				return err
			}
		}
		if _, err := decoder.Token(); err != nil {
			return fmt.Errorf("%w: decode prepare request", ErrInvalid)
		}
	default:
		return fmt.Errorf("%w: invalid JSON delimiter", ErrInvalid)
	}
	return nil
}
