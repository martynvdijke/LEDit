package handlers

import (
	"crypto/rand"
	"encoding/hex"
)

// generateDeviceToken returns a 32-character hex token used to identify and
// authenticate a device when it connects over WebSocket.
func generateDeviceToken() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}
