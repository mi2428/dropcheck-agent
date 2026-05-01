package control

import (
	"crypto/rand"
	"encoding/hex"
)

// RandomHex returns byteCount cryptographically random bytes encoded as
// lowercase hexadecimal.
func RandomHex(byteCount int) (string, error) {
	buf := make([]byte, byteCount)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
