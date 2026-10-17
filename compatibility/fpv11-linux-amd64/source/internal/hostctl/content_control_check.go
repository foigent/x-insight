package hostctl

import "github.com/foigent/x-insight/internal/sourcefacts"

// CheckContentControl uses only caller-supplied public documents. It has no
// Engine, database, network, credential loader, or managed-state write path.
type ContentControlCheckInput struct {
	Registration  []byte
	ManagedConfig []byte
	CollectionID  string
	GET           []byte
	Capabilities  []byte
	Action        string
	Request       []byte
	Receipt       []byte
}
type ContentControlCheckResult struct {
	CheckOnly           bool   `json:"check_only"`
	Evidence            string `json:"evidence"`
	RegistrationHash    string `json:"registration_hash,omitempty"`
	RequestHash         string `json:"request_hash,omitempty"`
	StateVersion        string `json:"state_version,omitempty"`
	BindingCandidate    bool   `json:"binding_candidate"`
	CapabilitiesMatched bool   `json:"capabilities_matched"`
	ReceiptMatched      bool   `json:"receipt_matched"`
}

func CheckContentControl(in ContentControlCheckInput) (ContentControlCheckResult, error) {
	out := ContentControlCheckResult{CheckOnly: true, Evidence: "supplied_public_snapshots_only"}
	if len(in.GET) > 0 {
		if in.Action != "" || len(in.Request) > 0 || len(in.Receipt) > 0 {
			return out, sourcefacts.ErrInvalid
		}
		reg, err := sourcefacts.DecodeSourceCredentialRegistration(in.Registration)
		if err != nil {
			return out, err
		}
		cfg, err := sourcefacts.ParseManagedConfig(in.ManagedConfig)
		if err != nil {
			return out, err
		}
		for _, p := range cfg.Contents {
			if p.CollectionID != in.CollectionID {
				continue
			}
			p, err = cfg.ResolveContent(p)
			if err != nil {
				return out, err
			}
			candidate, err := sourcefacts.ValidateContentControlBinding(in.GET, reg, p)
			if err != nil {
				return out, err
			}
			_, out.RegistrationHash, _ = reg.MarshalRegistration()
			out.BindingCandidate, out.StateVersion = true, candidate.StateVersion()
			if len(in.Capabilities) > 0 {
				if _, err := candidate.ValidateCapabilities(in.Capabilities); err != nil {
					return out, err
				}
				out.CapabilitiesMatched = true
			}
			return out, nil
		}
		return out, sourcefacts.ErrAuthorization
	}
	if len(in.Registration) > 0 || len(in.ManagedConfig) > 0 || in.CollectionID != "" || len(in.Capabilities) > 0 {
		return out, sourcefacts.ErrInvalid
	}
	request, err := sourcefacts.DecodeContentControlRequest(in.Action, in.Request)
	if err != nil {
		return out, err
	}
	out.RequestHash = request.Hash()
	if len(in.Receipt) > 0 {
		receipt, err := request.MatchReceipt(in.Receipt)
		if err != nil {
			return out, err
		}
		out.StateVersion, out.ReceiptMatched = receipt.StateVersion, true
	}
	return out, nil
}
