package casepkg

import "time"

const MaxEvidenceBytes = 10 * 1024 * 1024

type Upload struct {
	File
	SHA256    string    `json:"sha256,omitempty"`
	Finalized bool      `json:"finalized"`
	ExpiresAt time.Time `json:"expires_at"`
	Data      []byte    `json:"-"`
}
