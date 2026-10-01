package workflowcheck

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"time"

	"github.com/1260124186-cc/solo-0016-cellar-run-control/internal/clock"
	"github.com/1260124186-cc/solo-0016-cellar-run-control/internal/domain"
	"github.com/1260124186-cc/solo-0016-cellar-run-control/internal/httpapi"
	"github.com/1260124186-cc/solo-0016-cellar-run-control/internal/service"
	"github.com/1260124186-cc/solo-0016-cellar-run-control/internal/storage"
)

// commandReceiptsRestart proves receipts survive a process restart and that
// an interrupted ("processing") receipt never locks the command number.
func commandReceiptsRestart() error {
	dataDir, err := os.MkdirTemp("", "cellar-receipt-restart-*")
	if err != nil {
		return fmt.Errorf("create restart check directory: %w", err)
	}
	defer os.RemoveAll(dataDir)

	// First "process": create one run under a command number and stop.
	firstBody, err := createRunWithRestart(dataDir, "RUN-RESTART", "CMD-9001")
	if err != nil {
		return err
	}

	// Second "process": reopen the same snapshot and resend the identical
	// command. It must replay the captured response, not create a new run.
	secondBody, replayed, err := replayRunWithRestart(dataDir, "RUN-RESTART", "CMD-9001")
	if err != nil {
		return err
	}
	if !replayed {
		return errInvalidWorkflow("successful command did not replay after restart")
	}
	if string(secondBody) != string(firstBody) {
		return errInvalidWorkflow("replayed response changed across restart")
	}

	// Simulate a crash while processing: hand-write a snapshot containing a
	// receipt stuck in "processing". On reopen it must become retriable, and
	// the same command number must then succeed.
	if err := seedInterruptedReceipt(filepath.Join(dataDir, "interrupted"), "CMD-9002"); err != nil {
		return err
	}
	interruptedDir := filepath.Join(dataDir, "interrupted")
	store, err := storage.Open(interruptedDir)
	if err != nil {
		return err
	}
	app := service.New(store, clock.System{})
	server := httptest.NewServer(httpapi.NewHandler(app))
	api := newClient(server.URL)

	resp, err := api.callWithCommand("POST", "/v1/vessels", map[string]any{
		"code":            "VSL-RECOVER",
		"capacity_liters": 120.0,
	}, nil, &command{terminalID: "term", number: "CMD-9002"})
	server.Close()
	if err != nil {
		return err
	}
	if err := requireStatus(resp, 201,
		"retry command number recovered from interrupted processing"); err != nil {
		return err
	}
	if resp.replay {
		return errInvalidWorkflow("an interrupted receipt must not be replayed as a success")
	}
	return nil
}

func startCheckServer(dataDir string) (*client, func()) {
	store, err := storage.Open(dataDir)
	if err != nil {
		panic(err)
	}
	app := service.New(store, clock.System{})
	server := httptest.NewServer(httpapi.NewHandler(app))
	return newClient(server.URL), server.Close
}

func createRunWithRestart(dataDir, code, number string) ([]byte, error) {
	api, stop := startCheckServer(dataDir)
	defer stop()
	formula, err := ensureApprovedFormula(api, "Restart Formula")
	if err != nil {
		return nil, err
	}
	resp, err := api.callWithCommand("POST", "/v1/runs",
		createRunPayload(code, formula.ID), nil,
		&command{terminalID: "term", number: number})
	if err != nil {
		return nil, err
	}
	if err := requireStatus(resp, 201, "create run before restart"); err != nil {
		return nil, err
	}
	return resp.body, nil
}

func replayRunWithRestart(dataDir, code, number string) ([]byte, bool, error) {
	api, stop := startCheckServer(dataDir)
	defer stop()
	// The formula from the first process lives in the same snapshot; reuse it
	// so the resent payload is byte-for-byte identical to the original.
	formulaID, err := findFormulaID(api, "Restart Formula")
	if err != nil {
		return nil, false, err
	}
	resp, err := api.callWithCommand("POST", "/v1/runs",
		createRunPayload(code, formulaID), nil,
		&command{terminalID: "term", number: number})
	if err != nil {
		return nil, false, err
	}
	return resp.body, resp.replay, nil
}

func findFormulaID(api *client, name string) (string, error) {
	resp, err := api.call("GET", "/v1/formulas", nil, nil)
	if err != nil {
		return "", err
	}
	if err := requireStatus(resp, 200, "list formulas after restart"); err != nil {
		return "", err
	}
	var body struct {
		Formulas []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"formulas"`
	}
	if err := resp.decode(&body); err != nil {
		return "", err
	}
	for _, formula := range body.Formulas {
		if formula.Name == name {
			return formula.ID, nil
		}
	}
	return "", fmt.Errorf("formula %q was not found after restart", name)
}

// seedInterruptedReceipt writes a minimal valid snapshot carrying one receipt
// stuck in the processing state, as if the previous process died mid-request.
func seedInterruptedReceipt(dir, number string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	now := time.Now().UTC()
	snapshot := domain.EmptySnapshot()
	snapshot.Revision = 1
	snapshot.UpdatedAt = now
	snapshot.Receipts["term#"+number] = domain.Receipt{
		ID:            "term#" + number,
		TerminalID:    "term",
		CommandNumber: number,
		Operation:     "vessel.create",
		Fingerprint:   "stale",
		State:         domain.ReceiptProcessing,
		Attempts:      1,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "state.json"), encoded, 0o644)
}
