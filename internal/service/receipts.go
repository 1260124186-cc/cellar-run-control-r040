package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/1260124186-cc/solo-0016-cellar-run-control/internal/domain"
)

// Retention windows for persisted receipts. A retry after a weak-signal
// timeout always happens within minutes; the limits only bound snapshot
// growth. A completed command number older than its window may be reused by a
// new command, exactly as if the terminal had issued a fresh number.
const (
	succeededReceiptTTL = 7 * 24 * time.Hour
	failedReceiptTTL    = time.Hour
)

var commandTokenPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,63}$`)

// Command identifies one physical instruction issued by one terminal. The
// terminal generates CommandNumber locally and keeps resending the same value
// after a timeout; the server collapses every resend into one business fact.
type Command struct {
	TerminalID    string
	CommandNumber string
	// Operation is the stable action name, e.g. "formula.create".
	Operation string
	// TargetID is the path aggregate ID, empty on collection creation.
	TargetID string
	// WorkOrderID is the optional work order the command belongs to.
	WorkOrderID string
	// Fingerprint is the hex SHA-256 of the canonical request parameters.
	Fingerprint string
}

func (c Command) storageKey() string {
	return c.TerminalID + "#" + c.CommandNumber
}

// Validate checks that the terminal and command number are well formed.
func (c Command) Validate() error {
	if !commandTokenPattern.MatchString(c.TerminalID) {
		return domain.FieldError(domain.CodeCommandHeaderInvalid, "X-Terminal-ID",
			"must contain 1 to 64 letters, digits, dots, colons, underscores, or hyphens")
	}
	if !commandTokenPattern.MatchString(c.CommandNumber) {
		return domain.FieldError(domain.CodeCommandHeaderInvalid, "X-Command-Number",
			"must contain 1 to 64 letters, digits, dots, colons, underscores, or hyphens")
	}
	if c.Operation == "" || c.Fingerprint == "" {
		return domain.NewError(domain.CodeCommandHeaderInvalid,
			"the command is missing its operation or request fingerprint")
	}
	return nil
}

// FingerprintPayload returns the deterministic digest of the business
// parameters. The same terminal request always produces the same digest; any
// changed field changes it, which is how stolen command numbers are detected.
func FingerprintPayload(payload any, expectedVersion *int64) string {
	structured := struct {
		Payload         any    `json:"payload"`
		ExpectedVersion *int64 `json:"expected_version,omitempty"`
	}{Payload: payload, ExpectedVersion: expectedVersion}
	encoded, err := json.Marshal(structured)
	if err != nil {
		// The inputs are decoded JSON values and plain structs; a marshal
		// failure here indicates a programming error rather than client input.
		return fmt.Sprintf("marshal-error:%x", sha256.Sum256([]byte(fmt.Sprintf("%#v", structured))))
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}

// CommandResult is what the HTTP layer writes back. On a replay Body is the
// exact JSON captured from the first successful attempt.
type CommandResult struct {
	StatusCode int
	Body       []byte
	Replayed   bool
}

// receiptReplay aborts a write transaction without changing the snapshot when
// the receipt was already completed by a concurrent attempt. It carries the
// captured response straight back to the caller.
type receiptReplay struct {
	statusCode int
	body       []byte
}

func (e *receiptReplay) Error() string { return "command already completed" }

// runCommand executes a business mutation under optional command receipts.
//
// Without a command it behaves as a plain write. With a command it
// guarantees:
//
//   - identical resends (same terminal, number, operation, target, work
//     order, fingerprint) execute the business once and replay the captured
//     response;
//   - a reused number with different parameters fails with
//     command_reuse_conflict and creates no business fact;
//   - the receipt, the business change, and the captured response commit in a
//     single atomic snapshot transaction, so a crash can never leave a
//     half-issued command that blocks later retries.
func (s *Service) runCommand(
	cmd *Command,
	successStatus int,
	mutate func(*domain.Snapshot) (any, error),
) (CommandResult, error) {
	if cmd == nil {
		return s.runPlain(successStatus, mutate)
	}
	if err := cmd.Validate(); err != nil {
		return CommandResult{}, err
	}

	// Fast read-only path: a completed receipt can be replayed without
	// taking the write lock or bumping the snapshot revision.
	if replay, found, err := s.lookupReplay(cmd); err != nil {
		return CommandResult{}, err
	} else if found {
		return replay, nil
	}

	var captured []byte
	err := s.store.Execute(func(snapshot *domain.Snapshot) error {
		if existing, found := snapshot.Receipts[cmd.storageKey()]; found {
			switch {
			case existing.State == domain.ReceiptProcessing:
				// Only reachable for state produced outside this process;
				// startup recovery normally turns these into failures.
				return domain.NewError(domain.CodeCommandInProgress,
					"the command is still being processed by another attempt; resend it unchanged")
			case existing.State == domain.ReceiptSucceeded &&
				sameCommandScope(existing, *cmd):
				// Lost a race with another terminal request that completed
				// the command between the view and this transaction.
				return &receiptReplay{statusCode: existing.StatusCode, body: existing.ResponseBody}
			case existing.State == domain.ReceiptSucceeded:
				// A completed receipt is only replayable when the operation,
				// target, work order, and parameters all match. Anything else
				// means the number was stolen for a different instruction.
				return newReuseConflict(existing, *cmd)
			default:
				// A failed or recovered attempt never locks the number: it is
				// overwritten even when its old parameters differ.
			}
		}

		sweepExpiredReceipts(snapshot, s.clock.Now())

		value, err := mutate(snapshot)
		if err != nil {
			return err
		}
		encoded, err := json.Marshal(value)
		if err != nil {
			return domain.NewError(domain.CodeStorage,
				"capture command response: "+err.Error())
		}

		now := s.clock.Now()
		receipt := domain.Receipt{
			ID:            cmd.storageKey(),
			TerminalID:    cmd.TerminalID,
			CommandNumber: cmd.CommandNumber,
			Operation:     cmd.Operation,
			TargetID:      cmd.TargetID,
			WorkOrderID:   cmd.WorkOrderID,
			Fingerprint:   cmd.Fingerprint,
			State:         domain.ReceiptSucceeded,
			StatusCode:    successStatus,
			ResponseBody:  encoded,
			Attempts:      1,
			CreatedAt:     now,
			UpdatedAt:     now,
			CompletedAt:   &now,
		}
		snapshot.Receipts[cmd.storageKey()] = receipt
		captured = encoded
		return nil
	})
	if err != nil {
		var replay *receiptReplay
		if errors.As(err, &replay) {
			return CommandResult{StatusCode: replay.statusCode, Body: replay.body, Replayed: true}, nil
		}
		return CommandResult{}, err
	}
	return CommandResult{StatusCode: successStatus, Body: captured, Replayed: false}, nil
}

func (s *Service) runPlain(
	successStatus int,
	mutate func(*domain.Snapshot) (any, error),
) (CommandResult, error) {
	var captured []byte
	err := s.store.Execute(func(snapshot *domain.Snapshot) error {
		value, err := mutate(snapshot)
		if err != nil {
			return err
		}
		encoded, err := json.Marshal(value)
		if err != nil {
			return domain.NewError(domain.CodeStorage,
				"encode response: "+err.Error())
		}
		captured = encoded
		return nil
	})
	if err != nil {
		return CommandResult{}, err
	}
	return CommandResult{StatusCode: successStatus, Body: captured}, nil
}

func (s *Service) lookupReplay(cmd *Command) (CommandResult, bool, error) {
	var result CommandResult
	found := false
	err := s.store.View(func(snapshot domain.Snapshot) error {
		existing, ok := snapshot.Receipts[cmd.storageKey()]
		if !ok || existing.State != domain.ReceiptSucceeded {
			return nil
		}
		if !sameCommandScope(existing, *cmd) {
			return newReuseConflict(existing, *cmd)
		}
		result = CommandResult{
			StatusCode: existing.StatusCode,
			Body:       append([]byte(nil), existing.ResponseBody...),
			Replayed:   true,
		}
		found = true
		return nil
	})
	if err != nil {
		return CommandResult{}, false, err
	}
	return result, found, nil
}

// sameCommandScope decides whether an incoming request is the same command as
// the stored one. Fingerprint covers every business parameter; the remaining
// fields cover routing context that never appears in the JSON body.
func sameCommandScope(existing domain.Receipt, cmd Command) bool {
	return existing.Operation == cmd.Operation &&
		existing.TargetID == cmd.TargetID &&
		existing.WorkOrderID == cmd.WorkOrderID &&
		existing.Fingerprint == cmd.Fingerprint
}

func newReuseConflict(existing domain.Receipt, cmd Command) error {
	detail := fmt.Sprintf(
		"command number %q from terminal %q is already bound to operation %q",
		cmd.CommandNumber, cmd.TerminalID, existing.Operation)
	switch {
	case existing.Operation != cmd.Operation:
		detail += fmt.Sprintf(" and cannot be reused for %q", cmd.Operation)
	case existing.TargetID != cmd.TargetID:
		detail += fmt.Sprintf(" on target %q", existing.TargetID)
	case existing.WorkOrderID != cmd.WorkOrderID:
		detail += fmt.Sprintf(" under work order %q", existing.WorkOrderID)
	default:
		detail += " with different parameters; resend the original request or issue a new command number"
	}
	return domain.NewError(domain.CodeCommandReuseConflict, detail)
}

// sweepExpiredReceipts removes receipts whose retention window elapsed so the
// snapshot cannot grow without bound. Only the write path sweeps; reads and
// replays never block on it.
func sweepExpiredReceipts(snapshot *domain.Snapshot, now time.Time) {
	for id, receipt := range snapshot.Receipts {
		reference := receipt.UpdatedAt
		if receipt.CompletedAt != nil {
			reference = *receipt.CompletedAt
		}
		ttl := succeededReceiptTTL
		if receipt.State != domain.ReceiptSucceeded {
			ttl = failedReceiptTTL
		}
		if now.Sub(reference) > ttl {
			delete(snapshot.Receipts, id)
		}
	}
}
