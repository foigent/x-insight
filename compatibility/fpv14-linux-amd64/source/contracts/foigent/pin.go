package foigentcontract

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"regexp"
)

const (
	SourceNeutralProtocolVersion       = "2.0.2"
	SourceNeutralProtocolTag           = "v2.0.2"
	SourceNeutralProtocolReleaseDigest = "sha256:99f8a541b6d33b0a74d2e4c06aa312c6d6c88c35a4bfb1d4d34b98b4e4e2e8e8"
)

var (
	//go:embed event-connector-protocol.pin.json
	embeddedProtocolPin []byte
	protocolDigest      = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
)

type ProtocolPin struct {
	PinVersion      string `json:"pin_version"`
	Product         string `json:"product"`
	ProtocolVersion string `json:"protocol_version"`
	ReleaseDigest   string `json:"release_digest"`
	Authority       struct {
		Tag string `json:"tag"`
	} `json:"authority"`
}

// EmbeddedProtocolPin returns the exact immutable product pin compiled into
// the release binary. It intentionally embeds only product-owned pin metadata,
// never an editable copy of the shared protocol schemas or fixtures.
func EmbeddedProtocolPin() (ProtocolPin, error) {
	var pin ProtocolPin
	if err := json.Unmarshal(embeddedProtocolPin, &pin); err != nil {
		return ProtocolPin{}, fmt.Errorf("decode embedded protocol pin")
	}
	if pin.PinVersion != "foigent.protocol-pin.v2" || pin.Product != "xinsight" ||
		pin.ProtocolVersion != SourceNeutralProtocolVersion ||
		pin.Authority.Tag != SourceNeutralProtocolTag ||
		pin.ReleaseDigest != SourceNeutralProtocolReleaseDigest ||
		!protocolDigest.MatchString(pin.ReleaseDigest) {
		return ProtocolPin{}, fmt.Errorf("embedded protocol pin identity is invalid")
	}
	return pin, nil
}
