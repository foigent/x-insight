package connectorcontrol

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/foigent/x-insight/internal/connectorsync"
	"io"
	"time"
)

func (c *UnixController) ExecuteSync(ctx context.Context, r connectorsync.Request) (connectorsync.Response, error) {
	var out connectorsync.Response
	if c == nil || c.socketPath != LocalControllerSocket || c.dial == nil || !r.Valid() || !controllerHumanSubjectPattern.MatchString(r.Actor) {
		return out, ErrInvalid
	}
	raw, e := json.Marshal(r)
	if e != nil || len(raw) > maximumControllerMessageBytes-1 {
		return out, ErrInvalid
	}
	conn, e := c.dial(ctx)
	if e != nil {
		return out, e
	}
	defer conn.Close()
	deadline := time.Now().Add(c.timeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	if e = conn.SetDeadline(deadline); e != nil {
		return out, e
	}
	if _, e = conn.Write(append(raw, '\n')); e != nil {
		return out, e
	}
	raw, e = bufio.NewReader(io.LimitReader(conn, maximumControllerMessageBytes+1)).ReadBytes('\n')
	if e != nil || len(raw) > maximumControllerMessageBytes {
		return out, ErrInvalid
	}
	if decodeStrictControlJSON(bytes.TrimSuffix(raw, []byte{'\n'}), &out) != nil || out.SchemaVersion != connectorsync.LocalSchema {
		return out, ErrInvalid
	}
	if out.Error != "" {
		switch out.Error {
		case "conflict":
			return out, connectorsync.ErrConflict
		case "unauthorized":
			return out, ErrSetupUnauthorized
		case "invalid":
			return out, ErrInvalid
		default:
			return out, errors.New("sync controller unavailable")
		}
	}
	if out.Status == nil || out.Status.InstallationID != r.InstallationID || out.Status.Revision < 1 {
		return connectorsync.Response{}, ErrInvalid
	}
	return out, nil
}
