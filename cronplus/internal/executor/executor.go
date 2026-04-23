package executor

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"time"
)

// DecodeCommand tries to base64-decode a command string.
// Falls back to the original string if it's not valid base64.
func DecodeCommand(cmd string) string {
	if cmd == "" {
		return cmd
	}
	decoded, err := base64.StdEncoding.DecodeString(cmd)
	if err != nil {
		return cmd
	}
	// Verify roundtrip
	if base64.StdEncoding.EncodeToString(decoded) == cmd {
		return string(decoded)
	}
	return cmd
}

// NewRunID generates a short human-readable run identifier.
// Format: YYMMDD-HHMMSS-xxxx (4 random hex chars)
func NewRunID() string {
	now := time.Now()
	b := make([]byte, 2)
	rand.Read(b)
	return fmt.Sprintf("%s-%s",
		now.Format("060102-150405"),
		fmt.Sprintf("%02x%02x", b[0], b[1]),
	)
}
