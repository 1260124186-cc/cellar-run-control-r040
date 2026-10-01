package domain

import "time"

// InstructionReceipt is the durable record of a write instruction that the
// service has already applied exactly once. Re-submitting the same
// instruction key replays ResponseJSON verbatim; submitting it with a
// different request fingerprint means the key is already occupied.
type InstructionReceipt struct {
	Key          string    `json:"key"`
	TerminalID   string    `json:"terminal_id,omitempty"`
	Method       string    `json:"method"`
	Path         string    `json:"path"`
	Fingerprint  string    `json:"fingerprint"`
	Status       int       `json:"status"`
	ResponseJSON []byte    `json:"response_json"`
	CreatedAt    time.Time `json:"created_at"`
}

func (r InstructionReceipt) Clone() InstructionReceipt {
	cloned := r
	cloned.ResponseJSON = append([]byte(nil), r.ResponseJSON...)
	return cloned
}
