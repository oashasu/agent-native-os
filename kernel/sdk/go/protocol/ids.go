package protocol

import (
	"crypto/rand"
	"encoding/hex"
)

func NewID(prefix string) string {
	var b [12]byte
	_, _ = rand.Read(b[:])
	return prefix + "-" + hex.EncodeToString(b[:])
}
