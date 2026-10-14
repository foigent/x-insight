package connectorcontrol

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"
)

func (client *UnixController) AcknowledgeManaged(ctx context.Context, identity string, result ControlResultV3) (Installation, error) {
	var out Installation
	if client == nil || client.socketPath != LocalControllerSocket || client.dial == nil || client.timeout <= 0 || ValidateControlResultV3(result, time.Now().UTC()) != nil {
		return out, fmt.Errorf("managed ACK client preflight: %w", ErrInvalid)
	}
	raw, e := json.Marshal(ManagedACKRequest{SchemaVersion: LocalManagedACKSchema, ServiceIdentityID: identity, Result: result})
	if e != nil || len(raw) > maximumControllerMessageBytes-1 {
		return out, fmt.Errorf("managed ACK request encoding: %w", ErrInvalid)
	}
	connection, e := client.dial(ctx)
	if e != nil {
		return out, fmt.Errorf("managed controller unavailable")
	}
	defer connection.Close()
	deadline := time.Now().Add(client.timeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	if e = connection.SetDeadline(deadline); e != nil {
		return out, e
	}
	if _, e = connection.Write(append(raw, '\n')); e != nil {
		return out, fmt.Errorf("managed controller request failed")
	}
	raw, e = bufio.NewReader(io.LimitReader(connection, maximumControllerMessageBytes+1)).ReadBytes('\n')
	if e != nil || len(raw) > maximumControllerMessageBytes {
		return out, fmt.Errorf("managed controller response failed")
	}
	var response ManagedACKResponse
	if decodeStrictControlJSON(bytes.TrimSuffix(raw, []byte{'\n'}), &response) != nil || response.SchemaVersion != LocalManagedACKSchema {
		return out, fmt.Errorf("managed ACK response decoding: %w", ErrInvalid)
	}
	if response.Installation != nil {
		installation := *response.Installation
		installation.ControlServiceIdentityID = response.ControlServiceIdentityID
		installation.SourceInstance = response.SourceInstance
		if response.Error != "" || installation.InstallationID != result.InstallationID || installation.ControlServiceIdentityID != identity || ValidateInstallation(installation) != nil {
			return out, fmt.Errorf("managed ACK response installation: %w", ErrInvalid)
		}
		return installation, nil
	}
	if response.ControlServiceIdentityID != "" || response.SourceInstance != "" {
		return out, fmt.Errorf("managed ACK error receipt: %w", ErrInvalid)
	}
	switch response.Error {
	case "invalid":
		return out, fmt.Errorf("managed ACK local rejection: %w", ErrInvalid)
	case "not_found":
		return out, ErrNotFound
	case "conflict":
		return out, ErrConflict
	default:
		return out, fmt.Errorf("managed controller unavailable")
	}
}
