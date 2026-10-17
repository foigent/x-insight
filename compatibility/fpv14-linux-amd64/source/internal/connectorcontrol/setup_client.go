package connectorcontrol

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"
)

func (c *UnixController) ExecuteSetup(ctx context.Context, r SetupLocalRequest) (SetupLocalResponse, error) {
	var out SetupLocalResponse
	if c == nil || c.socketPath != LocalControllerSocket || c.timeout <= 0 || c.dial == nil || !r.Valid() {
		return out, ErrInvalid
	}
	raw, err := json.Marshal(r)
	if err != nil || len(raw) > maximumControllerMessageBytes-1 {
		return out, ErrInvalid
	}
	conn, err := c.dial(ctx)
	if err != nil {
		return out, fmt.Errorf("setup controller unavailable")
	}
	defer conn.Close()
	deadline := time.Now().Add(c.timeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	if err = conn.SetDeadline(deadline); err != nil {
		return out, err
	}
	if _, err = conn.Write(append(raw, '\n')); err != nil {
		return out, fmt.Errorf("setup request failed")
	}
	raw, err = bufio.NewReader(io.LimitReader(conn, maximumControllerMessageBytes+1)).ReadBytes('\n')
	if err != nil || len(raw) > maximumControllerMessageBytes {
		return out, fmt.Errorf("setup response unavailable")
	}
	if decodeStrictControlJSON(bytes.TrimSuffix(raw, []byte{'\n'}), &out) != nil || !ValidSetupResponse(r, out) {
		return SetupLocalResponse{}, ErrInvalid
	}
	switch out.Error {
	case "":
		return out, nil
	case "invalid":
		err = ErrInvalid
	case "not_found":
		err = ErrNotFound
	case "conflict":
		err = ErrConflict
	case "expired":
		err = ErrExpired
	case "unauthorized":
		err = ErrSetupUnauthorized
	default:
		err = errors.New("setup controller unavailable")
	}
	return SetupLocalResponse{}, err
}
