package connectorcontrol

import (
	"crypto/sha256"
	"encoding/hex"
)

// ManagedCollectionID preserves the original managed source identity preimage.
func ManagedCollectionID(installation, enrollment, project, kind string) string {
	d := sha256.Sum256([]byte("xinsight.managed-source-collection.v1\x00" + installation + "\x00" + enrollment + "\x00" + project + "\x00" + kind))
	d[6] = (d[6] & 15) | 0x50
	d[8] = (d[8] & 63) | 0x80
	v := hex.EncodeToString(d[:16])
	return v[:8] + "-" + v[8:12] + "-" + v[12:16] + "-" + v[16:20] + "-" + v[20:]
}
