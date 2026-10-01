package service

import (
	"encoding/json"
	"time"

	"github.com/1260124186-cc/solo-0016-cellar-run-control/internal/domain"
)

// receiptRetention bounds how long a completed instruction receipt is kept
// for replays. Expired receipts are pruned inside the same serialized
// transaction that applies a new instruction, so no background job is needed.
const receiptRetention = 30 * 24 * time.Hour

// Instruction carries the idempotency metadata of one write request. When
// Key is empty the request is processed without a receipt, which preserves
// the legacy non-idempotent behavior.
type Instruction struct {
	Key         string
	TerminalID  string
	Method      string
	Path        string
	Fingerprint string
}

// InstructionResult is what an idempotent use case returns: a typed payload
// for callers that want it, plus the exact status and encoded bytes that the
// HTTP layer must write. Replayed instructions return the stored bytes.
type InstructionResult[T any] struct {
	Payload T
	Status  int
	Body    []byte
	Replay  bool
}

// runInstruction applies mutate exactly once per instruction key.
//
// The duplicate check, the business mutation, response encoding, and the
// receipt write all happen inside one serialized store update, so a crash
// can never leave a receipt without its business fact or vice versa. A
// failure at any point before the commit leaves no record of the key: the
// same key can be retried and later succeed.
func runInstruction[T any](
	s *Service,
	instruction Instruction,
	successStatus int,
	mutate func(*domain.Snapshot) (T, error),
) (InstructionResult[T], error) {
	var empty InstructionResult[T]
	if instruction.Key == "" {
		var payload T
		err := s.store.Execute(func(snapshot *domain.Snapshot) error {
			pruneReceipts(snapshot, s.clock.Now())
			result, applyErr := mutate(snapshot)
			if applyErr != nil {
				return applyErr
			}
			payload = result
			return nil
		})
		if err != nil {
			return empty, err
		}
		body, err := encodeResponse(payload)
		if err != nil {
			return empty, err
		}
		return InstructionResult[T]{Payload: payload, Status: successStatus, Body: body}, nil
	}

	var outcome InstructionResult[T]
	err := s.store.Execute(func(snapshot *domain.Snapshot) error {
		now := s.clock.Now()
		pruneReceipts(snapshot, now)

		if existing, found := snapshot.Receipts[instruction.Key]; found {
			if existing.Fingerprint != instruction.Fingerprint {
				return domain.NewError(domain.CodeInstructionOccupied,
					"instruction key is already occupied by a request with different parameters")
			}
			var payload T
			if err := json.Unmarshal(existing.ResponseJSON, &payload); err != nil {
				return domain.NewError(domain.CodeStorage,
					"stored instruction response could not be decoded")
			}
			outcome = InstructionResult[T]{
				Payload: payload,
				Status:  existing.Status,
				Body:    append([]byte(nil), existing.ResponseJSON...),
				Replay:  true,
			}
			return nil
		}

		payload, applyErr := mutate(snapshot)
		if applyErr != nil {
			return applyErr
		}
		body, encodeErr := encodeResponse(payload)
		if encodeErr != nil {
			return domain.NewError(domain.CodeStorage, encodeErr.Error())
		}
		snapshot.Receipts[instruction.Key] = domain.InstructionReceipt{
			Key:          instruction.Key,
			TerminalID:   instruction.TerminalID,
			Method:       instruction.Method,
			Path:         instruction.Path,
			Fingerprint:  instruction.Fingerprint,
			Status:       successStatus,
			ResponseJSON: body,
			CreatedAt:    now,
		}
		outcome = InstructionResult[T]{
			Payload: payload,
			Status:  successStatus,
			Body:    body,
		}
		return nil
	})
	if err != nil {
		return empty, err
	}
	return outcome, nil
}

// encodeResponse matches the framing the HTTP writer uses so that replayed
// bytes are identical to what the first successful response carried.
func encodeResponse(value any) ([]byte, error) {
	body, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return append(body, '\n'), nil
}

func pruneReceipts(snapshot *domain.Snapshot, now time.Time) {
	cutoff := now.Add(-receiptRetention)
	for key, receipt := range snapshot.Receipts {
		if receipt.CreatedAt.Before(cutoff) {
			delete(snapshot.Receipts, key)
		}
	}
}
