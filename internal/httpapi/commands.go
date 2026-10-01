package httpapi

import (
	"net/http"
	"strings"

	"github.com/1260124186-cc/solo-0016-cellar-run-control/internal/domain"
	"github.com/1260124186-cc/solo-0016-cellar-run-control/internal/service"
)

const (
	headerTerminalID    = "X-Terminal-ID"
	headerCommandNumber = "X-Command-Number"
	headerWorkOrder     = "X-Work-Order-ID"
	headerReplay        = "Idempotent-Replay"
)

// parseCommand reads the idempotency headers. The terminal/command pair is
// optional but must arrive together: one without the other is a client bug
// and is rejected instead of silently running without a receipt.
func parseCommand(
	request *http.Request,
	operation string,
	targetID string,
	payload any,
	expectedVersion *int64,
) (*service.Command, error) {
	terminal := strings.TrimSpace(request.Header.Get(headerTerminalID))
	number := strings.TrimSpace(request.Header.Get(headerCommandNumber))
	switch {
	case terminal == "" && number == "":
		return nil, nil
	case terminal == "" || number == "":
		return nil, domain.NewError(domain.CodeCommandHeaderInvalid,
			"both X-Terminal-ID and X-Command-Number must be provided together")
	}
	return &service.Command{
		TerminalID:    terminal,
		CommandNumber: number,
		Operation:     operation,
		TargetID:      targetID,
		WorkOrderID:   strings.TrimSpace(request.Header.Get(headerWorkOrder)),
		Fingerprint:   service.FingerprintPayload(payload, expectedVersion),
	}, nil
}

// writeCommandResult writes the bytes captured from the first successful
// attempt. On a replay the body is the exact original response and the
// Idempotent-Replay marker lets the terminal distinguish a repeated result
// from a fresh execution.
func writeCommandResult(response http.ResponseWriter, result service.CommandResult) {
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	if result.Replayed {
		response.Header().Set(headerReplay, "true")
	}
	response.WriteHeader(result.StatusCode)
	_, _ = response.Write(result.Body)
}
