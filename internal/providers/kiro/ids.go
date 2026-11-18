package kiro

import (
	"crypto/rand"
	"encoding/hex"
)

func newToolID() string {
	var buf [8]byte
	_, _ = rand.Read(buf[:])
	return "toolu_" + hex.EncodeToString(buf[:])
}
