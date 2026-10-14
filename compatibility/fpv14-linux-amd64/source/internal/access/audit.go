package access

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"sync"
	"time"
)

type AuditEvent struct {
	Event         string     `json:"event"`
	Time          time.Time  `json:"time"`
	SubjectID     string     `json:"subject_id,omitempty"`
	Role          Role       `json:"role,omitempty"`
	Permission    Permission `json:"permission,omitempty"`
	Result        string     `json:"result"`
	CorrelationID string     `json:"correlation_id"`
}

type Auditor interface {
	Record(AuditEvent)
}

type WriterAuditor struct {
	mu      sync.Mutex
	encoder *json.Encoder
}

func NewWriterAuditor(writer io.Writer) *WriterAuditor {
	return &WriterAuditor{encoder: json.NewEncoder(writer)}
}

func (auditor *WriterAuditor) Record(event AuditEvent) {
	if auditor == nil || auditor.encoder == nil {
		return
	}
	event.Time = event.Time.UTC()
	auditor.mu.Lock()
	defer auditor.mu.Unlock()
	_ = auditor.encoder.Encode(event)
}

type DiscardAuditor struct{}

func (DiscardAuditor) Record(AuditEvent) {}

func audit(
	auditor Auditor,
	now time.Time,
	event string,
	principal *Principal,
	permission Permission,
	result string,
) string {
	correlationID := newAuditCorrelationID()
	entry := AuditEvent{
		Event:         event,
		Time:          now.UTC(),
		Permission:    permission,
		Result:        result,
		CorrelationID: correlationID,
	}
	if principal != nil {
		entry.SubjectID = principal.SubjectID
		entry.Role = principal.Role
	}
	if auditor != nil {
		auditor.Record(entry)
	}
	return correlationID
}

func newAuditCorrelationID() string {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "xa_unavailable"
	}
	return "xa_" + hex.EncodeToString(value[:])
}
