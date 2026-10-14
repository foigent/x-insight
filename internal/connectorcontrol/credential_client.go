package connectorcontrol

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"syscall"
	"time"
)

func credentialClientFailureCategory(err error) string {
	category := "internal"
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		category = "context_deadline"
	case errors.Is(err, context.Canceled):
		category = "context_cancelled"
	case errors.Is(err, os.ErrNotExist):
		category = "socket_missing"
	case errors.Is(err, syscall.ECONNREFUSED):
		category = "connection_refused"
	case errors.Is(err, syscall.ECONNRESET), errors.Is(err, syscall.EPIPE), errors.Is(err, io.EOF):
		category = "connection_closed"
	}
	return category
}

func logCredentialClientFailure(action, phase string, err error) {
	log.Printf("connector credential client action=%s phase=%s category=%s", action, phase, credentialClientFailureCategory(err))
}

func (c *UnixController) ExecuteCredential(ctx context.Context, r CredentialLocalRequest) (CredentialLocalResponse, error) {
	var out CredentialLocalResponse
	if c == nil || c.socketPath != LocalControllerSocket || c.timeout <= 0 || c.dial == nil || !r.Valid() {
		return out, ErrInvalid
	}
	raw, err := json.Marshal(r)
	if err != nil || len(raw) > maximumControllerMessageBytes-1 {
		return out, ErrInvalid
	}
	conn, err := c.dial(ctx)
	if err != nil {
		logCredentialClientFailure(r.Action, "dial", err)
		return out, fmt.Errorf("credential controller unavailable")
	}
	defer conn.Close()
	deadline := time.Now().Add(c.timeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	if err = conn.SetDeadline(deadline); err != nil {
		logCredentialClientFailure(r.Action, "deadline", err)
		return out, err
	}
	if _, err = conn.Write(append(raw, '\n')); err != nil {
		logCredentialClientFailure(r.Action, "write", err)
		return out, fmt.Errorf("credential request failed")
	}
	raw, err = bufio.NewReader(io.LimitReader(conn, maximumControllerMessageBytes+1)).ReadBytes('\n')
	if err != nil || len(raw) > maximumControllerMessageBytes {
		if err != nil {
			logCredentialClientFailure(r.Action, "read", err)
		} else {
			log.Printf("connector credential client action=%s phase=read category=response_oversized", r.Action)
		}
		return out, fmt.Errorf("credential response unavailable")
	}
	if decodeStrictControlJSON(bytes.TrimSuffix(raw, []byte{'\n'}), &out) != nil || !ValidCredentialLocalResponse(r, out) {
		log.Printf("connector credential client action=%s phase=decode category=invalid_response", r.Action)
		return CredentialLocalResponse{}, ErrInvalid
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
		err = errors.New("credential controller unavailable")
	}
	return CredentialLocalResponse{}, err
}
