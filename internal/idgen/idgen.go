// Package idgen generates opaque local identifiers (session IDs, job IDs,
// generated-app IDs) without pulling in an external UUID dependency.
package idgen

import (
	"crypto/rand"
	"encoding/hex"
)

// New returns a random 16-byte hex-encoded identifier.
func New() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err) // crypto/rand failure means the OS RNG is broken; nothing to recover to
	}
	return hex.EncodeToString(b)
}
