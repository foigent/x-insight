package workbench

const RuntimeActionWitnessesVersion = "xinsight.runtime.action-witnesses.v1"

type RuntimeActionWitnessItem struct {
	Record               ActionWitness               `json:"record"`
	RecordHash           string                      `json:"record_hash"`
	ChainState           string                      `json:"chain_state"`
	ChangedSinceReading  bool                        `json:"changed_since_reading"`
	TriggerRequestRef    string                      `json:"trigger_request_ref"`
	FinalRequestRef      string                      `json:"final_request_ref"`
	ResolvedContentLinks []ActionResolvedContentLink `json:"resolved_content_links"`
}

type ActionResolvedContentLink struct {
	Purpose    string `json:"purpose"`
	RequestRef string `json:"request_ref"`
}

type RuntimeActionWitnesses struct {
	Version       string                     `json:"action_witnesses_version"`
	ReadingID     string                     `json:"reading_id"`
	RequestRef    string                     `json:"request_ref"`
	MembershipRef string                     `json:"membership_ref"`
	Coverage      string                     `json:"coverage"`
	Items         []RuntimeActionWitnessItem `json:"items"`
	NextCursor    string                     `json:"next_cursor,omitempty"`
}
