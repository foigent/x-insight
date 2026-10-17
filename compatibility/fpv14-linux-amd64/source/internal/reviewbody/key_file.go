package reviewbody

import (
	"fmt"
	"strings"

	"github.com/foigent/x-insight/internal/config"
)

// LoadKeyRingFromFile uses the existing restricted-file loader. Absence is
// distinct from a configured but invalid key pair.
func LoadKeyRingFromFile(id, path string) (KeyRing, error) {
	id, path = strings.TrimSpace(id), strings.TrimSpace(path)
	if id == "" && path == "" {
		return KeyRing{}, nil
	}
	if id == "" || path == "" {
		return KeyRing{}, fmt.Errorf("review body key id and file must be configured together")
	}
	key, err := config.LoadReviewBodyKeyFile(path)
	if err != nil {
		return KeyRing{}, err
	}
	ring := KeyRing{ActiveID: id, Keys: map[string][]byte{id: key}}
	if err := ring.Validate(); err != nil {
		return KeyRing{}, err
	}
	return ring, nil
}
