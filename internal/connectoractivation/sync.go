package connectoractivation

import (
	"context"
	"github.com/foigent/x-insight/internal/connectorcontrol"
	"github.com/foigent/x-insight/internal/connectorsync"
)

type SyncStore interface {
	ExecuteConnectorSync(context.Context, connectorsync.Request) (connectorsync.Status, error)
}
type SyncHost interface{ AuthorizeSetupActor(string, bool) error }
type SyncManager struct {
	Store SyncStore
	Host  SyncHost
	Wake  func()
}

func (m *SyncManager) ExecuteSync(ctx context.Context, r connectorsync.Request) (connectorsync.Response, error) {
	out := connectorsync.Response{SchemaVersion: connectorsync.LocalSchema}
	if m == nil || m.Store == nil || m.Host == nil || !r.Valid() {
		return out, connectorsync.ErrInvalid
	}
	if e := m.Host.AuthorizeSetupActor(r.Actor, false); e != nil {
		return out, connectorcontrol.ErrSetupUnauthorized
	}
	status, e := m.Store.ExecuteConnectorSync(ctx, r)
	if e != nil {
		return out, e
	}
	out.Status = &status
	if m.Wake != nil && (r.Action == "once" || r.Action == "set" && r.Auto) {
		m.Wake()
	}
	return out, nil
}
func (s *UnixServer) SetSyncController(c connectorsync.Controller) error {
	if c == nil || s.syncController != nil {
		return connectorsync.ErrInvalid
	}
	s.syncController = c
	return nil
}
