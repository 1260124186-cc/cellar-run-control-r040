package domain

import "time"

// ReceiptState tracks where an issued command stands. Only completed commands
// become business facts; a command number that never completed is never locked.
type ReceiptState string

const (
	// ReceiptProcessing means a command record exists without a result. Under
	// the single-transaction design a processing receipt is never committed,
	// so this state only appears after an interrupted process is restarted.
	ReceiptProcessing ReceiptState = "processing"
	// ReceiptSucceeded means the business fact and the captured response were
	// committed together. Replays return the captured response untouched.
	ReceiptSucceeded ReceiptState = "succeeded"
	// ReceiptFailed means a previous attempt produced no business fact (for
	// example after a crash recovery). The command number is free to retry.
	ReceiptFailed ReceiptState = "failed"
)

// Receipt is the independent command acknowledgment. It is keyed by the issuing
// terminal and its locally generated command number, so retries of the same
// physical button press resolve to exactly one business fact.
type Receipt struct {
	// ID is the storage key, derived from terminal ID and command number.
	ID string `json:"id"`
	// TerminalID identifies the operator terminal that issued the command.
	TerminalID string `json:"terminal_id"`
	// CommandNumber is unique per terminal; a resubmission reuses it.
	CommandNumber string `json:"command_number"`
	// Operation is the stable action name, for example "run.create".
	Operation string `json:"operation"`
	// TargetID is the aggregate addressed by path (formula/run/vessel ID), or
	// empty for collection creation routes.
	TargetID string `json:"target_id,omitempty"`
	// WorkOrderID is the optional cellar work order the command belongs to.
	WorkOrderID string `json:"work_order_id,omitempty"`
	// Fingerprint is the SHA-256 hex digest of the canonical request payload,
	// including the business parameters. A different fingerprint on the same
	// command number means the number was stolen by different parameters.
	Fingerprint string `json:"fingerprint"`
	// State is processing, succeeded, or failed.
	State ReceiptState `json:"state"`
	// StatusCode is the HTTP status captured on the first success.
	StatusCode int `json:"status_code,omitempty"`
	// ResponseBody is the exact JSON body returned by the first success,
	// replayed byte-for-byte on every retry.
	ResponseBody []byte `json:"response_body,omitempty"`
	// Attempts counts how many times the command number was presented.
	Attempts int `json:"attempts"`
	// LastErrorCode records why a recovered attempt failed; it never blocks.
	LastErrorCode string    `json:"last_error_code,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
	// CompletedAt is set when the receipt reaches a terminal state.
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}

// Completed reports whether the receipt holds a replayable successful result.
func (r Receipt) Completed() bool {
	return r.State == ReceiptSucceeded && r.ResponseBody != nil
}

// Retriable reports whether a new attempt may take over this command number.
func (r Receipt) Retriable() bool {
	return r.State == ReceiptFailed
}
