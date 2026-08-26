// Package uuid produces the identifiers Claude Code accepts for --session-id.
//
// Fifteen lines rather than a dependency: the only thing needed here is a
// version 4 UUID, and pulling a module in for that would be a poor trade.
package uuid

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// New returns a random version 4 UUID in canonical lower-case form.
func New() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("uuid: %w", err)
	}
	// Version 4 in the high nibble of byte 6, variant 1 in the top bits of
	// byte 8. Without them the value is random but not a valid v4 UUID.
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80

	h := make([]byte, 32)
	hex.Encode(h, b[:])
	return fmt.Sprintf("%s-%s-%s-%s-%s", h[0:8], h[8:12], h[12:16], h[16:20], h[20:32]), nil
}
