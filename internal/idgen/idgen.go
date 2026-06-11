// Package idgen provides a shared ID generation utility.
package idgen

import (
	"crypto/rand"
	"encoding/hex"
)

// GenerateID generates a random 32-character lowercase hex ID.
// It panics if the system's CSPRNG fails, which is effectively impossible
// on supported platforms.
func GenerateID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic("idgen: crypto/rand.Read failed: " + err.Error())
	}
	return hex.EncodeToString(b)
}
