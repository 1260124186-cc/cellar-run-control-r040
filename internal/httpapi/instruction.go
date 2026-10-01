package httpapi

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/1260124186-cc/solo-0016-cellar-run-control/internal/domain"
	"github.com/1260124186-cc/solo-0016-cellar-run-control/internal/service"
)

const (
	instructionKeyHeader    = "X-Instruction-Key"
	terminalIDHeader        = "X-Terminal-ID"
	instructionReplayHeader = "X-Instruction-Replay"
	maxInstructionKeyLength = 128
	minInstructionKeyLength = 8
	maxTerminalIDLength     = 64
)

// buildInstruction resolves the idempotency metadata for a write. body is the
// already-decoded request payload; it is re-encoded canonically so that the
// fingerprint only depends on the parameters themselves, not on their
// formatting. X-Expected-Version is deliberately excluded because it is an
// optimistic-lock precondition, not a business parameter.
func buildInstruction(
	request *http.Request,
	route string,
	body any,
) (service.Instruction, error) {
	instruction := service.Instruction{
		Method: request.Method,
		Path:   route,
	}
	key := strings.TrimSpace(request.Header.Get(instructionKeyHeader))
	if key == "" {
		return instruction, nil
	}
	if len(key) < minInstructionKeyLength || len(key) > maxInstructionKeyLength {
		return service.Instruction{}, domain.FieldError(domain.CodeInvalidInput,
			instructionKeyHeader,
			"must contain between 8 and 128 non-blank characters")
	}
	instruction.Key = key
	if terminal := strings.TrimSpace(request.Header.Get(terminalIDHeader)); terminal != "" {
		if len([]rune(terminal)) > maxTerminalIDLength {
			return service.Instruction{}, domain.FieldError(domain.CodeInvalidInput,
				terminalIDHeader, "must not exceed 64 characters")
		}
		instruction.TerminalID = terminal
	}

	canonical, err := json.Marshal(body)
	if err != nil {
		return service.Instruction{}, domain.NewError(domain.CodeStorage, err.Error())
	}
	digest := sha256.New()
	digest.Write([]byte(request.Method))
	digest.Write([]byte{'\n'})
	digest.Write([]byte(route))
	digest.Write([]byte{'\n'})
	digest.Write(canonical)
	instruction.Fingerprint = hex.EncodeToString(digest.Sum(nil))
	return instruction, nil
}

// writeInstructionResult emits the stored bytes of an idempotent result and
// marks replays so a terminal can tell a duplicated submission apart.
func writeInstructionResult[T any](
	response http.ResponseWriter,
	result service.InstructionResult[T],
) {
	if result.Replay {
		response.Header().Set(instructionReplayHeader, "true")
	}
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.WriteHeader(result.Status)
	_, _ = io.Copy(response, bytes.NewReader(result.Body))
}
