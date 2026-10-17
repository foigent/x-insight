package connectorcontrol

import (
	"net/netip"
	"regexp"
	"strconv"
)

const SourceDescriptorSchema = "unirouter.xinsight.connector-source-descriptor.v1"
const SourceBindingSchema = "unirouter.xinsight.connector-source-scope.v1"
const SourceBindingFeedSchema = "xinsight.completed-execution-feed.v1"

// Field order is the frozen UR consent preimage, not the XI control project.
type SourceBindingV1 struct {
	SchemaVersion  string `json:"schema_version"`
	InstallationID string `json:"installation_id"`
	DeploymentID   string `json:"x_deployment_id"`
	OrgID          int64  `json:"org_id"`
	ProjectID      int64  `json:"project_id"`
	SourceInstance string `json:"source_instance"`
	EventScope     string `json:"event_scope"`
	FeedSchema     string `json:"feed_schema"`
}
type ReceiverNetworkV1 struct {
	FeedKeyID               string `json:"feed_key_id"`
	KeyPrefix               string `json:"key_prefix"`
	RegistrationOperationID string `json:"registration_operation_id"`
	VerifierScheme          string `json:"verifier_scheme"`
	AllowedIP               string `json:"allowed_ip"`
}
type SourceDescriptorV1 struct {
	SchemaVersion         string            `json:"schema_version"`
	SourceBinding         SourceBindingV1   `json:"source_binding"`
	SourceBindingSHA256   string            `json:"source_binding_sha256"`
	ReceiverNetwork       ReceiverNetworkV1 `json:"receiver_network"`
	ReceiverNetworkSHA256 string            `json:"receiver_network_sha256"`
	ResumeOperationID     string            `json:"resume_operation_id"`
	ResumeResultSHA256    string            `json:"resume_result_sha256"`
}

func ValidateSourceDescriptor(v SourceDescriptorV1) error {
	s, n := v.SourceBinding, v.ReceiverNetwork
	ip, e := netip.ParsePrefix(n.AllowedIP)
	key, keyErr := strconv.ParseInt(n.FeedKeyID, 10, 64)
	if v.SchemaVersion != SourceDescriptorSchema || s.SchemaVersion != SourceBindingSchema || !IsInstallationID(s.InstallationID) || !regexp.MustCompile(`^xid_[A-Za-z0-9_-]{22}$`).MatchString(s.DeploymentID) || s.OrgID < 1 || s.ProjectID < 1 || !v3Source(s.SourceInstance) || s.EventScope != CompletedEventType || s.FeedSchema != SourceBindingFeedSchema || !MatchesCanonicalDigest(s, v.SourceBindingSHA256) || keyErr != nil || key < 1 || strconv.FormatInt(key, 10) != n.FeedKeyID || !regexp.MustCompile(`^ur-xf1-[a-f0-9]{13}$`).MatchString(n.KeyPrefix) || !receiverOperationPattern.MatchString(n.RegistrationOperationID) || n.VerifierScheme != "receiver_sha256_v1" || e != nil || ip.String() != n.AllowedIP || ip.Bits() != ip.Addr().BitLen() || !MatchesCanonicalDigest(n, v.ReceiverNetworkSHA256) || !operationIDPattern.MatchString(v.ResumeOperationID) || !hashPattern.MatchString(v.ResumeResultSHA256) {
		return ErrInvalid
	}
	return nil
}
func DecodeSourceDescriptor(raw []byte) (SourceDescriptorV1, error) {
	var v SourceDescriptorV1
	if len(raw) > 16<<10 || decodeV3(raw, &v) != nil || ValidateSourceDescriptor(v) != nil {
		return SourceDescriptorV1{}, ErrInvalid
	}
	return v, nil
}

// VerifiedSourceContext is local durable identity readback. It asserts neither
// a machine grant nor an adoption; Bootstrap remains exclusive to adoption.
type VerifiedSourceContext struct {
	SourceInstance   string
	OrgID, ProjectID int64
	AllowedIP        string
}

func (b ManagedBoundary) VerifiedSource() (VerifiedSourceContext, bool) {
	if b.Bootstrap != nil {
		v := b.Bootstrap
		return VerifiedSourceContext{v.SourceInstance, v.OrgID, v.ProjectID, v.AllowedIP}, true
	}
	if b.Descriptor != nil {
		v := b.Descriptor
		return VerifiedSourceContext{v.SourceBinding.SourceInstance, v.SourceBinding.OrgID, v.SourceBinding.ProjectID, v.ReceiverNetwork.AllowedIP}, true
	}
	return VerifiedSourceContext{}, false
}

// OriginalReceiverProof qualifies the accepted v2 chain without relying on a
// descriptor or a feed item (the original feed may legitimately be empty).
func OriginalReceiverProof(b ManagedBoundary) (PublicReceiverRegistrationV1, ExistingWorkerConfig, error) {
	var empty PublicReceiverRegistrationV1
	var worker ExistingWorkerConfig
	if b.Bootstrap != nil || b.Installation.Status != "connected" || b.Installation.DesiredState != "active" || b.Installation.ObservedState != "connected" || b.Installation.InstallationAcknowledgementSHA256 == "" || !IsSHA256Reference(b.SourceBindingSHA256) {
		return empty, worker, ErrConflict
	}
	prepare, register, resume := b.Phase(ControlPhasePreparePausedSource, ""), b.Phase(ControlPhaseRegisterReceiver, ""), b.Phase(ControlPhaseResumeSource, "")
	for _, p := range []*ActivationPhase{prepare, register, resume} {
		if p == nil || p.Result == nil || p.Operation.Status != "completed" || p.Operation.SchemaVersion != ControlOperationSchemaV2 || ValidateControlOperationV2(p.Operation) != nil || ValidateControlResultV2ForOperation(p.Operation, *p.Result) != nil || p.Result.Status != "completed" || !MatchesCanonicalDigest(*p.Result, p.Operation.ResultSHA256) || p.Result.Result == nil || p.Result.Result.SourceInstance != b.Installation.SourceInstance || p.Operation.InstallationID != b.Installation.InstallationID || p.Operation.ProjectID != b.Installation.ProjectID {
			return empty, worker, ErrConflict
		}
	}
	if register.Operation.PredecessorOperationID != prepare.Operation.OperationID || register.Operation.PredecessorResultSHA256 != prepare.Operation.ResultSHA256 || resume.Operation.PredecessorOperationID != register.Operation.OperationID || resume.Operation.PredecessorResultSHA256 != register.Operation.ResultSHA256 {
		return empty, worker, ErrConflict
	}
	for _, pair := range [][2]*ActivationPhase{{register, prepare}, {resume, register}} {
		request, _, _, err := CanonicalControlOperationRequestV2(pair[0].Operation)
		if err != nil || ValidateControlOperationV2Predecessor(request, *pair[1].Result) != nil {
			return empty, worker, ErrConflict
		}
	}
	r := *register.Operation.Request.ReceiverRegistration
	h, _, _ := CanonicalSHA256(r)
	if resume.Operation.Request.ReceiverRegistrationSHA256 != h || resume.Operation.Request.NetworkBindingSHA256 != register.Operation.Request.NetworkBinding.BindingSHA256 {
		return empty, worker, ErrConflict
	}
	worker = ExistingWorkerConfig{WorkerUnit: "xinsight-feed.service", UROrigin: b.UniRouterOrigin, FeedRoute: ReceiverFeedRoute, SourceInstance: b.Installation.SourceInstance, FeedSchema: ControlFeedSchema, CursorKey: ControlCursorKey, ReceiverRegistrationSHA256: h}
	return r, worker, nil
}
func ValidateSourceDescriptorProof(b ManagedBoundary, v SourceDescriptorV1) error {
	r, _, e := OriginalReceiverProof(b)
	if e != nil || ValidateSourceDescriptor(v) != nil {
		return ErrConflict
	}
	s, n := v.SourceBinding, v.ReceiverNetwork
	resume := b.Phase(ControlPhaseResumeSource, "")
	if s.InstallationID != b.Installation.InstallationID || s.DeploymentID != b.Installation.DeploymentID || s.SourceInstance != b.Installation.SourceInstance || v.SourceBindingSHA256 != b.SourceBindingSHA256 || v.ResumeOperationID != resume.Operation.OperationID || v.ResumeResultSHA256 != resume.Operation.ResultSHA256 || n.KeyPrefix != r.KeyPrefix || n.RegistrationOperationID != r.OperationID || n.VerifierScheme != r.VerifierScheme {
		return ErrConflict
	}
	return nil
}
