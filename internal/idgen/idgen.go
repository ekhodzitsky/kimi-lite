// Package idgen provides a shared ID generation utility.
package idgen

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
)

// randRead is the source of randomness used by the package. It is a variable
// so tests can replace it to exercise error paths. A mutex protects it so
// concurrent callers and tests remain race-free.
var (
	randReadMu sync.RWMutex
	randRead   = rand.Read
)

func getRandRead() func([]byte) (int, error) {
	randReadMu.RLock()
	defer randReadMu.RUnlock()
	return randRead
}

// GenerateID generates a random 32-character lowercase hex ID.
// It panics if the system's CSPRNG fails, which is effectively impossible
// on supported platforms.
//
// For error-sensitive callers, use GenerateIDError.
func GenerateID() string {
	id, err := GenerateIDError()
	if err != nil {
		panic("idgen: crypto/rand.Read failed: " + err.Error())
	}
	return id
}

// GenerateIDError generates a random 32-character lowercase hex ID and
// returns any error from the underlying CSPRNG instead of panicking.
func GenerateIDError() (string, error) {
	var b [16]byte
	if _, err := getRandRead()(b[:]); err != nil {
		return "", fmt.Errorf("crypto/rand.Read: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}
