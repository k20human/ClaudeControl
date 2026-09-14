package config

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// LayoutFingerprint identifies the arrangement a configuration file asks for.
//
// It exists so that editing the file can stop meaning "throw away the
// arrangement I left". The saved arrangement is kept unless the configured one
// has actually changed, and this is what "actually changed" is compared with —
// a question about the layout section rather than about the file's date, which
// answered yes to adding a service or a key that has nothing to do with panes.
//
// JSON rather than YAML, which the file is written in, because encoding/json
// sorts the keys of a map and the YAML encoder does not: options read into a
// map would otherwise fingerprint differently on each run and every start
// would look like an edit.
func LayoutFingerprint(spec *NodeSpec) string {
	if spec == nil {
		return ""
	}
	raw, err := json.Marshal(spec)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:8])
}
