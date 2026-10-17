package openinference

import "errors"

const (
	SharedProtocolCompatibilityStatus = "protocol_v2_product_pin_adopted_bounded_offline_authorization_required"
	AuthorityManifestProjectionSHA256 = "sha256:d5abcabffeb8bb368e5c188b7ed5179b5674092e9421af2c38e548bfdbec7077"
	AuthorityReleaseProjectionSHA256  = "sha256:d5abcabffeb8bb368e5c188b7ed5179b5674092e9421af2c38e548bfdbec7077"
)

var ErrSharedProtocolActivationBlocked = errors.New(
	"OpenInference general runtime activation is blocked: only an exact root-owned expiring non-production bounded-offline authorization may activate one import",
)

// ValidateSharedProtocolActivation remains the general runtime gate, not a
// product pin check. It deliberately does not authorize listeners, feeds or
// production bootstrap; the product-local bounded-offline importer has its own
// exact, expiring one-operation authorization boundary.
func ValidateSharedProtocolActivation() error {
	return ErrSharedProtocolActivationBlocked
}
